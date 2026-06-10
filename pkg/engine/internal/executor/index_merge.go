package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/util/loser"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// executeIndexMerge orchestrates the index merge: existence-check, classify sections,
// drive both merges, feed indexobj.Builder, and upload the result.
func (c *Context) executeIndexMerge(_ context.Context, node *physical.IndexMerge) Pipeline {
	return newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
		if err := c.doIndexMerge(ctx, node); err != nil {
			return errorPipeline(ctx, err)
		}
		return emptyPipeline()
	}, nil)
}

func (c *Context) doIndexMerge(ctx context.Context, node *physical.IndexMerge) error {
	// Check prerequisites
	if c.bucket == nil {
		return errors.New("no object store bucket configured")
	}

	// Check if output already exists (short-circuit on retry)
	exists, err := c.bucket.Exists(ctx, node.OutputIndexPath)
	if err != nil {
		return fmt.Errorf("checking output existence: %w", err)
	}
	if exists {
		level.Info(c.logger).Log("msg", "IndexMerge: output already exists, short-circuiting", "path", node.OutputIndexPath)
		return nil
	}

	// Classify sections from all runs
	postingsSections, statsSections, err := c.classifyRuns(ctx, node)
	if err != nil {
		return fmt.Errorf("classifying runs: %w", err)
	}

	// Create index object builder
	builder, err := indexobj.NewMergeBuilder(c.indexobjCfg, c.scratchStore)
	if err != nil {
		return fmt.Errorf("creating index builder: %w", err)
	}

	// Merge postings sections
	if err := c.mergePostingsIntoBuilder(ctx, node.Tenant, postingsSections, builder); err != nil {
		return fmt.Errorf("merging postings: %w", err)
	}

	// Merge stats sections
	if err := c.mergeStatsIntoBuilder(ctx, node.Tenant, statsSections, builder); err != nil {
		return fmt.Errorf("merging stats: %w", err)
	}

	// Snapshot the builder's in-memory accumulated size BEFORE Flush. This is the
	// uncompressed pre-encoding size used for the output_bytes_uncompressed
	// histogram observation below.
	uncompressedBytes := int64(builder.GetEstimatedSize())

	// Flush builder and upload result
	obj, closer, err := builder.Flush()
	if err != nil {
		if errors.Is(err, indexobj.ErrBuilderEmpty) {
			// Upload a zero-byte sentinel.
			return c.bucket.Upload(ctx, node.OutputIndexPath, io.NopCloser(bytes.NewReader([]byte{})))
		}
		return fmt.Errorf("flushing builder: %w", err)
	}
	defer closer.Close()

	// Stream object directly to upload
	reader, err := obj.Reader(ctx)
	if err != nil {
		return fmt.Errorf("getting object reader: %w", err)
	}
	defer reader.Close()

	if err := c.bucket.Upload(ctx, node.OutputIndexPath, reader); err != nil {
		return fmt.Errorf("uploading merged index: %w", err)
	}

	// Observe output sizes once the upload has succeeded. obj.Size() reflects
	// the encoded/compressed bytes that just got written.
	if c.indexMergeObserver != nil {
		c.indexMergeObserver.ObserveIndexMergeOutput(node.Tenant, obj.Size(), uncompressedBytes)
	}
	return nil
}

// classifyRuns scans all sections of each referenced source object and
// classifies them as postings or stats. Non-mergable section types (streams,
// pointers, indexPointers) are silently ignored. Validates that all stats
// sections share the same SortSchema.
func (c *Context) classifyRuns(ctx context.Context, node *physical.IndexMerge) (
	postingsSections []runSection,
	statsSections []runSection,
	err error,
) {
	// Deduplicate source objects by path. One object may be referenced by
	// multiple SectionRefs in the same or different runs; we open and scan it
	// exactly once.
	type objectEntry struct {
		obj    *dataobj.Object
		runIdx int // first run that referenced this object
	}
	objects := make(map[string]*objectEntry)

	for runIdx, runRef := range node.Runs {
		for _, sectionRef := range runRef.Sections {
			if _, seen := objects[sectionRef.ObjectPath]; seen {
				continue
			}
			obj, openErr := dataobj.FromBucket(ctx, c.bucket, sectionRef.ObjectPath, 0)
			if openErr != nil {
				return nil, nil, fmt.Errorf("opening object %q: %w", sectionRef.ObjectPath, openErr)
			}
			objects[sectionRef.ObjectPath] = &objectEntry{obj: obj, runIdx: runIdx}
		}
	}

	// For each unique source object, scan all of its sections and classify by
	// type. Unknown section types (streams, pointers, indexPointers) are
	// skipped silently — they're not applicable to an index merge.
	// Iterate in sorted order for deterministic section ordering.
	paths := make([]string, 0, len(objects))
	for path := range objects {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	// Make sure all stats section have the same sort schema by checking the sort
	// schema from the first row of each section. Catches misconfigured
	// cross-tenant merges or upstream schema-evolution bugs.
	var firstSortSchema string
	for _, path := range paths {
		entry := objects[path]
		for _, sec := range entry.obj.Sections() {
			switch {
			case postings.CheckSection(sec):
				postingsSections = append(postingsSections, runSection{
					section: sec,
					runIdx:  entry.runIdx,
				})
			case stats.CheckSection(sec):
				sortSchema, readErr := readStatsSortSchema(ctx, sec)
				if readErr != nil {
					return nil, nil, fmt.Errorf("reading stats section %d SortSchema: %w", len(statsSections), readErr)
				}
				if len(statsSections) == 0 {
					firstSortSchema = sortSchema
				} else if sortSchema != firstSortSchema {
					return nil, nil, fmt.Errorf(
						"stats sections have mismatched SortSchema: section 0 has %q, section %d has %q",
						firstSortSchema, len(statsSections), sortSchema,
					)
				}
				statsSections = append(statsSections, runSection{
					section: sec,
					runIdx:  entry.runIdx,
				})
				// default: skip silently (streams/pointers/indexPointers etc.)
			}
		}
	}

	return postingsSections, statsSections, nil
}

// readStatsSortSchema opens a stats section and returns the SortSchema from
// its first row. Returns an empty string if the section has no rows.
func readStatsSortSchema(ctx context.Context, sec *dataobj.Section) (string, error) {
	statsSec, err := stats.Open(ctx, sec)
	if err != nil {
		return "", fmt.Errorf("opening stats section: %w", err)
	}
	reader := stats.NewRowReader(ctx, statsSec)
	defer reader.Close()
	var row stats.Stat
	if reader.Next() {
		row = reader.At()
	}
	if err := reader.Err(); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return row.SortSchema, nil
}

type runSection struct {
	section *dataobj.Section
	runIdx  int
}

// mergePostingsIntoBuilder merges postings sections using a K-way merge heap and
// feeds results into the builder.
func (c *Context) mergePostingsIntoBuilder(ctx context.Context, tenant string, sections []runSection, builder *indexobj.MergeBuilder) error {
	if len(sections) == 0 {
		return nil
	}

	// Open all postings sections and create readers.
	readers := make([]sequence[postings.Row], 0, len(sections))

	for i, rs := range sections {
		sec, err := postings.Open(ctx, rs.section)
		if err != nil {
			return fmt.Errorf("opening postings section from run %d: %w", rs.runIdx, err)
		}

		readers = append(readers, indexedSeq[postings.Row]{
			CloseIterator: postings.NewRowReader(ctx, sec),
			idx:           i,
		})
	}

	// Drive a K-way merge over the readers via a loser tree.
	tree := loser.New(
		readers,
		heapVal[postings.Row]{isMax: true},
		heapAt[postings.Row],
		heapLess(comparePostingsRow),
		closeSeq[postings.Row])
	defer tree.Close()

	var last *postings.Row
	for tree.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}

		row := tree.Winner().At()

		// A collision on the full sort key (Kind, ObjectPath, SectionIndex,
		// ColumnName, LabelValue) means two source indexes reference the same
		// physical section/column/label — this shouldn't happen so log a warning
		// and emit a metric for tracking. The data is logically equivalent, so
		// keep the first row and drop the later duplicate.
		if last != nil && comparePostingsRow(*last, row) == 0 {
			if region := xcap.RegionFromContext(ctx); region != nil {
				region.Record(statIndexMergeDuplicatePostings.Observe(1))
			}
			level.Warn(c.logger).Log(
				"msg", "IndexMerge: postings full-key collision",
				"tenant", tenant,
				"kind", row.Kind,
				"object_path", row.ObjectPath,
				"section_index", row.SectionIndex,
				"column_name", row.ColumnName,
				"label_value", row.LabelValue,
			)
			continue
		}

		last = &row
		if err := c.writePostingsRow(builder, tenant, row); err != nil {
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

func (c *Context) writePostingsRow(builder *indexobj.MergeBuilder, tenant string, row postings.Row) error {
	switch row.Kind {
	case postings.KindLabel:
		return builder.AppendPostingsLabelEntry(tenant, row.LabelEntry())
	case postings.KindBloom:
		return builder.AppendPostingsBloomEntry(tenant, row.BloomEntry())
	default:
		return fmt.Errorf("unknown postings kind: %v", row.Kind)
	}
}

// mergeStatsIntoBuilder merges stats sections using a K-way merge heap and
// feeds results into the builder. Uses D3 aggregation with schema/label validation.
func (c *Context) mergeStatsIntoBuilder(ctx context.Context, tenant string, sections []runSection, builder *indexobj.MergeBuilder) error {
	if len(sections) == 0 {
		return nil
	}

	// Open all stats sections and create readers.
	readers := make([]sequence[stats.Stat], 0, len(sections))

	for i, rs := range sections {
		sec, err := stats.Open(ctx, rs.section)
		if err != nil {
			return fmt.Errorf("opening stats section from run %d: %w", rs.runIdx, err)
		}

		readers = append(readers, indexedSeq[stats.Stat]{
			CloseIterator: stats.NewRowReader(ctx, sec),
			idx:           i,
		})
	}

	// Drive a K-way merge over the readers via a loser tree.
	tree := loser.New(
		readers,
		heapVal[stats.Stat]{isMax: true},
		heapAt[stats.Stat],
		heapLess(compareStatsRow),
		closeSeq[stats.Stat])
	defer tree.Close()

	var last *stats.Stat
	for tree.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}

		row := tree.Winner().At()

		// The comparator includes (ObjectPath, SectionIndex) as final
		// tiebreakers, so an equal-key collision here means two source indexes
		// reference the same physical (ObjectPath, SectionIndex) — which
		// shouldn't happen. SortSchema and Labels are guaranteed to match on
		// such collisions (same source section), as are the aggregate counts;
		// keep the first row, drop the later duplicate, warn, and observe an
		// xcap statistic.
		if last != nil && compareStatsRow(*last, row) == 0 {
			if region := xcap.RegionFromContext(ctx); region != nil {
				region.Record(statIndexMergeDuplicateStats.Observe(1))
			}
			level.Warn(c.logger).Log(
				"msg", "IndexMerge: stats full-key collision",
				"tenant", tenant,
				"object_path", row.ObjectPath,
				"section_index", row.SectionIndex,
				"min_timestamp", row.MinTimestamp,
				"max_timestamp", row.MaxTimestamp,
			)
			continue
		}

		last = &row
		if err := builder.AppendStat(tenant, row); err != nil {
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

// indexedSeq attaches a stable index to an [iter.CloseIterator] so the
// loser tree can break ties deterministically. The index must be readable when
// the tree snapshots each sequence so it travels with
// the sequence rather than being derived at yield time.
//
// indexedSeq satisfies sequence[R] via the embedded iterator (promoting
// Next/At/Err/Close) plus Index.
type indexedSeq[R any] struct {
	iter.CloseIterator[R] // promotes Next/At/Err/Close
	idx                   int
}

// Index returns the iterators index in the merge. Used for stable tiebreak ordering.
func (s indexedSeq[R]) Index() int { return s.idx }

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
