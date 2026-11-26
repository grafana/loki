package tsdb

import (
	"context"
	"math"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// chunkTotals is the KB and line count of a set of chunk entries.
type chunkTotals struct {
	kb    uint64
	lines uint64
}

// add returns c with the entries chks added.
func (c chunkTotals) add(chks []tsdbindex.ChunkMeta) chunkTotals {
	for _, chk := range chks {
		c.kb += uint64(chk.KB)
		c.lines += uint64(chk.Entries)
	}
	return c
}

// plus returns c and o summed.
func (c chunkTotals) plus(o chunkTotals) chunkTotals {
	return chunkTotals{kb: c.kb + o.kb, lines: c.lines + o.lines}
}

// minus returns c less o, clamped at zero.
func (c chunkTotals) minus(o chunkTotals) chunkTotals {
	return chunkTotals{kb: c.kb - min(o.kb, c.kb), lines: c.lines - min(o.lines, c.lines)}
}

// mergeStageSizes holds one merge's chunk totals: listed (new ingester files, duplicates
// included), existing (the compacted index from earlier runs) and afterDedupe.
type mergeStageSizes struct {
	listed      chunkTotals
	existing    chunkTotals
	afterDedupe chunkTotals
}

// plus sums s and o, to total a run's tenants.
func (s mergeStageSizes) plus(o mergeStageSizes) mergeStageSizes {
	return mergeStageSizes{
		listed:      s.listed.plus(o.listed),
		existing:    s.existing.plus(o.existing),
		afterDedupe: s.afterDedupe.plus(o.afterDedupe),
	}
}

// distinct is what the merge added to the compacted index: afterDedupe - existing.
func (s mergeStageSizes) distinct() chunkTotals {
	return s.afterDedupe.minus(s.existing)
}

// dedupeTracker exports listed and distinct chunk bytes and lines. It remembers the ingester files it
// counted, so files re-uploaded after an ingester restart aren't counted twice. The memory is lost
// when the compactor restarts.
type dedupeTracker struct {
	listedBytes   prometheus.Counter
	distinctBytes prometheus.Counter
	listedLines   prometheus.Counter
	distinctLines prometheus.Counter

	// TEST ONLY: the counting proposed in review, without re-upload filtering, run side by side.
	unfiltered *unfilteredDedupeMetrics

	mtx       sync.Mutex
	maxTables int
	seen      map[string]map[string]struct{}
}

// newDedupeTracker registers the counters with reg, or leaves them unregistered if reg is nil.
func newDedupeTracker(reg prometheus.Registerer, maxTables int) *dedupeTracker {
	if maxTables < 1 {
		maxTables = 1
	}
	const sizeNote = " Sizes come from the index, rounded to whole KB per chunk, so chunks under 512 bytes count as 0."
	return &dedupeTracker{
		listedBytes: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Subsystem: "compactor",
			Name:      "tsdb_listed_chunk_uncompressed_bytes_total",
			Help:      "Total uncompressed bytes of chunk entries read from ingester index files by the compactor, before deduplication." + sizeNote,
		}),
		distinctBytes: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Subsystem: "compactor",
			Name:      "tsdb_distinct_chunk_uncompressed_bytes_total",
			Help:      "Total uncompressed bytes of chunk entries that were new in the compacted index after deduplication." + sizeNote,
		}),
		listedLines: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Subsystem: "compactor",
			Name:      "tsdb_listed_chunk_lines_total",
			Help:      "Total lines of chunk entries read from ingester index files by the compactor, before deduplication.",
		}),
		distinctLines: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Subsystem: "compactor",
			Name:      "tsdb_distinct_chunk_lines_total",
			Help:      "Total lines of chunk entries that were new in the compacted index after deduplication.",
		}),
		unfiltered: newUnfilteredDedupeMetrics(reg),
		maxTables:  maxTables,
		seen:       make(map[string]map[string]struct{}),
	}
}

// recordRun counts a successful run of table, taking files counted by earlier runs out of listed.
func (m *dedupeTracker) recordRun(ctx context.Context, logger log.Logger, table string, files []storage.IndexFile, indices []Index, sizes mergeStageSizes) {
	listed := sizes.listed
	var newNames []string
	for i, f := range files {
		if !m.isSeen(table, f.Name) {
			newNames = append(newNames, f.Name)
			continue
		}
		reuploaded, err := indexChunkTotals(ctx, indices[i])
		if err != nil {
			level.Warn(logger).Log("msg", "failed to read re-uploaded ingester index file, not counting this run", "table", table, "file", f.Name, "err", err)
			return
		}
		listed = listed.minus(reuploaded)
	}
	if len(newNames) == 0 {
		return
	}
	m.record(table, newNames, listed, sizes.distinct())
}

// isSeen reports whether fileName was already counted for table.
func (m *dedupeTracker) isSeen(table, fileName string) bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	_, ok := m.seen[table][fileName]
	return ok
}

// record adds to the counters and remembers fileNames, keeping the maxTables newest tables by date.
func (m *dedupeTracker) record(table string, fileNames []string, listed, distinct chunkTotals) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.listedBytes.Add(float64(listed.kb * 1024))
	m.distinctBytes.Add(float64(distinct.kb * 1024))
	m.listedLines.Add(float64(listed.lines))
	m.distinctLines.Add(float64(distinct.lines))

	files, ok := m.seen[table]
	if !ok {
		files = make(map[string]struct{}, len(fileNames))
		m.seen[table] = files
	}
	for _, name := range fileNames {
		files[name] = struct{}{}
	}

	for len(m.seen) > m.maxTables {
		oldest := ""
		for t := range m.seen {
			if oldest == "" || retention.ExtractIntervalFromTableName(t).Start < retention.ExtractIntervalFromTableName(oldest).Start {
				oldest = t
			}
		}
		delete(m.seen, oldest)
	}
}

// indexChunkTotals sums the chunks of a multi-tenant index across all tenants.
func indexChunkTotals(ctx context.Context, idx Index) (chunkTotals, error) {
	var t chunkTotals
	err := idx.(*TSDBFile).Index.(*TSDBIndex).ForSeries(ctx, "", nil, 0, math.MaxInt64, func(_ labels.Labels, _ model.Fingerprint, chks []tsdbindex.ChunkMeta) (stop bool) {
		t = t.add(chks)
		return false
	}, labels.MustNewMatcher(labels.MatchEqual, "", ""))
	return t, err
}

// totalChunks sums the chunks in the builder.
func (b *Builder) totalChunks() chunkTotals {
	var t chunkTotals
	for s := range b.allStreams() {
		t = t.add(s.chunks)
	}
	return t
}

// unfilteredDedupeMetrics is TEST ONLY. It counts listed and deduped chunk entries as they are read
// and dropped, with no re-upload filtering. A nil *unfilteredDedupeMetrics counts nothing.
type unfilteredDedupeMetrics struct {
	listedBytes  prometheus.Counter
	listedLines  prometheus.Counter
	dedupedBytes prometheus.Counter
	dedupedLines prometheus.Counter
}

func newUnfilteredDedupeMetrics(reg prometheus.Registerer) *unfilteredDedupeMetrics {
	c := func(name, help string) prometheus.Counter {
		return promauto.With(reg).NewCounter(prometheus.CounterOpts{Namespace: "loki", Subsystem: "compactor", Name: name, Help: "TEST ONLY. " + help})
	}
	return &unfilteredDedupeMetrics{
		listedBytes:  c("tsdb_unfiltered_listed_chunk_uncompressed_bytes_total", "Uncompressed bytes of chunk entries read from ingester index files, without re-upload filtering."),
		listedLines:  c("tsdb_unfiltered_listed_chunk_lines_total", "Lines of chunk entries read from ingester index files, without re-upload filtering."),
		dedupedBytes: c("tsdb_unfiltered_deduped_chunk_uncompressed_bytes_total", "Uncompressed bytes of duplicate chunk entries dropped, without re-upload filtering."),
		dedupedLines: c("tsdb_unfiltered_deduped_chunk_lines_total", "Lines of duplicate chunk entries dropped, without re-upload filtering."),
	}
}

func (m *unfilteredDedupeMetrics) listed(chks []tsdbindex.ChunkMeta) {
	if m == nil {
		return
	}
	t := chunkTotals{}.add(chks)
	m.listedBytes.Add(float64(t.kb * 1024))
	m.listedLines.Add(float64(t.lines))
}

func (m *unfilteredDedupeMetrics) deduped(kb, lines uint64) {
	if m == nil {
		return
	}
	m.dedupedBytes.Add(float64(kb * 1024))
	m.dedupedLines.Add(float64(lines))
}
