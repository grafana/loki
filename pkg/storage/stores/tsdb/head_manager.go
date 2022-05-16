package tsdb

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/cespare/xxhash"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/grafana/loki/pkg/util/wal"
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

const defaultRotationPeriod = period(15 * time.Minute)

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
}

func NewHeadManager(logger log.Logger, dir string, metrics *Metrics, tsdbManager TSDBManager) *HeadManager {
	shards := defaultHeadManagerStripeSize
	m := &HeadManager{
		log:         log.With(logger, "component", "tsdb-head-manager"),
		dir:         dir,
		metrics:     metrics,
		tsdbManager: tsdbManager,

		period: defaultRotationPeriod,
		shards: shards,
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

		indices = append(indices, m.tsdbManager)

		return NewMultiIndex(indices...)

	})

	return m
}

func (m *HeadManager) Stop() error {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.active.Stop()
}

func (m *HeadManager) Append(userID string, ls labels.Labels, chks index.ChunkMetas) error {
	// TSDB doesnt need the __name__="log" convention the old chunk store index used.
	// We must create a copy of the labels here to avoid mutating the existing
	// labels when writing across index buckets.
	b := labels.NewBuilder(ls)
	b.Del(labels.MetricName)
	ls = b.Labels()

	m.mtx.RLock()
	now := time.Now()
	if m.period.PeriodFor(now) > m.period.PeriodFor(m.activeHeads.start) {
		m.mtx.RUnlock()
		if err := m.Rotate(now); err != nil {
			return errors.Wrap(err, "rotating TSDB Head")
		}
		m.mtx.RLock()
	}
	defer m.mtx.RUnlock()
	rec := m.activeHeads.Append(userID, ls, chks)
	return m.active.Log(rec)
}

func (m *HeadManager) Start() error {
	if err := os.RemoveAll(filepath.Join(m.dir, "scratch")); err != nil {
		return errors.Wrap(err, "removing tsdb scratch dir")
	}

	for _, d := range managerRequiredDirs(m.dir) {
		if err := util.EnsureDirectory(d); err != nil {
			return errors.Wrapf(err, "ensuring required directory exists: %s", d)
		}
	}

	now := time.Now()
	curPeriod := m.period.PeriodFor(now)

	walsByPeriod, err := walsByPeriod(m.dir, m.period)
	if err != nil {
		return err
	}
	level.Info(m.log).Log("msg", "loaded wals by period", "groups", len(walsByPeriod))

	m.activeHeads = newTenantHeads(now, m.shards, m.metrics, m.log)

	// Load the shipper with any previously built TSDBs
	if err := m.tsdbManager.Start(); err != nil {
		return errors.Wrap(err, "failed to start tsdb manager")
	}

	// Build any old WALs into TSDBs for the shipper
	for _, group := range walsByPeriod {
		if group.period < curPeriod {
			if err := m.tsdbManager.BuildFromWALs(
				m.period.TimeForPeriod(group.period),
				group.wals,
			); err != nil {
				return errors.Wrap(err, "building tsdb")
			}
			// Now that we've built tsdbs of this data, we can safely remove the WALs
			if err := m.removeWALGroup(group); err != nil {
				return errors.Wrapf(err, "removing wals for period %d", group.period)
			}
		}

		if group.period >= curPeriod {
			if err := recoverHead(m.dir, m.activeHeads, group.wals); err != nil {
				return errors.Wrap(err, "recovering tsdb head from wal")
			}
		}
	}

	nextWALPath := walPath(m.dir, now)
	nextWAL, err := newHeadWAL(m.log, nextWALPath, now)
	if err != nil {
		return errors.Wrapf(err, "creating tsdb wal: %s", nextWALPath)
	}
	m.active = nextWAL

	return nil
}

func managerRequiredDirs(parent string) []string {
	return []string{
		managerScratchDir(parent),
		managerWalDir(parent),
		managerMultitenantDir(parent),
		managerPerTenantDir(parent),
	}
}
func managerScratchDir(parent string) string {
	return filepath.Join(parent, "scratch")
}

func managerWalDir(parent string) string {
	return filepath.Join(parent, "wal")
}

func managerMultitenantDir(parent string) string {
	return filepath.Join(parent, "multitenant")
}

func managerPerTenantDir(parent string) string {
	return filepath.Join(parent, "per_tenant")
}

func (m *HeadManager) Rotate(t time.Time) error {
	// create new wal
	nextWALPath := walPath(m.dir, t)
	nextWAL, err := newHeadWAL(m.log, nextWALPath, t)
	if err != nil {
		return errors.Wrapf(err, "creating tsdb wal: %s during rotation", nextWALPath)
	}

	// create new tenant heads
	nextHeads := newTenantHeads(t, m.shards, m.metrics, m.log)

	stopPrev := func(s string) {
		if m.prev != nil {
			if err := m.prev.Stop(); err != nil {
				level.Error(m.log).Log(
					"msg", "failed stopping wal",
					"period", m.period.PeriodFor(m.prev.initialized),
					"err", err,
					"wal", s,
				)
			}
		}
	}

	stopPrev("previous cycle") // stop the previous wal if it hasn't been cleaned up yet
	m.mtx.Lock()
	m.prev = m.active
	m.prevHeads = m.activeHeads
	m.active = nextWAL
	m.activeHeads = nextHeads
	m.mtx.Unlock()
	stopPrev("freshly rotated") // stop the newly rotated-out wal

	// build tsdb from rotated-out period
	// TODO(owen-d): don't block Append() waiting for tsdb building. Use a work channel/etc
	if m.prev != nil {
		level.Debug(m.log).Log("msg", "combining tsdb WALs")
		grp, _, err := walsForPeriod(m.dir, m.period, m.period.PeriodFor(m.prev.initialized))
		if err != nil {
			return errors.Wrap(err, "listing wals")
		}
		level.Debug(m.log).Log("msg", "listed WALs", "pd", grp.period, "n", len(grp.wals))

		// TODO(owen-d): It's probably faster to build this from the *tenantHeads instead,
		// but we already need to impl BuildFromWALs to ensure we can correctly build/ship
		// TSDBs from orphaned WALs of previous periods during startup.
		// we use the m.prev.initialized timestamp here for the filename to ensure it can't clobber
		// an existing file from a previous cycle. I don't think this is possible, but
		// perhaps in some unusual crashlooping it could be, so let's be safe and protect ourselves.
		if err := m.tsdbManager.BuildFromWALs(m.prev.initialized, grp.wals); err != nil {
			return errors.Wrapf(err, "building TSDB from prevHeads WALs for period %d", grp.period)
		}

		// Now that a TSDB has been created from this group, it's safe to remove them
		if err := m.removeWALGroup(grp); err != nil {
			return errors.Wrapf(err, "removing prev TSDB WALs for period %d", grp.period)
		}
		level.Debug(m.log).Log("msg", "removing wals", "pd", grp.period, "n", len(grp.wals))

	}

	// Now that the tsdbManager has the updated TSDBs, we can remove our references
	m.mtx.Lock()
	m.prevHeads = nil
	m.prev = nil
	m.mtx.Unlock()
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
	files, err := ioutil.ReadDir(managerWalDir(dir))
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
		if err := os.RemoveAll(walPath(m.dir, wal.ts)); err != nil {
			return errors.Wrapf(err, "removing tsdb wal: %s", walPath(m.dir, wal.ts))
		}
	}
	return nil
}

func walPath(parent string, t time.Time) string {
	return filepath.Join(
		managerWalDir(parent),
		fmt.Sprintf("%d", t.Unix()),
	)
}

// recoverHead recovers from all WALs belonging to some period
// and inserts it into the active *tenantHeads
func recoverHead(dir string, heads *tenantHeads, wals []WALIdentifier) error {
	for _, id := range wals {
		// use anonymous function for ease of cleanup
		if err := func(id WALIdentifier) error {
			reader, closer, err := wal.NewWalReader(walPath(dir, id.ts), -1)
			if err != nil {
				return err
			}
			defer closer.Close()

			// map of users -> ref -> series.
			// Keep track of which ref corresponds to which series
			// for each WAL so we replay into the correct series
			seriesMap := make(map[string]map[uint64]labels.Labels)

			for reader.Next() {
				rec := &WALRecord{}
				if err := decodeWALRecord(reader.Record(), rec); err != nil {
					return err
				}

				// labels are always written to the WAL before corresponding chunks
				if len(rec.Series.Labels) > 0 {
					tenant, ok := seriesMap[rec.UserID]
					if !ok {
						tenant = make(map[uint64]labels.Labels)
						seriesMap[rec.UserID] = tenant
					}
					tenant[uint64(rec.Series.Ref)] = rec.Series.Labels
				}

				if len(rec.Chks.Chks) > 0 {
					tenant, ok := seriesMap[rec.UserID]
					if !ok {
						return errors.New("found tsdb chunk metas without user in WAL replay")
					}
					ls, ok := tenant[rec.Chks.Ref]
					if !ok {
						return errors.New("found tsdb chunk metas without series in WAL replay")
					}
					_ = heads.Append(rec.UserID, ls, rec.Chks.Chks)
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

func (t *tenantHeads) Append(userID string, ls labels.Labels, chks index.ChunkMetas) *WALRecord {
	idx := t.shardForTenant(userID)

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

	// First, check if this tenant has been created
	var (
		mtx       = &t.locks[idx]
		newStream bool
		refID     uint64
	)
	mtx.RLock()
	if head, ok := t.tenants[idx][userID]; ok {
		newStream, refID = head.Append(ls, chks)
		mtx.RUnlock()
	} else {
		// tenant does not exist, so acquire write lock to insert it
		mtx.RUnlock()
		mtx.Lock()
		head := NewHead(userID, t.metrics, t.log)
		t.tenants[idx][userID] = head
		newStream, refID = head.Append(ls, chks)
		mtx.Unlock()
	}

	rec := &WALRecord{
		UserID: userID,
		Chks: ChunkMetasRecord{
			Ref:  refID,
			Chks: chks,
		},
	}

	if newStream {
		rec.Series = record.RefSeries{
			Ref:    chunks.HeadSeriesRef(refID),
			Labels: ls,
		}
	}

	return rec
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

func (t *tenantHeads) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	idx, ok := t.tenantIndex(userID, from, through)
	if !ok {
		return nil, nil
	}
	return idx.GetChunkRefs(ctx, userID, from, through, nil, shard, matchers...)

}

// Series follows the same semantics regarding the passed slice and shard as GetChunkRefs.
func (t *tenantHeads) Series(ctx context.Context, userID string, from, through model.Time, res []Series, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error) {
	idx, ok := t.tenantIndex(userID, from, through)
	if !ok {
		return nil, nil
	}
	return idx.Series(ctx, userID, from, through, nil, shard, matchers...)

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

// helper only used in building TSDBs
func (t *tenantHeads) forAll(fn func(user string, ls labels.Labels, chks index.ChunkMetas)) error {
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

				_, err := idx.Series(ps.At(), &ls, &chks)

				if err != nil {
					return errors.Wrapf(err, "iterating postings for tenant: %s", user)
				}

				fn(user, ls, chks)
			}
		}
	}

	return nil
}
