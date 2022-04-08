package tsdb

import (
	"sync"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/storage/tsdb/index"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"go.uber.org/atomic"
)

/*
Disclaimer: This is largely inspired from Prometheus' TSDB Head, albeit
with significant changes (generally reductions rather than additions) to accommodate Loki
*/

const (
	// Note, this is significantly less than the stripe values used by Prometheus' stripeSeries.
	// This is for two reasons.
	// 1) Heads are per-tenant in Loki
	// 2) Loki tends to have a few orders of magnitude less series per node than
	// Prometheus|Cortex|Mimir.
	defaultStripeSize = 64
)

/*
Head is a per-tenant accumulator for index entries in memory.
It can be queried as an IndexReader and consumed to generate a TSDB index.
These are written to on the ingester component when chunks are flushed,
then written to disk as per tenant TSDB indices at the end of the WAL checkpointing cycle.
Every n cycles, they are compacted together and written to object storage.

In turn, many `Head`s may be wrapped into a multi-tenant head.
This allows Loki to serve `GetChunkRefs` requests for _chunks_ which have been flushed
whereas the corresponding index has not yet been uploaded to object storage,
guaranteeing we maintain querying consistency for the entire data lifecycle.
*/

// TODO(owen-d)
type HeadMetrics struct{}

func NewHeadMetrics(r prometheus.Registerer) *HeadMetrics {
	return &HeadMetrics{}
}

type Head struct {
	tenant           string
	numSeries        atomic.Uint64
	minTime, maxTime atomic.Int64 // Current min and max of the samples included in the head.

	// auto incrementing counter to uniquely identify series. This is also used
	// in the MemPostings, but is eventually discarded when we create a real TSDB index.
	lastSeriesID atomic.Uint64

	metrics *HeadMetrics
	logger  log.Logger

	series *stripeSeries

	postings *index.MemPostings // Postings lists for terms.

	closedMtx sync.Mutex
	closed    bool
}

func NewHead(tenant string, metrics *HeadMetrics, logger log.Logger) *Head {
	return &Head{
		tenant:    tenant,
		metrics:   metrics,
		logger:    logger,
		series:    newStripeSeries(),
		postings:  index.NewMemPostings(),
		closedMtx: sync.Mutex{},
		closed:    false,
	}
}

// MinTime returns the lowest time bound on visible data in the head.
func (h *Head) MinTime() int64 {
	return h.minTime.Load()
}

// MaxTime returns the highest timestamp seen in data of the head.
func (h *Head) MaxTime() int64 {
	return h.maxTime.Load()
}

func (h *Head) updateMinMaxTime(mint, maxt int64) {
	for {
		lt := h.MinTime()
		if mint >= lt {
			break
		}
		if h.minTime.CAS(lt, mint) {
			break
		}
	}
	for {
		ht := h.MaxTime()
		if maxt <= ht {
			break
		}
		if h.maxTime.CAS(ht, maxt) {
			break
		}
	}
}

// Note: chks must not be nil or zero-length
func (h *Head) Append(ls labels.Labels, chks index.ChunkMetas) {
	from, through := chks.Bounds()
	var id uint64
	created := h.series.Append(ls, chks, func() *memSeries {
		id = h.lastSeriesID.Inc()
		return newMemSeries(id, ls)
	})
	h.updateMinMaxTime(int64(from), int64(through))

	if !created {
		return
	}
	h.postings.Add(storage.SeriesRef(id), ls)
	h.numSeries.Inc()
}

// seriesHashmap is a simple hashmap for memSeries by their label set. It is built
// on top of a regular hashmap and holds a slice of series to resolve hash collisions.
// Its methods require the hash to be submitted with it to avoid re-computations throughout
// the code.
type seriesHashmap map[uint64][]*memSeries

func (m seriesHashmap) get(hash uint64, ls labels.Labels) *memSeries {
	for _, s := range m[hash] {
		if labels.Equal(s.ls, ls) {
			return s
		}
	}
	return nil
}

func (m seriesHashmap) set(hash uint64, s *memSeries) {
	l := m[hash]
	for i, prev := range l {
		if labels.Equal(prev.ls, s.ls) {
			l[i] = s
			return
		}
	}
	m[hash] = append(l, s)
}

type stripeSeries struct {
	shards int
	locks  []sync.RWMutex
	hashes []seriesHashmap
	// Sharded by ref. A series ref is the value of `size` when the series was being newly added.
	series []map[uint64]*memSeries
}

func newStripeSeries() *stripeSeries {
	s := &stripeSeries{
		shards: defaultStripeSize,
		locks:  make([]sync.RWMutex, defaultStripeSize),
		hashes: make([]seriesHashmap, defaultStripeSize),
		series: make([]map[uint64]*memSeries, defaultStripeSize),
	}
	for i := range s.hashes {
		s.hashes[i] = seriesHashmap{}
	}
	for i := range s.series {
		s.series[i] = map[uint64]*memSeries{}
	}
	return s
}

func (s *stripeSeries) getByID(id uint64) *memSeries {
	i := id & uint64(s.shards-1)

	s.locks[i].RLock()
	series := s.series[i][id]
	s.locks[i].RUnlock()

	return series
}

func (s *stripeSeries) getByHash(hash uint64, lset labels.Labels) *memSeries {
	i := hash & uint64(s.shards-1)

	s.locks[i].RLock()
	series := s.hashes[i].get(hash, lset)
	s.locks[i].RUnlock()

	return series
}

// Append adds chunks to the correct series and returns whether a new series was added
func (s *stripeSeries) Append(
	ls labels.Labels,
	chks index.ChunkMetas,
	createFn func() *memSeries,
) (created bool) {
	fp := ls.Hash()
	i := fp & uint64(s.shards-1)
	mtx := &s.locks[i]

	mtx.Lock()
	series := s.hashes[i].get(fp, ls)
	if series == nil {
		series = createFn()
		s.hashes[i].set(fp, series)
		created = true
	}
	mtx.Unlock()

	series.Lock()
	series.chks = append(series.chks, chks...)
	series.Unlock()

	return
}

type memSeries struct {
	sync.RWMutex
	ref  uint64 // The unique reference within a *Head
	ls   labels.Labels
	fp   model.Fingerprint
	chks index.ChunkMetas
}

func newMemSeries(ref uint64, ls labels.Labels) *memSeries {
	return &memSeries{
		ref: ref,
		ls:  ls,
		fp:  model.Fingerprint(ls.Hash()),
	}
}
