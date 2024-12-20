package tsdb

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/util/wal"
)

/*
period is a duration which the ingesters use to group index writes into a (WAL,TenantHeads) pair.
After each period elapses, a set of zero or more multitenant TSDB indices are built (one per
index bucket, generally 24h).

It's important to note that this cycle occurs in real time as opposed to the timestamps of
chunk entries. Index writes during the `period` may span multiple index buckets. Periods
also expose some helper functions to get the remainder-less offset integer for that period,
which we use in file creation/etc.
*/
type period time.Duration

const (
	defaultRotationPeriod = period(15 * time.Minute)
	// defines the period to check for active head rotation
	defaultRotationCheckPeriod = 1 * time.Minute
)

func (p period) PeriodFor(t time.Time) int {
	return int(t.UnixNano() / int64(p))
}

func (p period) TimeForPeriod(n int) time.Time {
	return time.Unix(0, int64(p)*int64(n))
}

// Do not specify without bit shifting. This allows us to
// do shard index calcuations via bitwise & rather than modulos.
const defaultHeadManagerStripeSize = 1 << 7

/*
HeadManager both accepts flushed chunk writes
and exposes the index interface for multiple tenants.
It also handles updating an underlying WAL and periodically
rotates both the tenant Heads and the underlying WAL, using
the old versions to build + upload TSDB files.

On disk, it looks like:

tsdb/
     v1/
		# scratch directory used for temp tsdb files during build stage
		scratch/
		# wal directory used to store WALs being written on the ingester.
		# These are eventually shipped to storage as multi-tenant TSDB files
		# and compacted into per tenant indices
		wal/
			<timestamp>
		# multitenant tsdb files which are created on the ingesters/shipped
		multitenant/
					<timestamp>-<ingester-name>.tsdb
		per_tenant/
		 		  # post-compaction tenant tsdbs which are grouped per
				  # period bucket
				  <tenant>/
						   <bucket>/
									index-<from>-<through>-<checksum>.tsdb
*/

type HeadManager struct {
	name    string
	log     log.Logger
	dir     string
	metrics *Metrics

	// RLocked for all writes/reads,
	// Locked before rotating heads/wal
	mtx sync.RWMutex

	// how often WALs should be rotated and TSDBs cut
	period period

	tsdbManager  TSDBManager
	active, prev *headWAL

	shards                 int
	activeHeads, prevHeads *tenantHeads

	Index

	wg     sync.WaitGroup
	cancel chan struct{}
}

func NewHeadManager(name string, logger log.Logger, dir string, metrics *Metrics, tsdbManager TSDBManager) *HeadManager {
	shards := defaultHeadManagerStripeSize
	m := &HeadManager{
		name:        name,
		log:         log.With(logger, "component", "tsdb-head-manager"),
		dir:         dir,
		metrics:     metrics,
		tsdbManager: tsdbManager,

		period: defaultRotationPeriod,
		shards: shards,

		cancel: make(chan struct{}),
	}

	m.Index = LazyIndex(func() (Index, error) {
		m.mtx.RLock()
		defer m.mtx.RUnlock()

		var indices []Index
		if m.prevHeads != nil {
			indices = append(indices, m.prevHeads)
		}
		if m.activeHeads != nil {
			indices = append(indices, m.activeHeads)
		}

		return NewMultiIndex(IndexSlice(indices)), nil
	})

	return m
}

func (m *HeadManager) buildPrev() error {
	if m.prev == nil {
		return nil
	}

	if err := m.buildTSDBFromHead(m.prevHeads); err != nil {
		return err
	}

	// Now that the tsdbManager has the updated TSDBs, we can remove our references
	m.mtx.Lock()
	defer m.mtx.Unlock()
	// We nil-out the previous wal to signal that we've built the TSDBs from it successfully.
	// We don't nil-out the heads because we need to keep the them around
	// in order to serve queries for the recently rotated out period until
	// the index-gws|queriers have time to download the new TSDBs
	m.prev = nil

	return nil
}

// tick handles one iteration for `loop()`. It builds new heads,
// cleans up previous heads, and performs rotations.
func (m *HeadManager) tick(now time.Time) {
	// retry tsdb build failures from previous run
	if err := m.buildPrev(); err != nil {
		level.Error(m.log).Log(
			"msg", "failed building tsdb head",
			"period", m.period.PeriodFor(m.prev.initialized),
			"err", err,
		)
		// rotating head without building prev would result in loss of index for that period (until restart)
		return
	}

	if activePeriod := m.period.PeriodFor(m.activeHeads.start); m.period.PeriodFor(now) > activePeriod {
		if err := m.Rotate(now); err != nil {
			m.metrics.headRotations.WithLabelValues(statusFailure).Inc()
			level.Error(m.log).Log(
				"msg", "failed rotating tsdb head",
				"period", activePeriod,
				"err", err,
			)
			return
		}
		m.metrics.headRotations.WithLabelValues(statusSuccess).Inc()
	}

	// build tsdb from rotated-out period
	if err := m.buildPrev(); err != nil {
		level.Error(m.log).Log(
			"msg", "failed building tsdb head",
			"period", m.period.PeriodFor(m.prev.initialized),
			"err", err,
		)
	}
}

func (m *HeadManager) loop() {
	defer m.wg.Done()

	ticker := time.NewTicker(defaultRotationCheckPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			m.tick(now)
		case <-m.cancel:
			return
		}
	}
}

func (m *HeadManager) Stop() error {
	close(m.cancel)
	m.wg.Wait()

	m.mtx.Lock()
	defer m.mtx.Unlock()
	if err := m.active.Stop(); err != nil {
		return err
	}

	if m.prev != nil {
		if err := m.buildTSDBFromHead(m.prevHeads); err != nil {
			// log the error and start building active head
			level.Error(m.log).Log(
				"msg", "failed building tsdb from prev head",
				"period", m.period.PeriodFor(m.prev.initialized),
				"err", err,
			)
		}
	}

	return m.buildTSDBFromHead(m.activeHeads)
}

func (m *HeadManager) Append(userID string, ls labels.Labels, fprint uint64, chks index.ChunkMetas) error {
	// TSDB doesnt need the __name__="log" convention the old chunk store index used.
	// We must create a copy of the labels here to avoid mutating the existing
	// labels when writing across index buckets.
	b := labels.NewBuilder(ls)
	b.Del(labels.MetricName)
	ls = b.Labels()

	m.mtx.RLock()
	defer m.mtx.RUnlock()

	rec := m.activeHeads.Append(userID, ls, fprint, chks)
	return m.active.Log(rec)
}

func (m *HeadManager) Start() error {
	if err := os.RemoveAll(filepath.Join(m.dir, "scratch")); err != nil {
		return errors.Wrap(err, "removing tsdb scratch dir")
	}

	for _, d := range managerRequiredDirs(m.name, m.dir) {
		if err := util.EnsureDirectory(d); err != nil {
			return errors.Wrapf(err, "ensuring required directory exists: %s", d)
		}
	}

	// build tsdb from legacy WALs in the common wal dir, these would've been generated before the TSDB multi-store support was added.
	if err := m.buildTSDBFromWALs(true); err != nil {
		return errors.Wrap(err, "building tsdb from legacy WAL files")
	}

	// Load the shipper with any previously built TSDBs
	if err := m.tsdbManager.Start(); err != nil {
		return errors.Wrap(err, "failed to start tsdb manager")
	}

	// build tsdb from store specific WAL files
	if err := m.buildTSDBFromWALs(false); err != nil {
		return errors.Wrap(err, "building tsdb from old WAL files")
	}

	err := m.Rotate(time.Now())
	if err != nil {
		return errors.Wrap(err, "rotating tsdb head")
	}

	m.wg.Add(1)
	go m.loop()

	return nil
}

func (m *HeadManager) buildTSDBFromWALs(legacy bool) error {
	walDir := managerWalDir(m.name, m.dir)
	if legacy {
		walDir = managerLegacyWalDir(m.dir)
	}

	walsByPeriod, err := walsByPeriod(walDir, m.period)
	if err != nil {
		return errors.Wrap(err, "loading wals by period")
	}
	level.Info(m.log).Log("msg", "loaded wals by period", "groups", len(walsByPeriod))

	// Build any old WALs into a TSDB for the shipper
	var allWALs []WALIdentifier
	for _, group := range walsByPeriod {
		allWALs = append(allWALs, group.wals...)
	}

	now := time.Now()
	if err := m.tsdbManager.BuildFromWALs(
		now,
		allWALs,
		legacy,
	); err != nil {
		return errors.Wrap(err, "building tsdb from WALs")
	}

	if legacy {
		for _, grp := range walsByPeriod {
			if err := m.removeLegacyWALGroup(grp); err != nil {
				return errors.Wrapf(err, "removing legacy TSDB WALs for period %d", grp.period)
			}
		}
	} else {
		if err := os.RemoveAll(managerWalDir(m.name, m.dir)); err != nil {
			m.metrics.walTruncations.WithLabelValues(statusFailure).Inc()
			return errors.Wrap(err, "cleaning (removing) wal dir")
		}
		m.metrics.walTruncations.WithLabelValues(statusSuccess).Inc()
	}

	return nil
}

func managerRequiredDirs(name, parent string) []string {
	return []string{
		managerScratchDir(parent),
		managerWalDir(name, parent),
		managerMultitenantDir(parent),
		managerPerTenantDir(parent),
	}
}
func managerScratchDir(parent string) string {
	return filepath.Join(parent, "scratch")
}

func managerWalDir(name, parent string) string {
	return filepath.Join(parent, "wal", name)
}

func managerLegacyWalDir(parent string) string {
	return filepath.Join(parent, "wal")
}

func managerMultitenantDir(parent string) string {
	return filepath.Join(parent, "multitenant")
}

func managerPerTenantDir(parent string) string {
	return filepath.Join(parent, "per_tenant")
}

func (m *HeadManager) Rotate(t time.Time) (err error) {
	// create new wal
	nextWALPath := walPath(m.name, m.dir, t)
	nextWAL, err := newHeadWAL(m.log, nextWALPath, t)
	if err != nil {
		return errors.Wrapf(err, "creating tsdb wal: %s during rotation", nextWALPath)
	}

	// create new tenant heads
	nextHeads := newTenantHeads(t, m.shards, m.metrics, m.log)

	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.prev = m.active
	m.prevHeads = m.activeHeads
	m.active = nextWAL
	m.activeHeads = nextHeads

	// stop the newly rotated-out wal
	if m.prev != nil {
		if err := m.prev.Stop(); err != nil {
			level.Error(m.log).Log(
				"msg", "failed stopping wal",
				"period", m.period.PeriodFor(m.prev.initialized),
				"err", err,
			)
		}
	}

	return nil
}

func (m *HeadManager) buildTSDBFromHead(head *tenantHeads) error {
	period := m.period.PeriodFor(head.start)
	if err := m.tsdbManager.BuildFromHead(head); err != nil {
		return errors.Wrap(err, "building tsdb from head")
	}

	// Now that a TSDB has been created from this group, it's safe to remove them
	if err := m.truncateWALForPeriod(period); err != nil {
		level.Error(m.log).Log(
			"msg", "failed truncating wal files",
			"period", period,
			"err", err,
		)
	}

	return nil
}

func (m *HeadManager) truncateWALForPeriod(period int) (err error) {
	defer func() {
		status := statusSuccess
		if err != nil {
			status = statusFailure
		}

		m.metrics.walTruncations.WithLabelValues(status).Inc()
	}()

	grp, _, err := walsForPeriod(managerWalDir(m.name, m.dir), m.period, period)
	if err != nil {
		return errors.Wrap(err, "listing wals")
	}
	level.Debug(m.log).Log("msg", "listed WALs", "pd", grp.period, "n", len(grp.wals))

	if err := m.removeWALGroup(grp); err != nil {
		return errors.Wrapf(err, "removing TSDB WALs for period %d", grp.period)
	}
	level.Debug(m.log).Log("msg", "removing wals", "pd", grp.period, "n", len(grp.wals))

	return nil
}

type WalGroup struct {
	period int
	wals   []WALIdentifier
}

func walsByPeriod(dir string, period period) ([]WalGroup, error) {
	groupsMap, err := walGroups(dir, period)
	if err != nil {
		return nil, err
	}
	res := make([]WalGroup, 0, len(groupsMap))
	for _, grp := range groupsMap {
		res = append(res, *grp)
	}
	// Ensure the earliers periods are seen first
	sort.Slice(res, func(i, j int) bool {
		return res[i].period < res[j].period
	})
	return res, nil
}

func walGroups(dir string, period period) (map[int]*WalGroup, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	groupsMap := map[int]*WalGroup{}

	for _, f := range files {
		if id, ok := parseWALPath(f.Name()); ok {
			pd := period.PeriodFor(id.ts)
			grp, ok := groupsMap[pd]
			if !ok {
				grp = &WalGroup{
					period: pd,
				}
				groupsMap[pd] = grp
			}
			grp.wals = append(grp.wals, id)
		}
	}

	for _, grp := range groupsMap {
		// Ensure the earliest wals are seen first
		sort.Slice(grp.wals, func(i, j int) bool {
			return grp.wals[i].ts.Before(grp.wals[j].ts)
		})
	}
	return groupsMap, nil
}

func walsForPeriod(dir string, period period, offset int) (WalGroup, bool, error) {
	groupsMap, err := walGroups(dir, period)
	if err != nil {
		return WalGroup{}, false, err
	}

	grp, ok := groupsMap[offset]
	if !ok {
		return WalGroup{}, false, nil
	}

	return *grp, true, nil
}

func (m *HeadManager) removeWALGroup(grp WalGroup) error {
	for _, wal := range grp.wals {
		if err := os.RemoveAll(walPath(m.name, m.dir, wal.ts)); err != nil {
			return errors.Wrapf(err, "removing tsdb wal: %s", walPath(m.name, m.dir, wal.ts))
		}
	}
	return nil
}

func (m *HeadManager) removeLegacyWALGroup(grp WalGroup) error {
	for _, wal := range grp.wals {
		if err := os.RemoveAll(legacyWalPath(m.dir, wal.ts)); err != nil {
			return errors.Wrapf(err, "removing tsdb wal: %s", legacyWalPath(m.dir, wal.ts))
		}
	}
	return nil
}

func walPath(name, parent string, t time.Time) string {
	return filepath.Join(
		managerWalDir(name, parent),
		fmt.Sprintf("%d", t.Unix()),
	)
}

func legacyWalPath(parent string, t time.Time) string {
	return filepath.Join(
		managerLegacyWalDir(parent),
		fmt.Sprintf("%d", t.Unix()),
	)
}

// recoverHead recovers from all WALs belonging to some period
// and inserts it into the active *tenantHeads
func recoverHead(name, dir string, heads *tenantHeads, wals []WALIdentifier, legacy bool) error {
	for _, id := range wals {
		// use anonymous function for ease of cleanup
		if err := func(id WALIdentifier) error {
			walPath := walPath(name, dir, id.ts)
			if legacy {
				walPath = legacyWalPath(dir, id.ts)
			}

			reader, closer, err := wal.NewWalReader(walPath, -1)
			if err != nil {
				return err
			}
			defer closer.Close()

			// map of users -> ref -> series.
			// Keep track of which ref corresponds to which series
			// for each WAL so we replay into the correct series
			type labelsWithFp struct {
				ls labels.Labels
				fp uint64
			}
			seriesMap := make(map[string]map[uint64]*labelsWithFp)

			for reader.Next() {
				rec := &WALRecord{}
				if err := decodeWALRecord(reader.Record(), rec); err != nil {
					return err
				}

				// labels are always written to the WAL before corresponding chunks
				if len(rec.Series.Labels) > 0 {
					tenant, ok := seriesMap[rec.UserID]
					if !ok {
						tenant = make(map[uint64]*labelsWithFp)
						seriesMap[rec.UserID] = tenant
					}
					tenant[uint64(rec.Series.Ref)] = &labelsWithFp{
						ls: rec.Series.Labels,
						fp: rec.Fingerprint,
					}
				}

				if len(rec.Chks.Chks) > 0 {
					tenant, ok := seriesMap[rec.UserID]
					if !ok {
						return fmt.Errorf("found tsdb chunk metas without user in WAL replay (period=%s): %+v", id.String(), *rec)
					}
					x, ok := tenant[rec.Chks.Ref]
					if !ok {
						return fmt.Errorf("found tsdb chunk metas without series in WAL replay (period=%s): %+v", id.String(), *rec)
					}
					_ = heads.Append(rec.UserID, x.ls, x.fp, rec.Chks.Chks)
				}
			}
			return reader.Err()

		}(id); err != nil {
			return errors.Wrap(
				err,
				"error recovering from TSDB WAL",
			)
		}
	}
	return nil
}

type WALIdentifier struct {
	ts time.Time
}

func (w WALIdentifier) String() string {
	return fmt.Sprint(w.ts.Unix())
}

func parseWALPath(p string) (id WALIdentifier, ok bool) {
	ts, err := strconv.Atoi(p)
	if err != nil {
		return
	}

	return WALIdentifier{
		ts: time.Unix(int64(ts), 0),
	}, true
}

type tenantHeads struct {
	mint, maxt atomic.Int64 // easy lookup for Bounds() impl

	start       time.Time
	shards      int
	locks       []sync.RWMutex
	tenants     []map[string]*Head
	log         log.Logger
	chunkFilter chunk.RequestChunkFilterer
	metrics     *Metrics
}

func newTenantHeads(start time.Time, shards int, metrics *Metrics, logger log.Logger) *tenantHeads {
	res := &tenantHeads{
		start:   start,
		shards:  shards,
		locks:   make([]sync.RWMutex, shards),
		tenants: make([]map[string]*Head, shards),
		log:     log.With(logger, "component", "tenant-heads"),
		metrics: metrics,
	}
	for i := range res.tenants {
		res.tenants[i] = make(map[string]*Head)
	}
	return res
}

func (t *tenantHeads) Append(userID string, ls labels.Labels, fprint uint64, chks index.ChunkMetas) *WALRecord {
	var mint, maxt int64
	for _, chk := range chks {
		if chk.MinTime < mint || mint == 0 {
			mint = chk.MinTime
		}

		if chk.MaxTime > maxt {
			maxt = chk.MaxTime
		}
	}
	updateMintMaxt(mint, maxt, &t.mint, &t.maxt)

	head := t.getOrCreateTenantHead(userID)
	newStream, refID := head.Append(ls, fprint, chks)

	rec := &WALRecord{
		UserID: userID,
		Chks: ChunkMetasRecord{
			Ref:  refID,
			Chks: chks,
		},
	}

	if newStream {
		rec.Fingerprint = fprint
		rec.Series = record.RefSeries{
			Ref:    chunks.HeadSeriesRef(refID),
			Labels: ls,
		}
	}

	return rec
}

func (t *tenantHeads) getOrCreateTenantHead(userID string) *Head {
	idx := t.shardForTenant(userID)
	mtx := &t.locks[idx]

	// return existing tenant head if it exists
	mtx.RLock()
	head, ok := t.tenants[idx][userID]
	mtx.RUnlock()
	if ok {
		return head
	}

	mtx.Lock()
	defer mtx.Unlock()

	// tenant head was not found before.
	// Check again if a competing request created the head already, don't create it again if so.
	head, ok = t.tenants[idx][userID]
	if !ok {
		head = NewHead(userID, t.metrics, t.log)
		t.tenants[idx][userID] = head
	}

	return head
}

func (t *tenantHeads) shardForTenant(userID string) uint64 {
	return xxhash.Sum64String(userID) & uint64(t.shards-1)
}

func (t *tenantHeads) Close() error { return nil }

func (t *tenantHeads) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	t.chunkFilter = chunkFilter
}

func (t *tenantHeads) Bounds() (model.Time, model.Time) {
	return model.Time(t.mint.Load()), model.Time(t.maxt.Load())
}

func (t *tenantHeads) tenantIndex(userID string, from, through model.Time) (idx Index, ok bool) {
	i := t.shardForTenant(userID)
	t.locks[i].RLock()
	defer t.locks[i].RUnlock()
	tenant, ok := t.tenants[i][userID]
	if !ok {
		return
	}

	idx = NewTSDBIndex(tenant.indexRange(int64(from), int64(through)))
	if t.chunkFilter != nil {
		idx.SetChunkFilterer(t.chunkFilter)
	}
	return idx, true

}

func (t *tenantHeads) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, _ []ChunkRef, fpFilter index.FingerprintFilter, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	idx, ok := t.tenantIndex(userID, from, through)
	if !ok {
		return nil, nil
	}
	return idx.GetChunkRefs(ctx, userID, from, through, nil, fpFilter, matchers...)

}

// Series follows the same semantics regarding the passed slice and shard as GetChunkRefs.
func (t *tenantHeads) Series(ctx context.Context, userID string, from, through model.Time, _ []Series, fpFilter index.FingerprintFilter, matchers ...*labels.Matcher) ([]Series, error) {
	idx, ok := t.tenantIndex(userID, from, through)
	if !ok {
		return nil, nil
	}
	return idx.Series(ctx, userID, from, through, nil, fpFilter, matchers...)

}

func (t *tenantHeads) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	idx, ok := t.tenantIndex(userID, from, through)
	if !ok {
		return nil, nil
	}
	return idx.LabelNames(ctx, userID, from, through, matchers...)

}

func (t *tenantHeads) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	idx, ok := t.tenantIndex(userID, from, through)
	if !ok {
		return nil, nil
	}
	return idx.LabelValues(ctx, userID, from, through, name, matchers...)

}

func (t *tenantHeads) Stats(ctx context.Context, userID string, from, through model.Time, acc IndexStatsAccumulator, fpFilter index.FingerprintFilter, shouldIncludeChunk shouldIncludeChunk, matchers ...*labels.Matcher) error {
	idx, ok := t.tenantIndex(userID, from, through)
	if !ok {
		return nil
	}
	return idx.Stats(ctx, userID, from, through, acc, fpFilter, shouldIncludeChunk, matchers...)
}

func (t *tenantHeads) Volume(ctx context.Context, userID string, from, through model.Time, acc VolumeAccumulator, fpFilter index.FingerprintFilter, shouldIncludeChunk shouldIncludeChunk, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) error {
	idx, ok := t.tenantIndex(userID, from, through)
	if !ok {
		return nil
	}
	return idx.Volume(ctx, userID, from, through, acc, fpFilter, shouldIncludeChunk, targetLabels, aggregateBy, matchers...)
}

func (t *tenantHeads) ForSeries(ctx context.Context, userID string, fpFilter index.FingerprintFilter, from model.Time, through model.Time, fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta) (stop bool), matchers ...*labels.Matcher) error {
	idx, ok := t.tenantIndex(userID, from, through)
	if !ok {
		return nil
	}
	return idx.ForSeries(ctx, userID, fpFilter, from, through, fn, matchers...)
}

// helper only used in building TSDBs
func (t *tenantHeads) forAll(fn func(user string, ls labels.Labels, fp uint64, chks index.ChunkMetas) error) error {
	for i, shard := range t.tenants {
		t.locks[i].RLock()
		defer t.locks[i].RUnlock()

		for user, tenant := range shard {
			idx := tenant.Index()
			ps, err := postingsForMatcher(idx, nil, labels.MustNewMatcher(labels.MatchEqual, "", ""))
			if err != nil {
				return err
			}

			for ps.Next() {
				var (
					ls   labels.Labels
					chks []index.ChunkMeta
				)

				fp, err := idx.Series(ps.At(), 0, math.MaxInt64, &ls, &chks)

				if err != nil {
					return errors.Wrapf(err, "iterating postings for tenant: %s", user)
				}

				if err := fn(user, ls, fp, chks); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
