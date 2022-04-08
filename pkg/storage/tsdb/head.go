package tsdb

import (
	"sync"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/storage/tsdb/index"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"go.uber.org/atomic"
)

const (
	// Note, this is significantly less than the stripe values used by Prometheus' stripeSeries.
	// This is for two reasons.
	// 1) Heads are per-tenant in Loki
	// 2) Loki tends to have a few orders of magnitude less series per node than
	// Prometheus|Cortex|Mimir.
	defaultSeriesMapShards = 64
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

type HeadMetrics struct{}

func NewHeadMetrics(r prometheus.Registerer) *HeadMetrics {
	return &HeadMetrics{}
}

type Head struct {
	tenant           string
	numSeries        atomic.Uint64
	minTime, maxTime atomic.Int64 // Current min and max of the samples included in the head.

	metrics *HeadMetrics
	logger  log.Logger

	series *seriesMap

	postings *index.MemPostings // Postings lists for terms.

	closedMtx sync.Mutex
	closed    bool
}

func NewHead(tenant string, metrics *HeadMetrics, logger log.Logger) *Head {
	return &Head{
		tenant:    tenant,
		metrics:   metrics,
		logger:    logger,
		series:    newSeriesMap(),
		postings:  index.NewMemPostings(),
		closedMtx: sync.Mutex{},
		closed:    false,
	}
}

func (h *Head) updateMinMaxTime(mint, maxt int64) {
	for {
		lt := h.minTime.Load()
		if mint >= lt {
			break
		}
		if h.minTime.CAS(lt, mint) {
			break
		}
	}
	for {
		ht := h.maxTime.Load()
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
	created := h.series.Append(ls, chks)
	h.updateMinMaxTime(int64(from), int64(through))

	if !created {
		return
	}
	h.numSeries.Add(1)
}

type seriesMap struct {
	shards int
	locks  []sync.RWMutex
	series []map[uint64]memSeriesList
}

func newSeriesMap() *seriesMap {
	s := &seriesMap{
		shards: defaultSeriesMapShards,
		locks:  make([]sync.RWMutex, defaultSeriesMapShards),
		series: make([]map[uint64]memSeriesList, defaultSeriesMapShards),
	}
	for i := range s.series {
		s.series[i] = map[uint64]memSeriesList{}
	}
	return s
}

// Append adds chunks to the correct series and returns whether a new series was added
func (s *seriesMap) Append(ls labels.Labels, chks index.ChunkMetas) (created bool) {
	fp := ls.Hash()
	i := fp & uint64(s.shards)
	mtx := &s.locks[i]

	mtx.Lock()
	xs, ok := s.series[i][fp]
	if !ok {
		xs = memSeriesList{}
	}

	series, ok := xs.Find(ls)
	if !ok {
		series = newMemSeries(ls, model.Fingerprint(fp))
		s.series[i][fp] = append(xs, series)
		created = true
	}
	// Safe to unlock the seriesMap shard.
	// We'll be using the series' mutex from now on.
	mtx.Unlock()
	series.Lock()
	series.chks = append(series.chks, chks...)
	series.Unlock()
	return
}

type memSeriesList []*memSeries

func (ms memSeriesList) Find(ls labels.Labels) (*memSeries, bool) {
	for _, x := range ms {
		if labels.Equal(x.ls, ls) {
			return x, true
		}
	}
	return nil, false
}

type memSeries struct {
	sync.RWMutex
	ls   labels.Labels
	fp   model.Fingerprint
	chks index.ChunkMetas
}

func newMemSeries(ls labels.Labels, fp model.Fingerprint) *memSeries {
	return &memSeries{
		ls: ls,
		fp: fp,
	}
}
