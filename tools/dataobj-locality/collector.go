package main

import (
	"context"
	"fmt"
	"math/bits"
	"sync"

	"github.com/apache/arrow-go/v18/arrow/scalar"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// labelValue identifies a label posting by its column name and value. The
// value is empty for the name-only rollup.
type labelValue struct {
	name  string
	value string
}

// keyAgg holds the global, cross-section aggregate for a single label key.
type keyAgg struct {
	// sectionSpread is the number of distinct physical postings sections the
	// key appears in ("clustering depth").
	sectionSpread int64
	// objectSpread is the number of distinct index objects the key appears in.
	objectSpread int64
	// seenObjects tracks which index object paths have been folded so that
	// objectSpread is counted correctly even when sections arrive out of order
	// (e.g. from concurrent goroutines).
	seenObjects map[string]struct{}
	// postingEntries is the number of KindLabel rows referencing the key
	// (= logs-section references).
	postingEntries int64
}

// sectionRef identifies a single logs section by the object that holds it and
// the section's index within that object.
type sectionRef struct {
	objectPath   string
	sectionIndex int64
}

// logsAgg holds the cross-object aggregate for a single sort-key value,
// measuring how the value's log records are spread across logs sections. The
// same logs section can be referenced by multiple postings sections (across
// overlapping index objects or compaction generations), so sections is a set
// keyed by [sectionRef] to dedupe those references.
type logsAgg struct {
	// sections maps each distinct logs section referencing the value to the
	// uncompressed size of that value's records within it.
	sections map[sectionRef]int64
	// objects is the set of distinct logs objects containing the value.
	objects map[string]struct{}
}

// collectorOptions controls which rollups the collector maintains.
type collectorOptions struct {
	byNameValue bool
	byName      bool
	// logsLocality enables the logs-section locality rollup keyed by the
	// values of the sortKey label column.
	logsLocality bool
	// sortKey is the label column whose values partition logs sections (the
	// primary dataobj sort dimension, e.g. service_name).
	sortKey string
	// logsSectionTargetBytes is the uncompressed logs-section target size used
	// to compute the ideal (perfectly clustered) section count.
	logsSectionTargetBytes int64
	// tenant is stamped onto every exported fact row to make shared output
	// files self-describing.
	tenant string
}

// collector accumulates label locality metrics across postings sections.
// foldSection may be called concurrently; all shared state is guarded by mu.
type collector struct {
	opts          collectorOptions
	mu            sync.Mutex
	totalSections int64
	// seenIndexObjects deduplicates index object paths across their sections so
	// each index object is classified and counted once. Guarded by mu.
	seenIndexObjects map[string]struct{}
	// compactedObjects and uncompactedObjects count distinct processed index
	// objects split by whether they were produced by the compactor. Guarded by
	// mu.
	compactedObjects   int64
	uncompactedObjects int64
	nameValue          map[labelValue]*keyAgg
	name               map[string]*keyAgg
	// logsBySortValue holds the logs-section locality aggregate per sort-key
	// value (e.g. service_name=foo).
	logsBySortValue map[string]*logsAgg
	// sink, when non-nil, receives one factRow per KindLabel posting for
	// raw-fact export. The sink is safe for concurrent use.
	sink factSink
}

// newCollector returns a collector configured for the requested rollups.
func newCollector(opts collectorOptions) *collector {
	c := &collector{
		opts:             opts,
		seenIndexObjects: make(map[string]struct{}),
	}
	if opts.byNameValue {
		c.nameValue = make(map[labelValue]*keyAgg)
	}
	if opts.byName {
		c.name = make(map[string]*keyAgg)
	}
	if opts.logsLocality {
		c.logsBySortValue = make(map[string]*logsAgg)
	}
	return c
}

// collect drives src, folding every tenant-owned postings section into the
// collector's global maps.
func (c *collector) collect(ctx context.Context, src sectionSource) error {
	return src.each(ctx, func(sec *dataobj.Section, objPath string, sectionIdx int64) error {
		return c.foldSection(ctx, sec, objPath, sectionIdx)
	})
}

// foldSection reads all KindLabel rows from a single postings section into
// section-local counts, then folds those counts into the global aggregated state.
func (c *collector) foldSection(ctx context.Context, sec *dataobj.Section, objPath string, sectionIdx int64) error {
	psec, err := postings.Open(ctx, sec)
	if err != nil {
		return fmt.Errorf("opening postings section %s: %w", objPath, err)
	}

	kindCol := findColumn(psec, postings.ColumnTypeKind)
	if kindCol == nil {
		// A section without a kind column has no label postings to attribute.
		return nil
	}

	pred := postings.EqualPredicate{Column: kindCol, Value: scalar.NewInt64Scalar(int64(postings.KindLabel))}
	rr := postings.NewRowReader(ctx, psec, []postings.Predicate{pred})
	defer rr.Close()

	var (
		nvLocal   map[labelValue]int64
		nameLocal map[string]int64
		// logsLocal maps a sort-key value to the logs sections it references in
		// this postings section, with the value's uncompressed size per section.
		logsLocal map[string]map[sectionRef]int64
		// facts accumulates one factRow per KindLabel row for raw export.
		facts []factRow
	)
	if c.opts.byNameValue {
		nvLocal = make(map[labelValue]int64)
	}
	if c.opts.byName {
		nameLocal = make(map[string]int64)
	}
	if c.opts.logsLocality {
		logsLocal = make(map[string]map[sectionRef]int64)
	}

	for rr.Next() {
		row := rr.At()
		if row.Kind != postings.KindLabel {
			continue
		}
		if c.opts.byNameValue {
			nvLocal[labelValue{name: row.ColumnName, value: row.LabelValue}]++
		}
		if c.opts.byName {
			nameLocal[row.ColumnName]++
		}
		if c.opts.logsLocality && row.ColumnName == c.opts.sortKey {
			refs := logsLocal[row.LabelValue]
			if refs == nil {
				refs = make(map[sectionRef]int64)
				logsLocal[row.LabelValue] = refs
			}
			ref := sectionRef{objectPath: row.ObjectPath, sectionIndex: row.SectionIndex}
			// Within a postings section a (section, value) pair is aggregated to
			// a single row, but guard against duplicates by keeping the largest.
			if sz, ok := refs[ref]; !ok || row.UncompressedSize > sz {
				refs[ref] = row.UncompressedSize
			}
		}

		if c.sink != nil {
			facts = append(facts, factRow{
				Tenant:           c.opts.tenant,
				IndexObject:      objPath,
				IndexSection:     sectionIdx,
				ColumnName:       row.ColumnName,
				LabelValue:       row.LabelValue,
				LogsObject:       row.ObjectPath,
				LogsSection:      row.SectionIndex,
				StreamRefs:       int64(popcount(row.StreamIDBitmap)),
				UncompressedSize: row.UncompressedSize,
				MinTimestamp:     row.MinTimestamp,
				MaxTimestamp:     row.MaxTimestamp,
			})
		}
	}
	if err := rr.Err(); err != nil {
		return fmt.Errorf("reading postings section %s: %w", objPath, err)
	}

	// Flush fact rows to the sink before taking the aggregation lock.
	// The sink self-synchronizes so this is safe to call concurrently.
	if c.sink != nil && len(facts) > 0 {
		if err := c.sink.write(facts); err != nil {
			return fmt.Errorf("writing facts for %s: %w", objPath, err)
		}
	}

	// I/O is done; merge section-local counts into the global maps under lock.
	c.mu.Lock()
	c.totalSections++
	// Classify and count the index object the first time any of its sections is
	// folded, so each object contributes to exactly one bucket.
	if _, seen := c.seenIndexObjects[objPath]; !seen {
		c.seenIndexObjects[objPath] = struct{}{}
		if isCompactedIndexPath(objPath) {
			c.compactedObjects++
		} else {
			c.uncompactedObjects++
		}
	}
	for k, entries := range nvLocal {
		foldInto(c.nameValue, k, objPath, entries)
	}
	for k, entries := range nameLocal {
		foldInto(c.name, k, objPath, entries)
	}
	for val, refs := range logsLocal {
		foldLogs(c.logsBySortValue, val, refs)
	}
	c.mu.Unlock()
	return nil
}

// foldInto folds one section's entry count for key into the global aggregate:
// sectionSpread grows by one per section and objectSpread grows only when the
// key is seen in a new object. Must be called with the collector's mu held.
func foldInto[K comparable](global map[K]*keyAgg, key K, objPath string, entries int64) {
	agg := global[key]
	if agg == nil {
		agg = &keyAgg{seenObjects: make(map[string]struct{})}
		global[key] = agg
	}
	agg.sectionSpread++
	agg.postingEntries += entries
	if _, seen := agg.seenObjects[objPath]; !seen {
		agg.seenObjects[objPath] = struct{}{}
		agg.objectSpread++
	}
}

// foldLogs merges one postings section's logs-section references for a sort-key
// value into the global aggregate, deduping logs sections by [sectionRef]. Must
// be called with the collector's mu held.
func foldLogs(global map[string]*logsAgg, value string, refs map[sectionRef]int64) {
	agg := global[value]
	if agg == nil {
		agg = &logsAgg{
			sections: make(map[sectionRef]int64),
			objects:  make(map[string]struct{}),
		}
		global[value] = agg
	}
	for ref, sz := range refs {
		// This works on the invariant that there should be a single
		// posting for a given (label value, logs section).
		if cur, ok := agg.sections[ref]; !ok || sz > cur {
			agg.sections[ref] = sz
		}
		agg.objects[ref.objectPath] = struct{}{}
	}
}

// popcount returns the number of set bits in b.
func popcount(b []byte) int {
	var n int
	for _, x := range b {
		n += bits.OnesCount8(x)
	}
	return n
}

// findColumn returns the section's column of type ct, or nil if absent.
func findColumn(sec *postings.Section, ct postings.ColumnType) *postings.Column {
	for _, c := range sec.Columns() {
		if c.Type == ct {
			return c
		}
	}
	return nil
}
