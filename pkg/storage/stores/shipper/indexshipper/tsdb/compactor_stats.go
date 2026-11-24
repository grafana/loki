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

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// chunkTotals is the size of a set of chunk entries: their KB and their lines.
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

// minus returns c less o, stopping at zero in each field.
func (c chunkTotals) minus(o chunkTotals) chunkTotals {
	return chunkTotals{kb: c.kb - min(o.kb, c.kb), lines: c.lines - min(o.lines, c.lines)}
}

// mergeStageSizes holds the chunk totals in a builder after each stage of setupBuilder.
// These are raw sizes, not the dedupe metric: listed and distinct derive the metric from them.
//
// The stages, in order:
//   - afterIngesterFiles: the entries from the new ingester index files were added, from every replica.
//   - afterCompactedIndex: the existing compacted index of the table was added. The compactor
//     re-merges a day's index on every run, so this holds what earlier runs merged.
//   - afterDedupe: FinalizeChunks dropped identical entries.
//
// Each size is a running total of the builder, so a later stage includes the earlier ones.
type mergeStageSizes struct {
	afterIngesterFiles  chunkTotals
	afterCompactedIndex chunkTotals
	afterDedupe         chunkTotals
}

// plus returns s and o summed stage by stage, to total the tenants of a run.
func (s mergeStageSizes) plus(o mergeStageSizes) mergeStageSizes {
	return mergeStageSizes{
		afterIngesterFiles:  s.afterIngesterFiles.plus(o.afterIngesterFiles),
		afterCompactedIndex: s.afterCompactedIndex.plus(o.afterCompactedIndex),
		afterDedupe:         s.afterDedupe.plus(o.afterDedupe),
	}
}

// listed is the size of the entries read from the ingester files, before deduplication.
func (s mergeStageSizes) listed() chunkTotals {
	return s.afterIngesterFiles
}

// distinct is the size that the merge added to the compacted index after deduplication:
// what is left after dedupe, minus what the existing compacted index already held.
func (s mergeStageSizes) distinct() chunkTotals {
	existing := s.afterCompactedIndex.minus(s.afterIngesterFiles)
	return s.afterDedupe.minus(existing)
}

// dedupeTracker exports the counters that measure how much the compactor's deduplication
// removes, in chunk KB and lines, and remembers which ingester index files it has already counted.
//
// An ingester that restarts uploads its local index files again under the same name.
// The tracker remembers the names of ingester files already counted for each table, so
// their entries are not counted as listed twice. This memory is lost when the compactor
// restarts, so a restart can count the same files again.
type dedupeTracker struct {
	listedKB      prometheus.Counter
	distinctKB    prometheus.Counter
	listedLines   prometheus.Counter
	distinctLines prometheus.Counter

	mtx       sync.Mutex
	maxTables int
	// seq is incremented on every record call and used to order tables by recency.
	seq      uint64
	lastUsed map[string]uint64
	seen     map[string]map[string]struct{}
}

// newDedupeTracker registers the dedupe counters with reg and returns a tracker
// that remembers ingester file names for at most maxTables tables.
// A nil reg creates the counters unregistered: promauto skips registration when its
// Registerer is nil, so the counters still count but are not exported.
func newDedupeTracker(reg prometheus.Registerer, maxTables int) *dedupeTracker {
	if maxTables < 1 {
		maxTables = 1
	}
	return &dedupeTracker{
		listedKB: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Subsystem: "compactor",
			Name:      "tsdb_listed_chunk_kb_total",
			Help:      "Total size in KB of chunk entries read from ingester index files by the compactor, before deduplication.",
		}),
		distinctKB: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Subsystem: "compactor",
			Name:      "tsdb_distinct_chunk_kb_total",
			Help:      "Total size in KB of chunk entries that were new in the compacted index after deduplication.",
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
		maxTables: maxTables,
		lastUsed:  make(map[string]uint64),
		seen:      make(map[string]map[string]struct{}),
	}
}

// recordRun counts one successful compaction run of table. sizes is the sum over the run's
// tenants. Ingester files already counted by an earlier run were merged again, but their size is
// taken out of listed. A run with no new ingester files changes nothing. If a re-uploaded file
// can't be read, the run is logged and not counted; compaction is not affected.
func (m *dedupeTracker) recordRun(ctx context.Context, logger log.Logger, table string, files []storage.IndexFile, indices []Index, sizes mergeStageSizes) {
	listed := sizes.listed()
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

// isSeen reports whether the ingester index file fileName was already counted for table.
func (m *dedupeTracker) isSeen(table, fileName string) bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	_, ok := m.seen[table][fileName]
	return ok
}

// record adds listed and distinct to the counters and marks fileNames as counted for table.
// When more than maxTables tables are tracked, the least recently recorded table is forgotten.
func (m *dedupeTracker) record(table string, fileNames []string, listed, distinct chunkTotals) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.listedKB.Add(float64(listed.kb))
	m.distinctKB.Add(float64(distinct.kb))
	m.listedLines.Add(float64(listed.lines))
	m.distinctLines.Add(float64(distinct.lines))

	m.seq++
	m.lastUsed[table] = m.seq

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
		var oldestSeq uint64
		for t := range m.seen {
			if oldest == "" || m.lastUsed[t] < oldestSeq {
				oldest, oldestSeq = t, m.lastUsed[t]
			}
		}
		delete(m.seen, oldest)
		delete(m.lastUsed, oldest)
	}
}

// indexChunkTotals returns the size of every chunk in the multi-tenant index idx, across all tenants.
// recordRun uses it to take re-uploaded ingester files out of listed.
func indexChunkTotals(ctx context.Context, idx Index) (chunkTotals, error) {
	var t chunkTotals
	err := idx.(*TSDBFile).Index.(*TSDBIndex).ForSeries(ctx, "", nil, 0, math.MaxInt64, func(_ labels.Labels, _ model.Fingerprint, chks []tsdbindex.ChunkMeta) (stop bool) {
		t = t.add(chks)
		return false
	}, labels.MustNewMatcher(labels.MatchEqual, "", ""))
	return t, err
}

// totalChunks returns the size of all chunks in the builder.
func (b *Builder) totalChunks() chunkTotals {
	var t chunkTotals
	for s := range b.allStreams() {
		t = t.add(s.chunks)
	}
	return t
}
