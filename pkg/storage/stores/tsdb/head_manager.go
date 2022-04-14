package tsdb

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cespare/xxhash"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"

	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

const defaultRotationPeriod = 15 * time.Minute

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
     # scratch directory used for temp tsdb files during build stage
     scratch/
     wal/
		 <timestamp>-<ingester-name>
	 # multitenant tsdb files which are created on the ingesters/shipped
     multitenant/
	             # contains built TSDBs
	             built/
				       <timestamp>-<ingester-name>.tsdb
	             # once shipped successfully, they're moved here and can be safely deleted later
	             shipped/
				         <timestamp>-<ingester-name>.tsdb
	 compacted/
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
	metrics *HeadMetrics

	// RLocked for all writes/reads,
	// Locked before rotating heads/wal
	mtx sync.RWMutex

	// how often WALs should be rotated and TSDBs cut
	period time.Duration

	tsdbManager  TSDBManager
	active, prev *headWAL

	shards                 int
	activeHeads, prevHeads *tenantHeads
}

func NewHeadManager(log log.Logger, dir string, reg prometheus.Registerer, name string, tsdbManager TSDBManager) *HeadManager {
	shards := defaultHeadManagerStripeSize
	metrics := NewHeadMetrics(reg)
	return &HeadManager{
		name:        name,
		log:         log,
		dir:         dir,
		metrics:     metrics,
		tsdbManager: tsdbManager,

		period: defaultRotationPeriod,
		shards: shards,
	}

}

func (m *HeadManager) Append(userID string, ls labels.Labels, chks index.ChunkMetas) error {
	m.mtx.RLock()
	now := time.Now()
	if m.PeriodFor(now) > m.PeriodFor(m.activeHeads.start) {
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

func (m *HeadManager) PeriodFor(t time.Time) int {
	return int(t.UnixNano() / int64(m.period))
}

func (m *HeadManager) TimeForPeriod(period int) time.Time {
	return time.Unix(0, int64(m.period)*int64(period))
}

func (m *HeadManager) Start() error {
	if err := os.RemoveAll(filepath.Join(m.dir, "scratch")); err != nil {
		return errors.Wrap(err, "removing tsdb scratch dir")
	}

	for _, d := range m.RequiredDirs() {
		if err := util.EnsureDirectory(d); err != nil {
			return errors.Wrapf(err, "ensuring required directory exists: %s", d)
		}
	}

	now := time.Now()
	curPeriod := m.PeriodFor(now)

	toRemove, err := m.shippedTSDBsBeforePeriod(curPeriod)
	if err != nil {
		return err
	}

	for _, x := range toRemove {
		if err := os.RemoveAll(x); err != nil {
			return errors.Wrapf(err, "removing tsdb: %s", x)
		}
	}

	walsByPeriod, err := m.walsByPeriod()
	if err != nil {
		return err
	}

	m.activeHeads = newTenantHeads(now, m.shards, m.metrics, m.log)

	for _, group := range walsByPeriod {
		if group.period < (curPeriod) {
			if err := m.tsdbManager.BuildFromWALs(
				m.TimeForPeriod(group.period),
				group.wals,
			); err != nil {
				return errors.Wrap(err, "building tsdb")
			}
			// Now that we've built tsdbs of this data, we can safely remove the WALs
			if err := m.removeWALGroup(group); err != nil {
				return errors.Wrapf(err, "removing wals for period %d", group.period)
			}
		}

		if group.period == curPeriod {
			if err := m.recoverHead(group); err != nil {
				return errors.Wrap(err, "recovering tsdb head from wal")
			}
		}
	}

	nextWALPath := m.walPath(now)
	nextWAL, err := newHeadWAL(m.log, nextWALPath, now)
	if err != nil {
		return errors.Wrapf(err, "creating tsdb wal: %s", nextWALPath)
	}
	m.active = nextWAL

	return nil
}

func (m *HeadManager) RequiredDirs() []string {
	return []string{
		m.scratchDir(),
		m.walDir(),
		m.builtDir(),
		m.shippedDir(),
	}
}
func (m *HeadManager) scratchDir() string { return filepath.Join(m.dir, "scratch") }
func (m *HeadManager) walDir() string     { return filepath.Join(m.dir, "wal") }
func (m *HeadManager) builtDir() string   { return filepath.Join(m.dir, "multitenant", "built") }
func (m *HeadManager) shippedDir() string { return filepath.Join(m.dir, "multitenant", "shipped") }

func (m *HeadManager) Rotate(t time.Time) error {
	// create new wal
	nextWALPath := m.walPath(t)
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
					"period", m.PeriodFor(m.prev.initialized),
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
	grp, _, err := m.walsForPeriod(m.PeriodFor(m.prev.initialized))
	if err != nil {
		return errors.Wrap(err, "listing wals")
	}

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

	// Now that the tsdbManager has the updated TSDBs, we can remove our references
	m.mtx.Lock()
	m.prevHeads = nil
	m.prev = nil
	m.mtx.Unlock()
	return nil
}

func (m *HeadManager) shippedTSDBsBeforePeriod(period int) (res []string, err error) {
	files, err := ioutil.ReadDir(m.shippedDir())
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if id, ok := parseTSDBPath(f.Name()); ok {
			if found := m.PeriodFor(id.ts); found < period {
				res = append(res, f.Name())
			}
		}
	}
	return
}

type walGroup struct {
	period int
	wals   []WALIdentifier
}

func (m *HeadManager) walsByPeriod() ([]walGroup, error) {

	groupsMap, err := m.walGroups()
	if err != nil {
		return nil, err
	}
	res := make([]walGroup, 0, len(groupsMap))
	for _, grp := range groupsMap {
		res = append(res, *grp)
	}
	// Ensure the earliers periods are seen first
	sort.Slice(res, func(i, j int) bool {
		return res[i].period < res[j].period
	})
	return res, nil
}

func (m *HeadManager) walGroups() (map[int]*walGroup, error) {
	files, err := ioutil.ReadDir(m.walDir())
	if err != nil {
		return nil, err
	}

	groupsMap := map[int]*walGroup{}

	for _, f := range files {
		if id, ok := parseWALPath(f.Name()); ok {
			pd := m.PeriodFor(id.ts)
			grp, ok := groupsMap[pd]
			if !ok {
				grp := walGroup{
					period: pd,
				}
				groupsMap[pd] = &grp
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

func (m *HeadManager) walsForPeriod(period int) (walGroup, bool, error) {
	groupsMap, err := m.walGroups()
	if err != nil {
		return walGroup{}, false, err
	}

	grp, ok := groupsMap[period]
	if !ok {
		return walGroup{}, false, nil
	}

	return *grp, true, nil
}

func (m *HeadManager) removeWALGroup(grp walGroup) error {
	for _, wal := range grp.wals {
		if err := os.RemoveAll(m.walPath(wal.ts)); err != nil {
			return errors.Wrapf(err, "removing tsdb wal: %s", m.walPath(wal.ts))
		}
	}
	return nil
}

func (m *HeadManager) walPath(t time.Time) string {
	return filepath.Join(
		m.walDir(),
		fmt.Sprintf("%d-%s", t.Unix(), m.name),
	)
}

// recoverHead recovers from all WALs belonging to some period
// and inserts it into the active *tenantHeads
func (m *HeadManager) recoverHead(grp walGroup) error {
	for _, id := range grp.wals {

		// use anonymous function for ease of cleanup
		if err := func() error {
			reader, closer, err := ingester.NewWalReader(m.walPath(id.ts), -1)
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
					_ = m.activeHeads.Append(rec.UserID, ls, rec.Chks.Chks)
				}
			}
			return reader.Err()

		}(); err != nil {
			return errors.Wrapf(
				err,
				"error recovering from TSDB WAL: %s",
				m.walPath(id.ts),
			)
		}
	}
	return nil
}

type WALIdentifier struct {
	nodeName string
	ts       time.Time
}
type MultitenantTSDBIdentifier WALIdentifier

func parseWALPath(p string) (id WALIdentifier, ok bool) {
	xs := strings.Split(p, "-")
	if len(xs) != 2 {
		return
	}

	// require node name isn't empty
	if len(xs[1]) == 0 {
		return
	}

	period, err := strconv.Atoi(xs[0])
	if err != nil {
		return
	}

	return WALIdentifier{
		ts:       time.Unix(int64(period), 0),
		nodeName: xs[1],
	}, true
}

func parseTSDBPath(p string) (id MultitenantTSDBIdentifier, ok bool) {
	trimmed := strings.TrimSuffix(p, ".tsdb")

	// incorrect suffix
	if trimmed == p {
		return
	}

	if found, ok := parseWALPath(trimmed); ok {
		return MultitenantTSDBIdentifier(found), true
	}
	return
}

type tenantHeads struct {
	start   time.Time
	shards  int
	locks   []sync.RWMutex
	tenants []map[string]*Head
	log     log.Logger
	metrics *HeadMetrics
}

func newTenantHeads(start time.Time, shards int, metrics *HeadMetrics, log log.Logger) *tenantHeads {
	res := &tenantHeads{
		start:   start,
		shards:  shards,
		locks:   make([]sync.RWMutex, shards),
		tenants: make([]map[string]*Head, shards),
		log:     log,
		metrics: metrics,
	}
	for i := range res.tenants {
		res.tenants[i] = make(map[string]*Head)
	}
	return res
}

func (t *tenantHeads) Append(userID string, ls labels.Labels, chks index.ChunkMetas) *WALRecord {
	idx := xxhash.Sum64String(userID) & uint64(t.shards-1)

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
