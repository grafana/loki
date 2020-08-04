// Copyright 2016 Prometheus Team
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package silence provides a storage for silences, which can share its
// state over a mesh network and snapshot it.
package silence

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os"
	"reflect"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/matttproud/golang_protobuf_extensions/pbutil"
	"github.com/pkg/errors"
	"github.com/prometheus/alertmanager/cluster"
	pb "github.com/prometheus/alertmanager/silence/silencepb"
	"github.com/prometheus/alertmanager/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	uuid "github.com/satori/go.uuid"
)

// ErrNotFound is returned if a silence was not found.
var ErrNotFound = fmt.Errorf("silence not found")

// ErrInvalidState is returned if the state isn't valid.
var ErrInvalidState = fmt.Errorf("invalid state")

func utcNow() time.Time {
	return time.Now().UTC()
}

type matcherCache map[*pb.Silence]types.Matchers

// Get retrieves the matchers for a given silence. If it is a missed cache
// access, it compiles and adds the matchers of the requested silence to the
// cache.
func (c matcherCache) Get(s *pb.Silence) (types.Matchers, error) {
	if m, ok := c[s]; ok {
		return m, nil
	}
	return c.add(s)
}

// add compiles a silences' matchers and adds them to the cache.
// It returns the compiled matchers.
func (c matcherCache) add(s *pb.Silence) (types.Matchers, error) {
	var (
		ms types.Matchers
		mt *types.Matcher
	)

	for _, m := range s.Matchers {
		mt = &types.Matcher{
			Name:  m.Name,
			Value: m.Pattern,
		}
		switch m.Type {
		case pb.Matcher_EQUAL:
			mt.IsRegex = false
		case pb.Matcher_REGEXP:
			mt.IsRegex = true
		}
		err := mt.Init()
		if err != nil {
			return nil, err
		}

		ms = append(ms, mt)
	}

	c[s] = ms

	return ms, nil
}

// Silencer binds together a Marker and a Silences to implement the Muter
// interface.
type Silencer struct {
	silences *Silences
	marker   types.Marker
	logger   log.Logger
}

// NewSilencer returns a new Silencer.
func NewSilencer(s *Silences, m types.Marker, l log.Logger) *Silencer {
	return &Silencer{
		silences: s,
		marker:   m,
		logger:   l,
	}
}

// Mutes implements the Muter interface.
func (s *Silencer) Mutes(lset model.LabelSet) bool {
	fp := lset.Fingerprint()
	ids, markerVersion, _ := s.marker.Silenced(fp)

	var (
		err        error
		sils       []*pb.Silence
		newVersion = markerVersion
	)
	if markerVersion == s.silences.Version() {
		// No new silences added, just need to check which of the old
		// silences are still relevant.
		if len(ids) == 0 {
			// Super fast path: No silences ever applied to this
			// alert, none have been added. We are done.
			return false
		}
		// This is still a quite fast path: No silences have been added,
		// we only need to check which of the applicable silences are
		// currently active. Note that newVersion is left at
		// markerVersion because the Query call might already return a
		// newer version, which is not the version our old list of
		// applicable silences is based on.
		sils, _, err = s.silences.Query(
			QIDs(ids...),
			QState(types.SilenceStateActive),
		)
	} else {
		// New silences have been added, do a full query.
		sils, newVersion, err = s.silences.Query(
			QState(types.SilenceStateActive),
			QMatches(lset),
		)
	}
	if err != nil {
		level.Error(s.logger).Log("msg", "Querying silences failed, alerts might not get silenced correctly", "err", err)
	}
	if len(sils) == 0 {
		s.marker.SetSilenced(fp, newVersion)
		return false
	}
	idsChanged := len(sils) != len(ids)
	if !idsChanged {
		// Length is the same, but is the content the same?
		for i, s := range sils {
			if ids[i] != s.Id {
				idsChanged = true
				break
			}
		}
	}
	if idsChanged {
		// Need to recreate ids.
		ids = make([]string, len(sils))
		for i, s := range sils {
			ids[i] = s.Id
		}
		sort.Strings(ids) // For comparability.
	}
	if idsChanged || newVersion != markerVersion {
		// Update marker only if something changed.
		s.marker.SetSilenced(fp, newVersion, ids...)
	}
	return true
}

// Silences holds a silence state that can be modified, queried, and snapshot.
type Silences struct {
	logger    log.Logger
	metrics   *metrics
	now       func() time.Time
	retention time.Duration

	mtx       sync.RWMutex
	st        state
	version   int // Increments whenever silences are added.
	broadcast func([]byte)
	mc        matcherCache
}

type metrics struct {
	gcDuration              prometheus.Summary
	snapshotDuration        prometheus.Summary
	snapshotSize            prometheus.Gauge
	queriesTotal            prometheus.Counter
	queryErrorsTotal        prometheus.Counter
	queryDuration           prometheus.Histogram
	silencesActive          prometheus.GaugeFunc
	silencesPending         prometheus.GaugeFunc
	silencesExpired         prometheus.GaugeFunc
	propagatedMessagesTotal prometheus.Counter
}

func newSilenceMetricByState(s *Silences, st types.SilenceState) prometheus.GaugeFunc {
	return prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name:        "alertmanager_silences",
			Help:        "How many silences by state.",
			ConstLabels: prometheus.Labels{"state": string(st)},
		},
		func() float64 {
			count, err := s.CountState(st)
			if err != nil {
				level.Error(s.logger).Log("msg", "Counting silences failed", "err", err)
			}
			return float64(count)
		},
	)
}

func newMetrics(r prometheus.Registerer, s *Silences) *metrics {
	m := &metrics{}

	m.gcDuration = prometheus.NewSummary(prometheus.SummaryOpts{
		Name:       "alertmanager_silences_gc_duration_seconds",
		Help:       "Duration of the last silence garbage collection cycle.",
		Objectives: map[float64]float64{},
	})
	m.snapshotDuration = prometheus.NewSummary(prometheus.SummaryOpts{
		Name:       "alertmanager_silences_snapshot_duration_seconds",
		Help:       "Duration of the last silence snapshot.",
		Objectives: map[float64]float64{},
	})
	m.snapshotSize = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "alertmanager_silences_snapshot_size_bytes",
		Help: "Size of the last silence snapshot in bytes.",
	})
	m.queriesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "alertmanager_silences_queries_total",
		Help: "How many silence queries were received.",
	})
	m.queryErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "alertmanager_silences_query_errors_total",
		Help: "How many silence received queries did not succeed.",
	})
	m.queryDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "alertmanager_silences_query_duration_seconds",
		Help: "Duration of silence query evaluation.",
	})
	m.propagatedMessagesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "alertmanager_silences_gossip_messages_propagated_total",
		Help: "Number of received gossip messages that have been further gossiped.",
	})
	if s != nil {
		m.silencesActive = newSilenceMetricByState(s, types.SilenceStateActive)
		m.silencesPending = newSilenceMetricByState(s, types.SilenceStatePending)
		m.silencesExpired = newSilenceMetricByState(s, types.SilenceStateExpired)
	}

	if r != nil {
		r.MustRegister(
			m.gcDuration,
			m.snapshotDuration,
			m.snapshotSize,
			m.queriesTotal,
			m.queryErrorsTotal,
			m.queryDuration,
			m.silencesActive,
			m.silencesPending,
			m.silencesExpired,
			m.propagatedMessagesTotal,
		)
	}
	return m
}

// Options exposes configuration options for creating a new Silences object.
// Its zero value is a safe default.
type Options struct {
	// A snapshot file or reader from which the initial state is loaded.
	// None or only one of them must be set.
	SnapshotFile   string
	SnapshotReader io.Reader

	// Retention time for newly created Silences. Silences may be
	// garbage collected after the given duration after they ended.
	Retention time.Duration

	// A logger used by background processing.
	Logger  log.Logger
	Metrics prometheus.Registerer
}

func (o *Options) validate() error {
	if o.SnapshotFile != "" && o.SnapshotReader != nil {
		return fmt.Errorf("only one of SnapshotFile and SnapshotReader must be set")
	}
	return nil
}

// New returns a new Silences object with the given configuration.
func New(o Options) (*Silences, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}
	if o.SnapshotFile != "" {
		if r, err := os.Open(o.SnapshotFile); err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
		} else {
			o.SnapshotReader = r
		}
	}
	s := &Silences{
		mc:        matcherCache{},
		logger:    log.NewNopLogger(),
		retention: o.Retention,
		now:       utcNow,
		broadcast: func([]byte) {},
		st:        state{},
	}
	s.metrics = newMetrics(o.Metrics, s)

	if o.Logger != nil {
		s.logger = o.Logger
	}
	if o.SnapshotReader != nil {
		if err := s.loadSnapshot(o.SnapshotReader); err != nil {
			return s, err
		}
	}
	return s, nil
}

// Maintenance garbage collects the silence state at the given interval. If the snapshot
// file is set, a snapshot is written to it afterwards.
// Terminates on receiving from stopc.
func (s *Silences) Maintenance(interval time.Duration, snapf string, stopc <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()

	f := func() error {
		start := s.now()
		var size int64

		level.Debug(s.logger).Log("msg", "Running maintenance")
		defer func() {
			level.Debug(s.logger).Log("msg", "Maintenance done", "duration", s.now().Sub(start), "size", size)
			s.metrics.snapshotSize.Set(float64(size))
		}()

		if _, err := s.GC(); err != nil {
			return err
		}
		if snapf == "" {
			return nil
		}
		f, err := openReplace(snapf)
		if err != nil {
			return err
		}
		if size, err = s.Snapshot(f); err != nil {
			return err
		}
		return f.Close()
	}

Loop:
	for {
		select {
		case <-stopc:
			break Loop
		case <-t.C:
			if err := f(); err != nil {
				level.Info(s.logger).Log("msg", "Running maintenance failed", "err", err)
			}
		}
	}
	// No need for final maintenance if we don't want to snapshot.
	if snapf == "" {
		return
	}
	if err := f(); err != nil {
		level.Info(s.logger).Log("msg", "Creating shutdown snapshot failed", "err", err)
	}
}

// GC runs a garbage collection that removes silences that have ended longer
// than the configured retention time ago.
func (s *Silences) GC() (int, error) {
	start := time.Now()
	defer func() { s.metrics.gcDuration.Observe(time.Since(start).Seconds()) }()

	now := s.now()
	var n int

	s.mtx.Lock()
	defer s.mtx.Unlock()

	for id, sil := range s.st {
		if sil.ExpiresAt.IsZero() {
			return n, errors.New("unexpected zero expiration timestamp")
		}
		if !sil.ExpiresAt.After(now) {
			delete(s.st, id)
			delete(s.mc, sil.Silence)
			n++
		}
	}

	return n, nil
}

func validateMatcher(m *pb.Matcher) error {
	if !model.LabelName(m.Name).IsValid() {
		return fmt.Errorf("invalid label name %q", m.Name)
	}
	switch m.Type {
	case pb.Matcher_EQUAL:
		if !model.LabelValue(m.Pattern).IsValid() {
			return fmt.Errorf("invalid label value %q", m.Pattern)
		}
	case pb.Matcher_REGEXP:
		if _, err := regexp.Compile(m.Pattern); err != nil {
			return fmt.Errorf("invalid regular expression %q: %s", m.Pattern, err)
		}
	default:
		return fmt.Errorf("unknown matcher type %q", m.Type)
	}
	return nil
}

func matchesEmpty(m *pb.Matcher) bool {
	switch m.Type {
	case pb.Matcher_EQUAL:
		return m.Pattern == ""
	case pb.Matcher_REGEXP:
		matched, _ := regexp.MatchString(m.Pattern, "")
		return matched
	default:
		return false
	}
}

func validateSilence(s *pb.Silence) error {
	if s.Id == "" {
		return errors.New("ID missing")
	}
	if len(s.Matchers) == 0 {
		return errors.New("at least one matcher required")
	}
	allMatchEmpty := true
	for i, m := range s.Matchers {
		if err := validateMatcher(m); err != nil {
			return fmt.Errorf("invalid label matcher %d: %s", i, err)
		}
		allMatchEmpty = allMatchEmpty && matchesEmpty(m)
	}
	if allMatchEmpty {
		return errors.New("at least one matcher must not match the empty string")
	}
	if s.StartsAt.IsZero() {
		return errors.New("invalid zero start timestamp")
	}
	if s.EndsAt.IsZero() {
		return errors.New("invalid zero end timestamp")
	}
	if s.EndsAt.Before(s.StartsAt) {
		return errors.New("end time must not be before start time")
	}
	if s.UpdatedAt.IsZero() {
		return errors.New("invalid zero update timestamp")
	}
	return nil
}

// cloneSilence returns a shallow copy of a silence.
func cloneSilence(sil *pb.Silence) *pb.Silence {
	s := *sil
	return &s
}

func (s *Silences) getSilence(id string) (*pb.Silence, bool) {
	msil, ok := s.st[id]
	if !ok {
		return nil, false
	}
	return msil.Silence, true
}

func (s *Silences) setSilence(sil *pb.Silence, now time.Time) error {
	sil.UpdatedAt = now

	if err := validateSilence(sil); err != nil {
		return errors.Wrap(err, "silence invalid")
	}

	msil := &pb.MeshSilence{
		Silence:   sil,
		ExpiresAt: sil.EndsAt.Add(s.retention),
	}
	b, err := marshalMeshSilence(msil)
	if err != nil {
		return err
	}

	if s.st.merge(msil, now) {
		s.version++
	}
	s.broadcast(b)

	return nil
}

// Set the specified silence. If a silence with the ID already exists and the modification
// modifies history, the old silence gets expired and a new one is created.
func (s *Silences) Set(sil *pb.Silence) (string, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	now := s.now()
	prev, ok := s.getSilence(sil.Id)

	if sil.Id != "" && !ok {
		return "", ErrNotFound
	}
	if ok {
		if canUpdate(prev, sil, now) {
			return sil.Id, s.setSilence(sil, now)
		}
		if getState(prev, s.now()) != types.SilenceStateExpired {
			// We cannot update the silence, expire the old one.
			if err := s.expire(prev.Id); err != nil {
				return "", errors.Wrap(err, "expire previous silence")
			}
		}
	}
	// If we got here it's either a new silence or a replacing one.
	sil.Id = uuid.NewV4().String()

	if sil.StartsAt.Before(now) {
		sil.StartsAt = now
	}

	return sil.Id, s.setSilence(sil, now)
}

// canUpdate returns true if silence a can be updated to b without
// affecting the historic view of silencing.
func canUpdate(a, b *pb.Silence, now time.Time) bool {
	if !reflect.DeepEqual(a.Matchers, b.Matchers) {
		return false
	}
	// Allowed timestamp modifications depend on the current time.
	switch st := getState(a, now); st {
	case types.SilenceStateActive:
		if !b.StartsAt.Equal(a.StartsAt) {
			return false
		}
		if b.EndsAt.Before(now) {
			return false
		}
	case types.SilenceStatePending:
		if b.StartsAt.Before(now) {
			return false
		}
	case types.SilenceStateExpired:
		return false
	default:
		panic("unknown silence state")
	}
	return true
}

// Expire the silence with the given ID immediately.
func (s *Silences) Expire(id string) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return s.expire(id)
}

// Expire the silence with the given ID immediately.
func (s *Silences) expire(id string) error {
	sil, ok := s.getSilence(id)
	if !ok {
		return ErrNotFound
	}
	sil = cloneSilence(sil)
	now := s.now()

	switch getState(sil, now) {
	case types.SilenceStateExpired:
		return errors.Errorf("silence %s already expired", id)
	case types.SilenceStateActive:
		sil.EndsAt = now
	case types.SilenceStatePending:
		// Set both to now to make Silence move to "expired" state
		sil.StartsAt = now
		sil.EndsAt = now
	}

	return s.setSilence(sil, now)
}

// QueryParam expresses parameters along which silences are queried.
type QueryParam func(*query) error

type query struct {
	ids     []string
	filters []silenceFilter
}

// silenceFilter is a function that returns true if a silence
// should be dropped from a result set for a given time.
type silenceFilter func(*pb.Silence, *Silences, time.Time) (bool, error)

// QIDs configures a query to select the given silence IDs.
func QIDs(ids ...string) QueryParam {
	return func(q *query) error {
		q.ids = append(q.ids, ids...)
		return nil
	}
}

// QMatches returns silences that match the given label set.
func QMatches(set model.LabelSet) QueryParam {
	return func(q *query) error {
		f := func(sil *pb.Silence, s *Silences, _ time.Time) (bool, error) {
			m, err := s.mc.Get(sil)
			if err != nil {
				return true, err
			}
			return m.Match(set), nil
		}
		q.filters = append(q.filters, f)
		return nil
	}
}

// getState returns a silence's SilenceState at the given timestamp.
func getState(sil *pb.Silence, ts time.Time) types.SilenceState {
	if ts.Before(sil.StartsAt) {
		return types.SilenceStatePending
	}
	if ts.After(sil.EndsAt) {
		return types.SilenceStateExpired
	}
	return types.SilenceStateActive
}

// QState filters queried silences by the given states.
func QState(states ...types.SilenceState) QueryParam {
	return func(q *query) error {
		f := func(sil *pb.Silence, _ *Silences, now time.Time) (bool, error) {
			s := getState(sil, now)

			for _, ps := range states {
				if s == ps {
					return true, nil
				}
			}
			return false, nil
		}
		q.filters = append(q.filters, f)
		return nil
	}
}

// QueryOne queries with the given parameters and returns the first result.
// Returns ErrNotFound if the query result is empty.
func (s *Silences) QueryOne(params ...QueryParam) (*pb.Silence, error) {
	res, _, err := s.Query(params...)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, ErrNotFound
	}
	return res[0], nil
}

// Query for silences based on the given query parameters. It returns the
// resulting silences and the state version the result is based on.
func (s *Silences) Query(params ...QueryParam) ([]*pb.Silence, int, error) {
	s.metrics.queriesTotal.Inc()
	defer prometheus.NewTimer(s.metrics.queryDuration).ObserveDuration()

	q := &query{}
	for _, p := range params {
		if err := p(q); err != nil {
			s.metrics.queryErrorsTotal.Inc()
			return nil, s.Version(), err
		}
	}
	sils, version, err := s.query(q, s.now())
	if err != nil {
		s.metrics.queryErrorsTotal.Inc()
	}
	return sils, version, err
}

// Version of the silence state.
func (s *Silences) Version() int {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.version
}

// CountState counts silences by state.
func (s *Silences) CountState(states ...types.SilenceState) (int, error) {
	// This could probably be optimized.
	sils, _, err := s.Query(QState(states...))
	if err != nil {
		return -1, err
	}
	return len(sils), nil
}

func (s *Silences) query(q *query, now time.Time) ([]*pb.Silence, int, error) {
	// If we have no ID constraint, all silences are our base set.  This and
	// the use of post-filter functions is the trivial solution for now.
	var res []*pb.Silence

	s.mtx.Lock()
	defer s.mtx.Unlock()

	if q.ids != nil {
		for _, id := range q.ids {
			if s, ok := s.st[id]; ok {
				res = append(res, s.Silence)
			}
		}
	} else {
		for _, sil := range s.st {
			res = append(res, sil.Silence)
		}
	}

	var resf []*pb.Silence
	for _, sil := range res {
		remove := false
		for _, f := range q.filters {
			ok, err := f(sil, s, now)
			if err != nil {
				return nil, s.version, err
			}
			if !ok {
				remove = true
				break
			}
		}
		if !remove {
			resf = append(resf, cloneSilence(sil))
		}
	}

	return resf, s.version, nil
}

// loadSnapshot loads a snapshot generated by Snapshot() into the state.
// Any previous state is wiped.
func (s *Silences) loadSnapshot(r io.Reader) error {
	st, err := decodeState(r)
	if err != nil {
		return err
	}
	for _, e := range st {
		// Comments list was moved to a single comment. Upgrade on loading the snapshot.
		if len(e.Silence.Comments) > 0 {
			e.Silence.Comment = e.Silence.Comments[0].Comment
			e.Silence.CreatedBy = e.Silence.Comments[0].Author
			e.Silence.Comments = nil
		}
		st[e.Silence.Id] = e
	}
	s.mtx.Lock()
	s.st = st
	s.version++
	s.mtx.Unlock()

	return nil
}

// Snapshot writes the full internal state into the writer and returns the number of bytes
// written.
func (s *Silences) Snapshot(w io.Writer) (int64, error) {
	start := time.Now()
	defer func() { s.metrics.snapshotDuration.Observe(time.Since(start).Seconds()) }()

	s.mtx.RLock()
	defer s.mtx.RUnlock()

	b, err := s.st.MarshalBinary()
	if err != nil {
		return 0, err
	}

	return io.Copy(w, bytes.NewReader(b))
}

// MarshalBinary serializes all silences.
func (s *Silences) MarshalBinary() ([]byte, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	return s.st.MarshalBinary()
}

// Merge merges silence state received from the cluster with the local state.
func (s *Silences) Merge(b []byte) error {
	st, err := decodeState(bytes.NewReader(b))
	if err != nil {
		return err
	}
	s.mtx.Lock()
	defer s.mtx.Unlock()

	now := s.now()

	for _, e := range st {
		if merged := s.st.merge(e, now); merged {
			s.version++
			if !cluster.OversizedMessage(b) {
				// If this is the first we've seen the message and it's
				// not oversized, gossip it to other nodes. We don't
				// propagate oversized messages because they're sent to
				// all nodes already.
				s.broadcast(b)
				s.metrics.propagatedMessagesTotal.Inc()
				level.Debug(s.logger).Log("msg", "Gossiping new silence", "silence", e)
			}
		}
	}
	return nil
}

// SetBroadcast sets the provided function as the one creating data to be
// broadcast.
func (s *Silences) SetBroadcast(f func([]byte)) {
	s.mtx.Lock()
	s.broadcast = f
	s.mtx.Unlock()
}

type state map[string]*pb.MeshSilence

func (s state) merge(e *pb.MeshSilence, now time.Time) bool {
	id := e.Silence.Id
	if e.ExpiresAt.Before(now) {
		return false
	}
	// Comments list was moved to a single comment. Apply upgrade
	// on silences received from peers.
	if len(e.Silence.Comments) > 0 {
		e.Silence.Comment = e.Silence.Comments[0].Comment
		e.Silence.CreatedBy = e.Silence.Comments[0].Author
		e.Silence.Comments = nil
	}

	prev, ok := s[id]
	if !ok || prev.Silence.UpdatedAt.Before(e.Silence.UpdatedAt) {
		s[id] = e
		return true
	}
	return false
}

func (s state) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer

	for _, e := range s {
		if _, err := pbutil.WriteDelimited(&buf, e); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func decodeState(r io.Reader) (state, error) {
	st := state{}
	for {
		var s pb.MeshSilence
		_, err := pbutil.ReadDelimited(r, &s)
		if err == nil {
			if s.Silence == nil {
				return nil, ErrInvalidState
			}
			st[s.Silence.Id] = &s
			continue
		}
		if err == io.EOF {
			break
		}
		return nil, err
	}
	return st, nil
}

func marshalMeshSilence(e *pb.MeshSilence) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := pbutil.WriteDelimited(&buf, e); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// replaceFile wraps a file that is moved to another filename on closing.
type replaceFile struct {
	*os.File
	filename string
}

func (f *replaceFile) Close() error {
	if err := f.File.Sync(); err != nil {
		return err
	}
	if err := f.File.Close(); err != nil {
		return err
	}
	return os.Rename(f.File.Name(), f.filename)
}

// openReplace opens a new temporary file that is moved to filename on closing.
func openReplace(filename string) (*replaceFile, error) {
	tmpFilename := fmt.Sprintf("%s.%x", filename, uint64(rand.Int63()))

	f, err := os.Create(tmpFilename)
	if err != nil {
		return nil, err
	}

	rf := &replaceFile{
		File:     f,
		filename: filename,
	}
	return rf, nil
}
