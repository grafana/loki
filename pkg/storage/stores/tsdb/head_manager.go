package tsdb

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/cespare/xxhash"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
)

const defaultRotationPeriod = 2 * time.Hour

// Do not specify without bit shifting. This allows us to
// do shard index calcuations via bitwise & rather than modulos.
const defaultHeadManagerStripeSize = 1 << 7

// HeadManager both accepts flushed chunk writes
// and exposes the index interface for multiple tenants.
// It also handles updating an underlying WAL and periodically
// rotates both the tenant Heads and the underlying WAL, using
// the old versions to build TSDB files.
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

	active *headWAL

	shards      int
	activeHeads *tenantHeads
	oldHeads    []*tenantHeads

	readyCh chan struct{}
	flusher flusher
}

func NewHeadManager(log log.Logger, dir string, reg prometheus.Registerer, name string, flusher flusher) *HeadManager {
	shards := 128
	metrics := NewHeadMetrics(reg)
	return &HeadManager{
		name:    name,
		log:     log,
		dir:     dir,
		metrics: metrics,

		period:  defaultRotationPeriod,
		shards:  shards,
		flusher: flusher,
		readyCh: make(chan struct{}),
	}

}

func (m *HeadManager) Run() {
	// Run once immediately
	if err := m.Rotate(); err != nil {
		level.Error(m.log).Log("msg", "failed to rotate TSDB WAL", "error", err.Error())
	}

	ticker := time.NewTicker(m.period)
	for {
		<-ticker.C
		if err := m.Rotate(); err != nil {
			level.Error(m.log).Log("msg", "failed to rotate TSDB WAL", "error", err.Error())
		}
	}
}

func (m *HeadManager) Ready() <-chan struct{} { return m.readyCh }

func (m *HeadManager) ensureReady() {
	// if m wasn't ready before, mark it ready
	select {
	case <-m.Ready():
	default:
		close(m.readyCh)
	}
}

func (m *HeadManager) Append(userID string, ls labels.Labels, chks index.ChunkMetas) error {
	m.mtx.RLock()
	defer m.mtx.RUnlock()
	rec := m.activeHeads.Append(userID, ls, chks)
	return m.active.Log(rec)
}

/*
Rotate starts using a new WAL and builds a TSDB index from the old one.
Once a WAL is shipped successfully to storage, it's moved into the shipped directory.
tsdb/
     wal/
	     pending/
		         <period>-<ingester-name>
	     shipped/
		         <period>-<ingester-name>
*/
func (m *HeadManager) Rotate() (err error) {
	// First, set up the next WAL

	t := time.Now()
	nextPeriod := t.UnixNano() / m.period.Nanoseconds()

	if m.activeHeads != nil && m.activeHeads.period == nextPeriod {
		return nil
	}

	nextWAL, err := newHeadWAL(m.log, m.pendingPath(nextPeriod))
	if err != nil {
		return err
	}
	if err := nextWAL.Start(t); err != nil {
		return err
	}

	nextHeads := newTenantHeads(nextPeriod, m.shards, m.metrics, m.log)

	m.mtx.Lock()
	oldWAL := m.active
	oldHeads := m.activeHeads

	m.active = nextWAL
	m.activeHeads = nextHeads
	m.mtx.Unlock()

	m.ensureReady()

	if err := oldWAL.Stop(); err != nil {
		return err
	}

	return m.flusher.Flush(oldHeads.period, oldWAL)
}

// TODO(owen-d): placeholder for shipper
type flusher interface {
	Flush(period int64, wal *headWAL) error
}

func (m *HeadManager) pendingPath(period int64) string {
	return filepath.Join(m.dir, "pending", fmt.Sprintf("%d-%s", period, m.name))
}

func (m *HeadManager) shippedPath(period int64) string {
	return filepath.Join(m.dir, "shipped", fmt.Sprintf("%d-%s", period, m.name))
}

type tenantHeads struct {
	period  int64
	shards  int
	locks   []sync.RWMutex
	tenants []map[string]*Head
	log     log.Logger
	metrics *HeadMetrics
}

func newTenantHeads(period int64, shards int, metrics *HeadMetrics, log log.Logger) *tenantHeads {
	res := &tenantHeads{
		period:  period,
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
