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

	// Check if output already exists (short-circuit on retry).
	//
	// We probe with a GetObject rather than Exists/HeadObject: under IRSA roles
	// that grant s3:GetObject/s3:PutObject but not s3:ListBucket, a HeadObject on
	// a missing key returns 403 AccessDenied instead of 404 NoSuchKey, which
	// objstore surfaces as a hard error. GetObject returns a genuine NoSuchKey on
	// a missing key without requiring ListBucket. Treat access-denied here as
	// "not present" too, so a restrictive read policy never blocks the merge.
	exists, err := c.outputExists(ctx, node.OutputIndexPath)
	if err != nil {
		return fmt.Errorf("checking output existence: %w", err)
	}
	if exists {
		level.Info(c.logger).Log("msg", "IndexMerge: output already exists, short-circuiting", "path", node.OutputIndexPath)
		return nil
	}

	// Classify sections from all runs, grouped by source object.
	objects, err := c.classifyRuns(ctx, node)
	if err != nil {
		return fmt.Errorf("classifying runs: %w", err)
	}

	// Create index object builder
	builder, err := indexobj.NewMergeBuilder(c.indexobjCfg, c.scratchStore)
	if err != nil {
		return fmt.Errorf("creating index builder: %w", err)
	}

	// Merge postings sections
	if err := c.mergePostingsIntoBuilder(ctx, node.Tenant, objects, builder); err != nil {
		return fmt.Errorf("merging postings: %w", err)
	}

	// Merge stats sections
	if err := c.mergeStatsIntoBuilder(ctx, node.Tenant, objects, builder); err != nil {
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

// outputExists reports whether the IndexMerge output object is already present.
// It probes with GetObject so a restrictive read policy (s3:GetObject without
// s3:ListBucket) still yields a correct answer: a missing key returns
// not-found, and an access-denied response is treated as not-present rather
// than a fatal error.
func (c *Context) outputExists(ctx context.Context, path string) (bool, error) {
	rc, err := c.bucket.Get(ctx, path)
	if err != nil {
		if c.bucket.IsObjNotFoundErr(err) || c.bucket.IsAccessDeniedErr(err) {
			return false, nil
		}
		return false, err
	}
	_ = rc.Close()
	return true, nil
}

// classifyRuns opens each unique source object once and groups its mergable
// sections by type (postings, stats), preserving section order within each
// object. Non-mergable section types (streams, pointers, indexPointers) are
// silently ignored. It validates that all stats sections share the same SortSchema
func (c *Context) classifyRuns(ctx context.Context, node *physical.IndexMerge) ([]objectSections, error) {
	// Deduplicate source objects by path. One object may be referenced by
	// multiple SectionRefs in the same or different runs; we open and scan it
	// exactly once.
	objects := make(map[string]*dataobj.Object)

	for _, runRef := range node.Runs {
		for _, sectionRef := range runRef.Sections {
			if _, seen := objects[sectionRef.ObjectPath]; seen {
				continue
			}
			obj, openErr := dataobj.FromBucket(ctx, c.bucket, sectionRef.ObjectPath, 0)
			if openErr != nil {
				return nil, fmt.Errorf("opening object %q: %w", sectionRef.ObjectPath, openErr)
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
	// preserving section order. Unknown section types (streams, pointers,
	// indexPointers) are skipped silently. Make sure all stats sections share
	// the same sort schema (checked from the first row of each). statsSeen
	// counts stats sections across all objects, for stable error messages.
	grouped := make([]objectSections, 0, len(paths))
	var firstSortSchema string
	statsSeen := 0
	for _, path := range paths {
		obj := objects[path]
		group := objectSections{path: path}
		for _, sec := range obj.Sections() {
			// Index objects are multi-tenant; only merge sections for the tenant
			// being compacted, or other tenants' rows leak into this output.
			if sec.Tenant != node.Tenant {
				continue
			}
			switch {
			case postings.CheckSection(sec):
				group.postings = append(group.postings, sec)
			case stats.CheckSection(sec):
				sortSchema, readErr := readStatsSortSchema(ctx, sec)
				if readErr != nil {
					return nil, fmt.Errorf("reading stats section %d SortSchema: %w", statsSeen, readErr)
				}
				if statsSeen == 0 {
					firstSortSchema = sortSchema
				} else if sortSchema != firstSortSchema {
					return nil, fmt.Errorf(
						"stats sections have mismatched SortSchema: section 0 has %q, section %d has %q",
						firstSortSchema, statsSeen, sortSchema,
					)
				}
				statsSeen++
				group.stats = append(group.stats, sec)
				// default: skip silently (streams/pointers/indexPointers etc.)
			}
		}
		if len(group.postings) > 0 || len(group.stats) > 0 {
			grouped = append(grouped, group)
		}
	}

	return grouped, nil
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

// objectSections holds one source object's mergable sections, split by type and
// kept in section order. A single object's same-type sections are contiguous,
// non-overlapping slices of that object's sorted row stream (the section
// encoders sort every row, then split into sections at size boundaries), so
// they can be concatenated in section order — see [sectionConcatSeq].
type objectSections struct {
	path     string
	postings []*dataobj.Section
	stats    []*dataobj.Section
}

// mergePostingsIntoBuilder merges the postings sections of all source objects
// with a K-way loser-tree merge and feeds the result into the builder.
func (c *Context) mergePostingsIntoBuilder(ctx context.Context, tenant string, objects []objectSections, builder *indexobj.MergeBuilder) error {
	readers := make([]sequence[postings.Row], 0, len(objects))
	for _, obj := range objects {
		if len(obj.postings) == 0 {
			continue
		}
		readers = append(readers, &sectionConcatSeq[postings.Row]{
			ctx:        ctx,
			objectPath: obj.path,
			sections:   obj.postings,
			seqIdx:     len(readers),
			open:       openPostingsReader,
		})
	}
	if len(readers) == 0 {
		return nil
	}

	// Drive a K-way merge over the per-object sequences via a loser tree.
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

		// A collision on the identity key (Kind, ObjectPath, SectionIndex,
		// ColumnName, LabelValue) means two source indexes reference the same
		// physical section/column/label — this shouldn't happen so log a warning
		// and emit a metric for tracking. The data is logically equivalent, so
		// keep the first row and drop the later duplicate.
		if last != nil && samePostingsKey(*last, row) {
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

// mergeStatsIntoBuilder merges the stats sections of all source objects with a
// K-way loser-tree merge and feeds the result into the builder. Uses D3
// aggregation with schema/label validation.
func (c *Context) mergeStatsIntoBuilder(ctx context.Context, tenant string, objects []objectSections, builder *indexobj.MergeBuilder) error {
	readers := make([]sequence[stats.Stat], 0, len(objects))
	for _, obj := range objects {
		if len(obj.stats) == 0 {
			continue
		}
		readers = append(readers, &sectionConcatSeq[stats.Stat]{
			ctx:        ctx,
			objectPath: obj.path,
			sections:   obj.stats,
			seqIdx:     len(readers),
			open:       openStatsReader,
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

		// stats.Compare includes (ObjectPath, SectionIndex) as final
		// tiebreakers, so an equal-key collision here means two source indexes
		// reference the same physical (ObjectPath, SectionIndex) — which
		// shouldn't happen. SortSchema and Labels are guaranteed to match on
		// such collisions (same source section), as are the aggregate counts;
		// keep the first row, drop the later duplicate, warn, and observe an
		// xcap statistic.
		if last != nil && stats.Compare(*last, row) == 0 {
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

// sectionConcatSeq lazily concatenates the row readers of one source object's
// same-type sections, read in section order, exposing them to the K-way merge
// as a single sequence[R]. Only one section reader is open at a time, so K such
// sequences hold K Arrow batches concurrently instead of one batch per section
// across the whole task — the core of the index-merge memory reduction.
// sectionConcatSeq satisfies sequence[R] (iter.CloseIterator[R] plus Index).
type sectionConcatSeq[R any] struct {
	ctx        context.Context
	objectPath string // source object path, for error context
	sections   []*dataobj.Section
	open       func(context.Context, *dataobj.Section) (iter.CloseIterator[R], error)
	seqIdx     int

	idx int                   // index of the section currently being read
	cur iter.CloseIterator[R] // reader for sections[idx-1]; nil until first Next / between sections
	row R                     // current row, valid after Next returns true
	err error
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
			it, err := s.open(s.ctx, s.sections[s.idx])
			if err != nil {
				s.err = fmt.Errorf("opening section %d/%d of object %q: %w", s.idx, len(s.sections), s.objectPath, err)
				return false
			}
			s.cur = it
		}
		if s.cur.Next() {
			s.row = s.cur.At()
			return true
		}
		// Current section drained or errored: capture any error, close, advance.
		err := s.cur.Err()
		_ = s.cur.Close()
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

// openPostingsReader opens a postings section and returns a row iterator over it.
func openPostingsReader(ctx context.Context, sec *dataobj.Section) (iter.CloseIterator[postings.Row], error) {
	ps, err := postings.Open(ctx, sec)
	if err != nil {
		return nil, err
	}
	return postings.NewRowReader(ctx, ps, nil), nil
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
