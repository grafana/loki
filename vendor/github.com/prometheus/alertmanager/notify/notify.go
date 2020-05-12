// Copyright 2015 Prometheus Team
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

package notify

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/cespare/xxhash"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/prometheus/alertmanager/cluster"
	"github.com/prometheus/alertmanager/inhibit"
	"github.com/prometheus/alertmanager/nflog"
	"github.com/prometheus/alertmanager/nflog/nflogpb"
	"github.com/prometheus/alertmanager/silence"
	"github.com/prometheus/alertmanager/types"
)

// ResolvedSender returns true if resolved notifications should be sent.
type ResolvedSender interface {
	SendResolved() bool
}

// MinTimeout is the minimum timeout that is set for the context of a call
// to a notification pipeline.
const MinTimeout = 10 * time.Second

// Notifier notifies about alerts under constraints of the given context. It
// returns an error if unsuccessful and a flag whether the error is
// recoverable. This information is useful for a retry logic.
type Notifier interface {
	Notify(context.Context, ...*types.Alert) (bool, error)
}

// Integration wraps a notifier and its configuration to be uniquely identified
// by name and index from its origin in the configuration.
type Integration struct {
	notifier Notifier
	rs       ResolvedSender
	name     string
	idx      int
}

// NewIntegration returns a new integration.
func NewIntegration(notifier Notifier, rs ResolvedSender, name string, idx int) Integration {
	return Integration{
		notifier: notifier,
		rs:       rs,
		name:     name,
		idx:      idx,
	}
}

// Notify implements the Notifier interface.
func (i *Integration) Notify(ctx context.Context, alerts ...*types.Alert) (bool, error) {
	return i.notifier.Notify(ctx, alerts...)
}

// SendResolved implements the ResolvedSender interface.
func (i *Integration) SendResolved() bool {
	return i.rs.SendResolved()
}

// Name returns the name of the integration.
func (i *Integration) Name() string {
	return i.name
}

// Index returns the index of the integration.
func (i *Integration) Index() int {
	return i.idx
}

// notifyKey defines a custom type with which a context is populated to
// avoid accidental collisions.
type notifyKey int

const (
	keyReceiverName notifyKey = iota
	keyRepeatInterval
	keyGroupLabels
	keyGroupKey
	keyFiringAlerts
	keyResolvedAlerts
	keyNow
)

// WithReceiverName populates a context with a receiver name.
func WithReceiverName(ctx context.Context, rcv string) context.Context {
	return context.WithValue(ctx, keyReceiverName, rcv)
}

// WithGroupKey populates a context with a group key.
func WithGroupKey(ctx context.Context, s string) context.Context {
	return context.WithValue(ctx, keyGroupKey, s)
}

// WithFiringAlerts populates a context with a slice of firing alerts.
func WithFiringAlerts(ctx context.Context, alerts []uint64) context.Context {
	return context.WithValue(ctx, keyFiringAlerts, alerts)
}

// WithResolvedAlerts populates a context with a slice of resolved alerts.
func WithResolvedAlerts(ctx context.Context, alerts []uint64) context.Context {
	return context.WithValue(ctx, keyResolvedAlerts, alerts)
}

// WithGroupLabels populates a context with grouping labels.
func WithGroupLabels(ctx context.Context, lset model.LabelSet) context.Context {
	return context.WithValue(ctx, keyGroupLabels, lset)
}

// WithNow populates a context with a now timestamp.
func WithNow(ctx context.Context, t time.Time) context.Context {
	return context.WithValue(ctx, keyNow, t)
}

// WithRepeatInterval populates a context with a repeat interval.
func WithRepeatInterval(ctx context.Context, t time.Duration) context.Context {
	return context.WithValue(ctx, keyRepeatInterval, t)
}

// RepeatInterval extracts a repeat interval from the context. Iff none exists, the
// second argument is false.
func RepeatInterval(ctx context.Context) (time.Duration, bool) {
	v, ok := ctx.Value(keyRepeatInterval).(time.Duration)
	return v, ok
}

// ReceiverName extracts a receiver name from the context. Iff none exists, the
// second argument is false.
func ReceiverName(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(keyReceiverName).(string)
	return v, ok
}

// GroupKey extracts a group key from the context. Iff none exists, the
// second argument is false.
func GroupKey(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(keyGroupKey).(string)
	return v, ok
}

// GroupLabels extracts grouping label set from the context. Iff none exists, the
// second argument is false.
func GroupLabels(ctx context.Context) (model.LabelSet, bool) {
	v, ok := ctx.Value(keyGroupLabels).(model.LabelSet)
	return v, ok
}

// Now extracts a now timestamp from the context. Iff none exists, the
// second argument is false.
func Now(ctx context.Context) (time.Time, bool) {
	v, ok := ctx.Value(keyNow).(time.Time)
	return v, ok
}

// FiringAlerts extracts a slice of firing alerts from the context.
// Iff none exists, the second argument is false.
func FiringAlerts(ctx context.Context) ([]uint64, bool) {
	v, ok := ctx.Value(keyFiringAlerts).([]uint64)
	return v, ok
}

// ResolvedAlerts extracts a slice of firing alerts from the context.
// Iff none exists, the second argument is false.
func ResolvedAlerts(ctx context.Context) ([]uint64, bool) {
	v, ok := ctx.Value(keyResolvedAlerts).([]uint64)
	return v, ok
}

// A Stage processes alerts under the constraints of the given context.
type Stage interface {
	Exec(ctx context.Context, l log.Logger, alerts ...*types.Alert) (context.Context, []*types.Alert, error)
}

// StageFunc wraps a function to represent a Stage.
type StageFunc func(ctx context.Context, l log.Logger, alerts ...*types.Alert) (context.Context, []*types.Alert, error)

// Exec implements Stage interface.
func (f StageFunc) Exec(ctx context.Context, l log.Logger, alerts ...*types.Alert) (context.Context, []*types.Alert, error) {
	return f(ctx, l, alerts...)
}

type NotificationLog interface {
	Log(r *nflogpb.Receiver, gkey string, firingAlerts, resolvedAlerts []uint64) error
	Query(params ...nflog.QueryParam) ([]*nflogpb.Entry, error)
}

type metrics struct {
	numNotifications           *prometheus.CounterVec
	numFailedNotifications     *prometheus.CounterVec
	notificationLatencySeconds *prometheus.HistogramVec
}

func newMetrics(r prometheus.Registerer) *metrics {
	m := &metrics{
		numNotifications: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "alertmanager",
			Name:      "notifications_total",
			Help:      "The total number of attempted notifications.",
		}, []string{"integration"}),
		numFailedNotifications: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "alertmanager",
			Name:      "notifications_failed_total",
			Help:      "The total number of failed notifications.",
		}, []string{"integration"}),
		notificationLatencySeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "alertmanager",
			Name:      "notification_latency_seconds",
			Help:      "The latency of notifications in seconds.",
			Buckets:   []float64{1, 5, 10, 15, 20},
		}, []string{"integration"}),
	}
	for _, integration := range []string{
		"email",
		"hipchat",
		"pagerduty",
		"wechat",
		"pushover",
		"slack",
		"opsgenie",
		"webhook",
		"victorops",
	} {
		m.numNotifications.WithLabelValues(integration)
		m.numFailedNotifications.WithLabelValues(integration)
		m.notificationLatencySeconds.WithLabelValues(integration)
	}
	r.MustRegister(m.numNotifications, m.numFailedNotifications, m.notificationLatencySeconds)
	return m
}

type PipelineBuilder struct {
	metrics *metrics
}

func NewPipelineBuilder(r prometheus.Registerer) *PipelineBuilder {
	return &PipelineBuilder{
		metrics: newMetrics(r),
	}
}

// New returns a map of receivers to Stages.
func (pb *PipelineBuilder) New(
	receivers map[string][]Integration,
	wait func() time.Duration,
	inhibitor *inhibit.Inhibitor,
	silencer *silence.Silencer,
	notificationLog NotificationLog,
	peer *cluster.Peer,
) RoutingStage {
	rs := make(RoutingStage, len(receivers))

	ms := NewGossipSettleStage(peer)
	is := NewMuteStage(inhibitor)
	ss := NewMuteStage(silencer)

	for name := range receivers {
		st := createReceiverStage(name, receivers[name], wait, notificationLog, pb.metrics)
		rs[name] = MultiStage{ms, is, ss, st}
	}
	return rs
}

// createReceiverStage creates a pipeline of stages for a receiver.
func createReceiverStage(
	name string,
	integrations []Integration,
	wait func() time.Duration,
	notificationLog NotificationLog,
	metrics *metrics,
) Stage {
	var fs FanoutStage
	for i := range integrations {
		recv := &nflogpb.Receiver{
			GroupName:   name,
			Integration: integrations[i].Name(),
			Idx:         uint32(integrations[i].Index()),
		}
		var s MultiStage
		s = append(s, NewWaitStage(wait))
		s = append(s, NewDedupStage(&integrations[i], notificationLog, recv))
		s = append(s, NewRetryStage(integrations[i], name, metrics))
		s = append(s, NewSetNotifiesStage(notificationLog, recv))

		fs = append(fs, s)
	}
	return fs
}

// RoutingStage executes the inner stages based on the receiver specified in
// the context.
type RoutingStage map[string]Stage

// Exec implements the Stage interface.
func (rs RoutingStage) Exec(ctx context.Context, l log.Logger, alerts ...*types.Alert) (context.Context, []*types.Alert, error) {
	receiver, ok := ReceiverName(ctx)
	if !ok {
		return ctx, nil, fmt.Errorf("receiver missing")
	}

	s, ok := rs[receiver]
	if !ok {
		return ctx, nil, fmt.Errorf("stage for receiver missing")
	}

	return s.Exec(ctx, l, alerts...)
}

// A MultiStage executes a series of stages sequentially.
type MultiStage []Stage

// Exec implements the Stage interface.
func (ms MultiStage) Exec(ctx context.Context, l log.Logger, alerts ...*types.Alert) (context.Context, []*types.Alert, error) {
	var err error
	for _, s := range ms {
		if len(alerts) == 0 {
			return ctx, nil, nil
		}

		ctx, alerts, err = s.Exec(ctx, l, alerts...)
		if err != nil {
			return ctx, nil, err
		}
	}
	return ctx, alerts, nil
}

// FanoutStage executes its stages concurrently
type FanoutStage []Stage

// Exec attempts to execute all stages concurrently and discards the results.
// It returns its input alerts and a types.MultiError if one or more stages fail.
func (fs FanoutStage) Exec(ctx context.Context, l log.Logger, alerts ...*types.Alert) (context.Context, []*types.Alert, error) {
	var (
		wg sync.WaitGroup
		me types.MultiError
	)
	wg.Add(len(fs))

	for _, s := range fs {
		go func(s Stage) {
			if _, _, err := s.Exec(ctx, l, alerts...); err != nil {
				me.Add(err)
				lvl := level.Error(l)
				if ctx.Err() == context.Canceled {
					// It is expected for the context to be canceled on
					// configuration reload or shutdown. In this case, the
					// message should only be logged at the debug level.
					lvl = level.Debug(l)
				}
				lvl.Log("msg", "Error on notify", "err", err, "context_err", ctx.Err())
			}
			wg.Done()
		}(s)
	}
	wg.Wait()

	if me.Len() > 0 {
		return ctx, alerts, &me
	}
	return ctx, alerts, nil
}

// GossipSettleStage waits until the Gossip has settled to forward alerts.
type GossipSettleStage struct {
	peer *cluster.Peer
}

// NewGossipSettleStage returns a new GossipSettleStage.
func NewGossipSettleStage(p *cluster.Peer) *GossipSettleStage {
	return &GossipSettleStage{peer: p}
}

func (n *GossipSettleStage) Exec(ctx context.Context, l log.Logger, alerts ...*types.Alert) (context.Context, []*types.Alert, error) {
	if n.peer != nil {
		n.peer.WaitReady()
	}
	return ctx, alerts, nil
}

// MuteStage filters alerts through a Muter.
type MuteStage struct {
	muter types.Muter
}

// NewMuteStage return a new MuteStage.
func NewMuteStage(m types.Muter) *MuteStage {
	return &MuteStage{muter: m}
}

// Exec implements the Stage interface.
func (n *MuteStage) Exec(ctx context.Context, l log.Logger, alerts ...*types.Alert) (context.Context, []*types.Alert, error) {
	var filtered []*types.Alert
	for _, a := range alerts {
		// TODO(fabxc): increment total alerts counter.
		// Do not send the alert if muted.
		if !n.muter.Mutes(a.Labels) {
			filtered = append(filtered, a)
		}
		// TODO(fabxc): increment muted alerts counter if muted.
	}
	return ctx, filtered, nil
}

// WaitStage waits for a certain amount of time before continuing or until the
// context is done.
type WaitStage struct {
	wait func() time.Duration
}

// NewWaitStage returns a new WaitStage.
func NewWaitStage(wait func() time.Duration) *WaitStage {
	return &WaitStage{
		wait: wait,
	}
}

// Exec implements the Stage interface.
func (ws *WaitStage) Exec(ctx context.Context, l log.Logger, alerts ...*types.Alert) (context.Context, []*types.Alert, error) {
	select {
	case <-time.After(ws.wait()):
	case <-ctx.Done():
		return ctx, nil, ctx.Err()
	}
	return ctx, alerts, nil
}

// DedupStage filters alerts.
// Filtering happens based on a notification log.
type DedupStage struct {
	rs    ResolvedSender
	nflog NotificationLog
	recv  *nflogpb.Receiver

	now  func() time.Time
	hash func(*types.Alert) uint64
}

// NewDedupStage wraps a DedupStage that runs against the given notification log.
func NewDedupStage(rs ResolvedSender, l NotificationLog, recv *nflogpb.Receiver) *DedupStage {
	return &DedupStage{
		rs:    rs,
		nflog: l,
		recv:  recv,
		now:   utcNow,
		hash:  hashAlert,
	}
}

func utcNow() time.Time {
	return time.Now().UTC()
}

var hashBuffers = sync.Pool{}

func getHashBuffer() []byte {
	b := hashBuffers.Get()
	if b == nil {
		return make([]byte, 0, 1024)
	}
	return b.([]byte)
}

func putHashBuffer(b []byte) {
	b = b[:0]
	//lint:ignore SA6002 relax staticcheck verification.
	hashBuffers.Put(b)
}

func hashAlert(a *types.Alert) uint64 {
	const sep = '\xff'

	b := getHashBuffer()
	defer putHashBuffer(b)

	names := make(model.LabelNames, 0, len(a.Labels))

	for ln := range a.Labels {
		names = append(names, ln)
	}
	sort.Sort(names)

	for _, ln := range names {
		b = append(b, string(ln)...)
		b = append(b, sep)
		b = append(b, string(a.Labels[ln])...)
		b = append(b, sep)
	}

	hash := xxhash.Sum64(b)

	return hash
}

func (n *DedupStage) needsUpdate(entry *nflogpb.Entry, firing, resolved map[uint64]struct{}, repeat time.Duration) bool {
	// If we haven't notified about the alert group before, notify right away
	// unless we only have resolved alerts.
	if entry == nil {
		return len(firing) > 0
	}

	if !entry.IsFiringSubset(firing) {
		return true
	}

	// Notify about all alerts being resolved.
	// This is done irrespective of the send_resolved flag to make sure that
	// the firing alerts are cleared from the notification log.
	if len(firing) == 0 {
		// If the current alert group and last notification contain no firing
		// alert, it means that some alerts have been fired and resolved during the
		// last interval. In this case, there is no need to notify the receiver
		// since it doesn't know about them.
		return len(entry.FiringAlerts) > 0
	}

	if n.rs.SendResolved() && !entry.IsResolvedSubset(resolved) {
		return true
	}

	// Nothing changed, only notify if the repeat interval has passed.
	return entry.Timestamp.Before(n.now().Add(-repeat))
}

// Exec implements the Stage interface.
func (n *DedupStage) Exec(ctx context.Context, l log.Logger, alerts ...*types.Alert) (context.Context, []*types.Alert, error) {
	gkey, ok := GroupKey(ctx)
	if !ok {
		return ctx, nil, fmt.Errorf("group key missing")
	}

	repeatInterval, ok := RepeatInterval(ctx)
	if !ok {
		return ctx, nil, fmt.Errorf("repeat interval missing")
	}

	firingSet := map[uint64]struct{}{}
	resolvedSet := map[uint64]struct{}{}
	firing := []uint64{}
	resolved := []uint64{}

	var hash uint64
	for _, a := range alerts {
		hash = n.hash(a)
		if a.Resolved() {
			resolved = append(resolved, hash)
			resolvedSet[hash] = struct{}{}
		} else {
			firing = append(firing, hash)
			firingSet[hash] = struct{}{}
		}
	}

	ctx = WithFiringAlerts(ctx, firing)
	ctx = WithResolvedAlerts(ctx, resolved)

	entries, err := n.nflog.Query(nflog.QGroupKey(gkey), nflog.QReceiver(n.recv))
	if err != nil && err != nflog.ErrNotFound {
		return ctx, nil, err
	}

	var entry *nflogpb.Entry
	switch len(entries) {
	case 0:
	case 1:
		entry = entries[0]
	default:
		return ctx, nil, fmt.Errorf("unexpected entry result size %d", len(entries))
	}

	if n.needsUpdate(entry, firingSet, resolvedSet, repeatInterval) {
		return ctx, alerts, nil
	}
	return ctx, nil, nil
}

// RetryStage notifies via passed integration with exponential backoff until it
// succeeds. It aborts if the context is canceled or timed out.
type RetryStage struct {
	integration Integration
	groupName   string
	metrics     *metrics
}

// NewRetryStage returns a new instance of a RetryStage.
func NewRetryStage(i Integration, groupName string, metrics *metrics) *RetryStage {
	return &RetryStage{
		integration: i,
		groupName:   groupName,
		metrics:     metrics,
	}
}

// Exec implements the Stage interface.
func (r RetryStage) Exec(ctx context.Context, l log.Logger, alerts ...*types.Alert) (context.Context, []*types.Alert, error) {
	var sent []*types.Alert

	// If we shouldn't send notifications for resolved alerts, but there are only
	// resolved alerts, report them all as successfully notified (we still want the
	// notification log to log them for the next run of DedupStage).
	if !r.integration.SendResolved() {
		firing, ok := FiringAlerts(ctx)
		if !ok {
			return ctx, nil, fmt.Errorf("firing alerts missing")
		}
		if len(firing) == 0 {
			return ctx, alerts, nil
		}
		for _, a := range alerts {
			if a.Status() != model.AlertResolved {
				sent = append(sent, a)
			}
		}
	} else {
		sent = alerts
	}

	var (
		i    = 0
		b    = backoff.NewExponentialBackOff()
		tick = backoff.NewTicker(b)
		iErr error
	)
	defer tick.Stop()

	for {
		i++
		// Always check the context first to not notify again.
		select {
		case <-ctx.Done():
			if iErr != nil {
				return ctx, nil, iErr
			}

			return ctx, nil, ctx.Err()
		default:
		}

		select {
		case <-tick.C:
			now := time.Now()
			retry, err := r.integration.Notify(ctx, sent...)
			r.metrics.notificationLatencySeconds.WithLabelValues(r.integration.Name()).Observe(time.Since(now).Seconds())
			r.metrics.numNotifications.WithLabelValues(r.integration.Name()).Inc()
			if err != nil {
				r.metrics.numFailedNotifications.WithLabelValues(r.integration.Name()).Inc()
				level.Debug(l).Log("msg", "Notify attempt failed", "attempt", i, "integration", r.integration.Name(), "receiver", r.groupName, "err", err)
				if !retry {
					return ctx, alerts, fmt.Errorf("cancelling notify retry for %q due to unrecoverable error: %s", r.integration.Name(), err)
				}

				// Save this error to be able to return the last seen error by an
				// integration upon context timeout.
				iErr = err
			} else {
				return ctx, alerts, nil
			}
		case <-ctx.Done():
			if iErr != nil {
				return ctx, nil, iErr
			}

			return ctx, nil, ctx.Err()
		}
	}
}

// SetNotifiesStage sets the notification information about passed alerts. The
// passed alerts should have already been sent to the receivers.
type SetNotifiesStage struct {
	nflog NotificationLog
	recv  *nflogpb.Receiver
}

// NewSetNotifiesStage returns a new instance of a SetNotifiesStage.
func NewSetNotifiesStage(l NotificationLog, recv *nflogpb.Receiver) *SetNotifiesStage {
	return &SetNotifiesStage{
		nflog: l,
		recv:  recv,
	}
}

// Exec implements the Stage interface.
func (n SetNotifiesStage) Exec(ctx context.Context, l log.Logger, alerts ...*types.Alert) (context.Context, []*types.Alert, error) {
	gkey, ok := GroupKey(ctx)
	if !ok {
		return ctx, nil, fmt.Errorf("group key missing")
	}

	firing, ok := FiringAlerts(ctx)
	if !ok {
		return ctx, nil, fmt.Errorf("firing alerts missing")
	}

	resolved, ok := ResolvedAlerts(ctx)
	if !ok {
		return ctx, nil, fmt.Errorf("resolved alerts missing")
	}

	return ctx, alerts, n.nflog.Log(n.recv, gkey, firing, resolved)
}
