// Copyright 2021 The Prometheus Authors
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
package tsdb

import (
	"sync"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
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
	// Do not specify without bit shifting. This allows us to
	// do shard index calcuations via bitwise & rather than modulos.
	defaultStripeSize = 64

	statusLabel          = "status"
	tsdbBuildSourceLabel = "source"

	statusFailure = "failure"
	statusSuccess = "success"
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
type Metrics struct {
	seriesNotFound       prometheus.Counter
	headRotations        *prometheus.CounterVec
	walTruncations       *prometheus.CounterVec
	tsdbBuilds           *prometheus.CounterVec
	tsdbBuildLastSuccess prometheus.Gauge
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		seriesNotFound: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: "loki_tsdb",
			Name:      "head_series_not_found_total",
			Help:      "Total number of requests for series that were not found",
		}),
		headRotations: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_tsdb",
			Name:      "head_rotation_attempts_total",
			Help:      "Total number of tsdb head rotations partitioned by status",
		}, []string{statusLabel}),
		walTruncations: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_tsdb",
			Name:      "wal_truncation_attempts_total",
			Help:      "Total number of WAL truncations partitioned by status",
		}, []string{statusLabel}),
		tsdbBuilds: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_tsdb",
			Name:      "build_index_attempts_total",
			Help:      "Total number of tsdb index builds partitioned by status",
		}, []string{statusLabel, tsdbBuildSourceLabel}),
		tsdbBuildLastSuccess: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_tsdb",
			Name:      "build_index_last_successful_timestamp_seconds",
			Help:      "Unix timestamp of the last successful tsdb index build",
		}),
	}
}

type Head struct {
	tenant           string
	numSeries        atomic.Uint64
	minTime, maxTime atomic.Int64 // Current min and max of the samples included in the head.

	// auto incrementing counter to uniquely identify series. This is also used
	// in the MemPostings, but is eventually discarded when we create a real TSDB index.
	lastSeriesID atomic.Uint64

	metrics *Metrics
	logger  log.Logger

	series *stripeSeries

	postings *index.MemPostings // Postings lists for terms.
}

func NewHead(tenant string, metrics *Metrics, logger log.Logger) *Head {
	return &Head{
		tenant:   tenant,
		metrics:  metrics,
		logger:   logger,
		series:   newStripeSeries(),
		postings: index.NewMemPostings(),
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

// Will CAS until successfully updates bounds or the condition is no longer valid
func updateMintMaxt(mint, maxt int64, mintSrc, maxtSrc *atomic.Int64) {
	for {
		lt := mintSrc.Load()
		if mint >= lt && lt != 0 {
			break
		}
		if mintSrc.CompareAndSwap(lt, mint) {
			break
		}
	}
	for {
		ht := maxtSrc.Load()
		if maxt <= ht {
			break
		}
		if maxtSrc.CompareAndSwap(ht, maxt) {
			break
		}
	}
}

// Note: chks must not be nil or zero-length
func (h *Head) Append(ls labels.Labels, fprint uint64, chks index.ChunkMetas) (created bool, refID uint64) {
	from, through := chks.Bounds()
	var id uint64
	created, refID = h.series.Append(ls, chks, func() *memSeries {
		id = h.lastSeriesID.Inc()
		return newMemSeries(id, ls, fprint)
	})
	updateMintMaxt(int64(from), int64(through), &h.minTime, &h.maxTime)

	if !created {
		return
	}
	h.postings.Add(storage.SeriesRef(id), ls)
	h.numSeries.Inc()
	return
}

// seriesHashmap is a simple hashmap for memSeries by their label set. It is built
// on top of a regular hashmap and holds a slice of series to resolve hash collisions.
// Its methods require the hash to be submitted with it to avoid re-computations throughout
// the code.
type seriesHashmap struct {
	sync.RWMutex
	m map[uint64][]*memSeries
}

func newSeriesHashmap() *seriesHashmap {
	return &seriesHashmap{
		m: map[uint64][]*memSeries{},
	}
}

func (m *seriesHashmap) get(hash uint64, ls labels.Labels) *memSeries {
	for _, s := range m.m[hash] {
		if labels.Equal(s.ls, ls) {
			return s
		}
	}
	return nil
}

func (m *seriesHashmap) set(hash uint64, s *memSeries) {
	l := m.m[hash]
	for i, prev := range l {
		if labels.Equal(prev.ls, s.ls) {
			l[i] = s
			return
		}
	}
	m.m[hash] = append(l, s)
}

type memSeriesMap struct {
	sync.RWMutex
	m map[uint64]*memSeries
}

func newMemseriesMap() *memSeriesMap {
	return &memSeriesMap{
		m: map[uint64]*memSeries{},
	}
}

type stripeSeries struct {
	shards int
	// Sharded by fingerprint.
	hashes []*seriesHashmap
	// Sharded by ref. A series ref is the value of `size` when the series was being newly added.
	series []*memSeriesMap
}

func newStripeSeries() *stripeSeries {
	s := &stripeSeries{
		shards: defaultStripeSize,
		hashes: make([]*seriesHashmap, defaultStripeSize),
		series: make([]*memSeriesMap, defaultStripeSize),
	}
	for i := range s.hashes {
		s.hashes[i] = newSeriesHashmap()
	}
	for i := range s.series {
		s.series[i] = newMemseriesMap()
	}
	return s
}

func (s *stripeSeries) getByID(id uint64) *memSeries {
	x := s.series[id&uint64(s.shards-1)]
	x.RLock()
	defer x.RUnlock()
	return x.m[id]
}

// Append adds chunks to the correct series and returns whether a new series was added
func (s *stripeSeries) Append(
	ls labels.Labels,
	chks index.ChunkMetas,
	createFn func() *memSeries,
) (created bool, refID uint64) {
	fp := ls.Hash()
	hashes := s.hashes[fp&uint64(s.shards-1)]
	hashes.Lock()
	series := hashes.get(fp, ls)
	if series == nil {
		series = createFn()
		hashes.set(fp, series)

		// the series locks are determined by the ref, not fingerprint
		seriesMap := s.series[series.ref&uint64(s.shards-1)]
		seriesMap.Lock()
		seriesMap.m[series.ref] = series
		seriesMap.Unlock()

		created = true
	}
	hashes.Unlock()

	series.Lock()
	series.chks = append(series.chks, chks...)
	refID = series.ref
	series.Unlock()

	return
}

type memSeries struct {
	sync.RWMutex
	ref  uint64 // The unique reference within a *Head
	ls   labels.Labels
	fp   uint64
	chks index.ChunkMetas
}

func newMemSeries(ref uint64, ls labels.Labels, fp uint64) *memSeries {
	return &memSeries{
		ref: ref,
		ls:  ls,
		fp:  fp,
	}
}
