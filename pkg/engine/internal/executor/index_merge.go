package executor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/util/loser"
)

// indexPrefetchBytes is how many bytes the index merge and filter read from
// the start of a source index object when they open it.
//
// The decoder keeps these bytes and serves later reads inside them from
// memory. A window above the 16 KiB minimum of the decoder also holds more
// section metadata, which saves a request per section.
const indexPrefetchBytes = 64 * 1024

// executeIndexMerge orchestrates the index merge: classify sections, drive
// both merges, upload the result, and emit a result record that reports the
// index objects of the new index run.
func (c *Context) executeIndexMerge(_ context.Context, node *physical.IndexMerge) Pipeline {
	return newLazyPipeline(func(ctx context.Context) Pipeline {
		artifacts, err := c.doIndexMerge(ctx, node)
		if err != nil {
			return errorPipeline(ctx, err)
		}
		rec, err := v2.EncodeResultArtifacts(memory.DefaultAllocator, artifacts)
		if err != nil {
			return errorPipeline(ctx, err)
		}
		return NewBufferedPipeline(rec)
	})
}

func (c *Context) doIndexMerge(ctx context.Context, node *physical.IndexMerge) ([]v2.ResultArtifact, error) {
	start := time.Now()
	// Check prerequisites
	if c.bucket == nil {
		return nil, errors.New("no object store bucket configured")
	}

	objects, runs, layout, err := c.classifyRuns(ctx, node)
	if err != nil {
		return nil, fmt.Errorf("classifying runs: %w", err)
	}

	out, err := c.newIndexOutput(node.Tenant, layout)
	if err != nil {
		return nil, err
	}
	defer out.reset()

	meter := newInputMeter(c.indexMergeObserver.ObserveIndexMergeInputBytes)
	// The deferred flush reports the last rows of a merge that fails.
	defer meter.flush()
	if err := c.mergePostingsInto(ctx, runs, node.KeySpan, keepAll, meter, out); err != nil {
		return nil, fmt.Errorf("merging postings: %w", err)
	}
	statsObjects, err := statsSources(node, objects)
	if err != nil {
		return nil, err
	}
	if err := c.mergeStatsInto(ctx, statsObjects, keepAll, meter, out); err != nil {
		return nil, fmt.Errorf("merging stats: %w", err)
	}
	meter.flush()

	artifacts, sizes, err := out.finish(ctx)
	if err != nil {
		return nil, fmt.Errorf("index merge output: %w", err)
	}

	summary := IndexMergeObservedStats{
		InputBytes:              meter.total(),
		OutputBytesCompressed:   sizes.compressed,
		OutputBytesUncompressed: sizes.uncompressed,
	}
	duration := time.Since(start)
	var inputBytesPerSecond int64
	if duration > 0 {
		inputBytesPerSecond = int64(float64(summary.InputBytes) / duration.Seconds())
	}
	var inputSections int
	for _, run := range runs {
		inputSections += len(run)
	}
	level.Info(c.logger).Log(
		"msg", "IndexMerge: built index run",
		"tenant", node.Tenant,
		"source_objects", len(objects),
		"input_runs", len(node.Runs),
		"input_postings_sections", inputSections,
		"input_bytes", summary.InputBytes,
		"input_bytes_per_second", inputBytesPerSecond,
		"output_objects", len(artifacts),
		"output_bytes", summary.OutputBytesCompressed,
		"key_span_bounded", !node.KeySpan.IsAll(),
		"duration", duration,
	)
	c.indexMergeObserver.ObserveIndexMerge(node.Tenant, summary, duration)
	return artifacts, nil
}

// IndexMergeObservedStats is the summary of one successful IndexMerge task
// that the executor reports to IndexMergeObserver.
type IndexMergeObservedStats struct {
	// InputBytes is the size of the postings and stats rows that the task
	// read, as postingsRowBytes and statBytes count it.
	InputBytes int64

	// OutputBytesCompressed is the size of the uploaded index objects.
	OutputBytesCompressed int64

	// OutputBytesUncompressed is the estimated size of the index objects at
	// flush.
	OutputBytesUncompressed int64
}

// postingsRowBytes returns the input size of row: the length of its strings,
// stream ID bitmap, and bloom filter. It does not count the fixed-size
// fields.
func postingsRowBytes(row postings.Row) int64 {
	return int64(len(row.ObjectPath) + len(row.ColumnName) + len(row.LabelValue) + len(row.StreamIDBitmap) + len(row.BloomFilter))
}

// statBytes returns the input size of stat: the length of its object path,
// sort schema, and label names and values. It does not count the fixed-size
// fields.
func statBytes(stat stats.Stat) int64 {
	size := len(stat.ObjectPath) + len(stat.SortSchema)
	for name, value := range stat.Labels {
		size += len(name) + len(value)
	}
	return int64(size)
}

// statsSources returns the objects whose stats rows node merges. A merge over
// every key merges the stats rows of every object. A merge with a bounded key
// span merges only those of its stats sources. statsSources returns an error
// if a stats source is not a source of node.
func statsSources(node *physical.IndexMerge, objects []objectSections) ([]objectSections, error) {
	if node.KeySpan.IsAll() {
		return objects, nil
	}
	sources := make(map[string]struct{})
	for _, run := range node.Runs {
		for _, ref := range run.Sections {
			sources[ref.ObjectPath] = struct{}{}
		}
	}
	wanted := make(map[string]struct{}, len(node.StatsSources))
	for _, path := range node.StatsSources {
		if _, ok := sources[path]; !ok {
			return nil, fmt.Errorf("stats source %q is not a source of the merge", path)
		}
		wanted[path] = struct{}{}
	}
	var kept []objectSections
	for _, obj := range objects {
		if _, ok := wanted[obj.path]; ok {
			kept = append(kept, obj)
		}
	}
	return kept, nil
}

// classifyRuns opens each unique source object once and groups its mergable
// sections by type (postings, stats), preserving section order within each
// object. Non-mergable section types (streams, indexPointers) are silently
// ignored.
//
// A merge with a bounded key span takes only the postings sections that its
// runs reference, so a task of a split merge reads no section outside its
// span. It returns an error if a referenced section is not a postings section
// of the tenant. A merge over every key takes every postings section of each
// object.
//
// classifyRuns also returns the postings sections of each run of node, in
// run order, for a merge that reads each run as one sequence. It returns an
// error if an object appears in more than one run, or in two places of one
// run.
//
// classifyRuns also returns the stream layout of the source indexes. It
// returns an error if they record different layouts, or if only some record
// one.
func (c *Context) classifyRuns(ctx context.Context, node *physical.IndexMerge) ([]objectSections, [][]sourceSection, streams.Layout, error) {
	// Deduplicate source objects by path. One object may be referenced by
	// several SectionRefs of one run; we open and scan it exactly once.
	objects := make(map[string]*dataobj.Object)
	// referenced holds the referenced section indexes of each object. Only a
	// merge with a bounded key span uses it.
	referenced := make(map[string]map[int64]struct{})
	// runPaths holds the paths of the objects of each run, in run order.
	runPaths := make([][]string, len(node.Runs))
	// runOf maps the path of each object to the index of its run.
	runOf := make(map[string]int)

	for runIdx, runRef := range node.Runs {
		for _, sectionRef := range runRef.Sections {
			// A run reads its objects one after another in key order, so the
			// sections of one object must be next to each other in one run.
			// An object in two runs would merge its rows twice.
			path := sectionRef.ObjectPath
			if owner, seen := runOf[path]; seen {
				last := runPaths[owner][len(runPaths[owner])-1]
				if owner != runIdx || last != path {
					return nil, nil, streams.Layout{}, fmt.Errorf("object %q of run %d appears in more than one place of the runs of the merge, also in run %d", path, runIdx, owner)
				}
			} else {
				runOf[path] = runIdx
				runPaths[runIdx] = append(runPaths[runIdx], path)
			}

			if referenced[sectionRef.ObjectPath] == nil {
				referenced[sectionRef.ObjectPath] = make(map[int64]struct{})
			}
			referenced[sectionRef.ObjectPath][sectionRef.SectionIndex] = struct{}{}
			if _, seen := objects[sectionRef.ObjectPath]; seen {
				continue
			}
			obj, openErr := dataobj.FromBucket(ctx, c.bucket, sectionRef.ObjectPath, indexPrefetchBytes)
			if openErr != nil {
				return nil, nil, streams.Layout{}, fmt.Errorf("opening object %q: %w", sectionRef.ObjectPath, openErr)
			}
			objects[sectionRef.ObjectPath] = obj
		}
	}

	// Iterate objects in sorted path order for deterministic output.
	paths := make([]string, 0, len(objects))
	for path := range objects {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	// For each unique source object, scan its sections and classify by type,
	// preserving section order. Other section types (streams, indexPointers)
	// are skipped silently. openFilterSources also skips streams sections, but
	// it rejects every other type.
	grouped := make([]objectSections, 0, len(paths))
	var layout sourceLayout
	for _, path := range paths {
		obj := objects[path]
		group := objectSections{path: path}
		var found int
		for i, sec := range obj.Sections() {
			// Index objects are multi-tenant; only merge sections for the tenant
			// being compacted, or other tenants' rows leak into this output.
			if sec.Tenant != node.Tenant {
				continue
			}
			switch {
			case postings.CheckSection(sec):
				if !node.KeySpan.IsAll() {
					if _, ok := referenced[path][int64(i)]; !ok {
						continue
					}
					found++
				}
				group.postings = append(group.postings, sec)
			case stats.CheckSection(sec):
				group.stats = append(group.stats, sec)
				// default: skip silently (streams/indexPointers etc.)
			}
		}
		if !node.KeySpan.IsAll() && found != len(referenced[path]) {
			return nil, nil, streams.Layout{}, fmt.Errorf("object %q: %d of the %d referenced sections are not postings sections of tenant %q", path, len(referenced[path])-found, len(referenced[path]), node.Tenant)
		}
		if err := layout.add(ctx, path, obj, node.Tenant); err != nil {
			return nil, nil, streams.Layout{}, err
		}
		if len(group.postings) > 0 || len(group.stats) > 0 {
			grouped = append(grouped, group)
		}
	}

	merged, err := layout.shared()
	if err != nil {
		return nil, nil, streams.Layout{}, err
	}

	byPath := make(map[string]objectSections, len(grouped))
	for _, group := range grouped {
		byPath[group.path] = group
	}
	runs := make([][]sourceSection, len(runPaths))
	for i, paths := range runPaths {
		for _, path := range paths {
			runs[i] = append(runs[i], sectionsOf(path, byPath[path].postings)...)
		}
	}
	return grouped, runs, merged, nil
}

// sourceLayout collects the stream layouts that the source index objects of
// one task record for a tenant.
//
// The output index records one layout for all the log objects it indexes. So
// all sources must record the same layout, or none must record one. The
// planner groups index runs that way. A merge that mixes a source without a
// layout with sources that have one would claim that layout for log objects
// that were not written in it.
type sourceLayout struct {
	layout            streams.Layout
	pathWithLayout    string
	pathWithoutLayout string
}

// add reads the layout that obj, the source index object at path, records
// for tenant. It returns an error if the layout differs from the layout of an
// earlier source.
func (s *sourceLayout) add(ctx context.Context, path string, obj *dataobj.Object, tenant string) error {
	layout, ok, err := indexobj.TenantLayout(ctx, obj, tenant)
	if err != nil {
		return fmt.Errorf("reading stream layout of index %q: %w", path, err)
	}
	if !ok {
		s.pathWithoutLayout = path
		return nil
	}
	if s.pathWithLayout != "" && !layout.Equal(s.layout) {
		return fmt.Errorf("index %q has stream layout %v, but index %q has %v", path, layout, s.pathWithLayout, s.layout)
	}
	s.layout, s.pathWithLayout = layout, path
	return nil
}

// shared returns the layout of the sources. It is the zero layout if no source
// records one. It returns an error if only some sources record a layout.
func (s *sourceLayout) shared() (streams.Layout, error) {
	if s.pathWithLayout != "" && s.pathWithoutLayout != "" {
		return streams.Layout{}, fmt.Errorf("index %q records no stream layout, but index %q records %v", s.pathWithoutLayout, s.pathWithLayout, s.layout)
	}
	return s.layout, nil
}

// objectSections holds one source object's mergable sections, split by type and
// kept in section order. The section builders write every row in sorted order
// and then split it into sections by size. So the same-type sections of one
// object are slices of one sorted row stream, and a [sectionConcatSeq] can
// read them in section order.
type objectSections struct {
	path     string
	postings []*dataobj.Section
	stats    []*dataobj.Section
}

// keepAll keeps every row, whatever log object the row references.
func keepAll(string) bool { return true }

// mergePostingsInto merges the postings rows of runs whose posting key is in
// keySpan, with a K-way loser-tree merge. It writes a row to out only if keep
// returns true for the log object path of the row. It counts every row that
// it reads in meter, kept or not.
//
// Each run holds postings sections in posting key order, for example the
// sections of every index object of one index run. The merge reads each run
// as one sequence, so it holds one section reader per run, not one per index
// object. It returns an error if a run is not in order.
//
// The readers skip the pages that hold no key of keySpan, so a task of a
// merge split by key span reads little more than its own range.
func (c *Context) mergePostingsInto(ctx context.Context, runs [][]sourceSection, keySpan postings.KeySpan, keep func(logObjectPath string) bool, meter *inputMeter, out *indexOutput) error {
	readers := postingsSequences(ctx, runs, keySpan)
	if len(readers) == 0 {
		return nil
	}

	// Drive a K-way merge over the per-run sequences via a loser tree.
	tree := loser.New(
		readers,
		heapVal[postings.Row]{isMax: true},
		heapAt[postings.Row],
		heapLess(comparePostingsRow),
		closeSeq[postings.Row])
	defer tree.Close()

	var dedup postingsDedup
	for tree.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}

		row := tree.Winner().At()
		meter.add(postingsRowBytes(row))
		if !keep(row.ObjectPath) {
			continue
		}

		// Two source index objects should not reference the same logs section,
		// so log a warning for a repeated row. The repeated row describes the
		// same postings, so keep the first row and drop the later one.
		duplicate, err := dedup.add(row)
		if err != nil {
			return fmt.Errorf("tenant %q: %w", out.tenant, err)
		}
		if duplicate {
			level.Warn(c.logger).Log(
				"msg", "IndexMerge: postings full-key collision",
				"tenant", out.tenant,
				"kind", row.Kind,
				"object_path", row.ObjectPath,
				"section_index", row.SectionIndex,
				"column_name", row.ColumnName,
				"label_value", row.LabelValue,
			)
			continue
		}

		if err := out.appendPostingsRow(ctx, row); err != nil {
			return err
		}
	}

	for _, r := range readers {
		if err := r.Err(); err != nil {
			return err
		}
	}

	return nil
}

// postingsSequences returns one sequence over the postings rows of each run
// that has postings sections, for the K-way merge. The sequences return only
// the rows whose posting key is in keySpan, and fail on a row out of order.
func postingsSequences(ctx context.Context, runs [][]sourceSection, keySpan postings.KeySpan) []sequence[postings.Row] {
	open := func(ctx context.Context, sec *dataobj.Section) (iter.CloseIterator[postings.Row], error) {
		return openPostingsReader(ctx, sec, keySpan)
	}
	readers := make([]sequence[postings.Row], 0, len(runs))
	for _, run := range runs {
		if len(run) == 0 {
			continue
		}
		readers = append(readers, &sectionConcatSeq[postings.Row]{
			ctx:      ctx,
			sections: run,
			seqIdx:   len(readers),
			open:     open,
			compare:  comparePostingsRow,
		})
	}
	return readers
}

// mergeStatsInto merges the stats sections of all source index objects with a
// K-way loser-tree merge. It writes a row to out only if keep returns true for
// the log object path of the row. It counts every row that it reads in meter,
// kept or not.
func (c *Context) mergeStatsInto(ctx context.Context, objects []objectSections, keep func(logObjectPath string) bool, meter *inputMeter, out *indexOutput) error {
	readers := make([]sequence[stats.Stat], 0, len(objects))
	for _, obj := range objects {
		if len(obj.stats) == 0 {
			continue
		}
		readers = append(readers, &sectionConcatSeq[stats.Stat]{
			ctx:      ctx,
			sections: sectionsOf(obj.path, obj.stats),
			seqIdx:   len(readers),
			open:     openStatsReader,
		})
	}
	if len(readers) == 0 {
		return nil
	}

	// Drive a K-way merge over the per-object sequences via a loser tree.
	tree := loser.New(
		readers,
		heapVal[stats.Stat]{isMax: true},
		heapAt[stats.Stat],
		heapLess(stats.Compare),
		closeSeq[stats.Stat])
	defer tree.Close()

	var last *stats.Stat
	for tree.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}

		row := tree.Winner().At()
		meter.add(statBytes(row))
		if !keep(row.ObjectPath) {
			continue
		}

		// stats.Compare includes (ObjectPath, SectionIndex) as final
		// tiebreakers, so an equal-key collision here means two source index objects
		// reference the same physical (ObjectPath, SectionIndex) — which
		// shouldn't happen. SortSchema and Labels are guaranteed to match on
		// such collisions (same source section), as are the aggregate counts;
		// keep the first row, drop the later duplicate, and warn.
		if last != nil && stats.Compare(*last, row) == 0 {
			level.Warn(c.logger).Log(
				"msg", "IndexMerge: stats full-key collision",
				"tenant", out.tenant,
				"object_path", row.ObjectPath,
				"section_index", row.SectionIndex,
				"min_timestamp", row.MinTimestamp,
				"max_timestamp", row.MaxTimestamp,
			)
			continue
		}

		last = &row
		if err := out.appendStat(row); err != nil {
			return err
		}
	}

	for _, r := range readers {
		if err := r.Err(); err != nil {
			return err
		}
	}

	return nil
}

// sourceSection is one section of a source index object.
type sourceSection struct {
	objectPath string
	section    *dataobj.Section
}

// sectionsOf tags each of sections with path, the path of the source index
// object that holds them.
func sectionsOf(path string, sections []*dataobj.Section) []sourceSection {
	out := make([]sourceSection, len(sections))
	for i, section := range sections {
		out[i] = sourceSection{objectPath: path, section: section}
	}
	return out
}

// sectionConcatSeq lazily concatenates the row readers of same-type sections,
// read in order, and exposes them to the K-way merge as a single sequence[R].
// The sections can come from several index objects, for example every index
// object of one run. Only one section reader is open at a time, so K such
// sequences hold K section readers at once, whatever the number of sections
// and index objects. sectionConcatSeq satisfies sequence[R]
// (iter.CloseIterator[R] plus Index).
type sectionConcatSeq[R any] struct {
	ctx      context.Context
	sections []sourceSection
	open     func(context.Context, *dataobj.Section) (iter.CloseIterator[R], error)
	seqIdx   int

	// compare, if set, makes the sequence fail on a row that sorts below the
	// row before it. The output builders also reject rows out of order, but
	// only this check can name the source objects. It also sees the rows that
	// the merge does not keep.
	compare func(a, b R) int

	idx int                   // index of the section currently being read
	cur iter.CloseIterator[R] // reader for sections[idx]; nil until first Next / between sections
	row R                     // current row, valid after Next returns true
	err error

	hasRow     bool
	rowSection int // index of the section of row
}

// Index returns this sequence's stable index in the merge, for tiebreak ordering.
func (s *sectionConcatSeq[R]) Index() int { return s.seqIdx }

// At returns the current row. Valid only after Next returns true.
func (s *sectionConcatSeq[R]) At() R { return s.row }

// Err returns the first error encountered, if any.
func (s *sectionConcatSeq[R]) Err() error { return s.err }

// Next advances to the next row, transparently opening the next section once the
// current one is drained. It keeps at most one section reader open at a time.
// Returns false on exhaustion or on the first error.
func (s *sectionConcatSeq[R]) Next() bool {
	for {
		if s.err != nil {
			return false
		}
		if s.cur == nil {
			if s.idx >= len(s.sections) {
				return false
			}
			source := s.sections[s.idx]
			it, err := s.open(s.ctx, source.section)
			if err != nil {
				s.err = fmt.Errorf("opening section %d/%d of the sequence, of object %q: %w", s.idx, len(s.sections), source.objectPath, err)
				return false
			}
			s.cur = it
		}
		if s.cur.Next() {
			row := s.cur.At()
			if s.compare != nil && s.hasRow && s.compare(row, s.row) < 0 {
				s.err = fmt.Errorf("a row of section %d/%d of the sequence, of object %q, sorts below the row before it, of object %q", s.idx, len(s.sections), s.sections[s.idx].objectPath, s.sections[s.rowSection].objectPath)
				return false
			}
			s.row, s.hasRow, s.rowSection = row, true, s.idx
			return true
		}
		// Current section drained or errored: capture any error, close, advance.
		err := errors.Join(s.cur.Err(), s.cur.Close())
		s.cur = nil
		if err != nil {
			s.err = err
			return false
		}
		s.idx++
	}
}

// Close releases the currently open section reader, if any.
func (s *sectionConcatSeq[R]) Close() error {
	if s.cur != nil {
		err := s.cur.Close()
		s.cur = nil
		return err
	}
	return nil
}

// openPostingsReader opens a postings section and returns an iterator over its
// rows whose posting key is in keySpan.
func openPostingsReader(ctx context.Context, sec *dataobj.Section, keySpan postings.KeySpan) (iter.CloseIterator[postings.Row], error) {
	ps, err := postings.Open(ctx, sec)
	if err != nil {
		return nil, err
	}
	predicate, err := keySpan.Predicate(ps)
	if err != nil {
		return nil, fmt.Errorf("building key span predicate: %w", err)
	}
	var predicates []postings.Predicate
	if predicate != nil {
		predicates = []postings.Predicate{predicate}
	}
	reader := postings.NewReader(postings.ReaderOptions{Columns: ps.Columns(), Predicates: predicates})
	if err := reader.Open(ctx); err != nil {
		return nil, fmt.Errorf("opening postings reader: %w", err)
	}
	return postings.NewRowReader(ctx, reader), nil
}

// openStatsReader opens a stats section and returns a row iterator over it.
func openStatsReader(ctx context.Context, sec *dataobj.Section) (iter.CloseIterator[stats.Stat], error) {
	ss, err := stats.Open(ctx, sec)
	if err != nil {
		return nil, err
	}
	return stats.NewRowReader(ctx, ss), nil
}

// sequence[R] is one group cursor for the K-way merge. It is an
// [iter.CloseIterator] (Next/At/Err/Close) extended with Index for stable
// tiebreak ordering.
//
// Records must be emitted in sorted order.
type sequence[R any] interface {
	iter.CloseIterator[R]
	// Index returns the iterator's index in the merge. Used for stable tiebreak ordering.
	Index() int
}

// heapVal[R] is the loser-tree value type: a snapshot of an iterator's current
// record plus its index for stable tiebreaks.
//
// isMax marks the +∞ sentinel.
type heapVal[R any] struct {
	rec   R
	idx   int
	isMax bool
}

// heapAt snapshots a sequence's current record and index into a heapVal.
func heapAt[R any](s sequence[R]) heapVal[R] {
	return heapVal[R]{rec: s.At(), idx: s.Index()}
}

// closeSeq closes a sequence when the loser tree is done with it.
//
// It drops the close error. A drained sequence has no open reader, because
// Next closes each reader and keeps its close error in Err. A sequence that
// still has an open reader belongs to a merge that already failed.
func closeSeq[R any](s sequence[R]) {
	_ = s.Close()
}

// heapLess returns the loser-tree ordering for the provided comparator. The
// maxVal sentinel sorts after every real record. Order by cmp; break ties by
// group index for a stable merge.
func heapLess[R any](cmp func(a, b R) int) func(a, b heapVal[R]) bool {
	return func(a, b heapVal[R]) bool {
		if a.isMax {
			return false
		}
		if b.isMax {
			return true
		}
		if cmpResult := cmp(a.rec, b.rec); cmpResult != 0 {
			return cmpResult < 0
		}
		// Equal keys: lower group index wins.
		return a.idx < b.idx
	}
}
