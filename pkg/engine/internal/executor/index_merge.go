package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// executeIndexMerge orchestrates the index merge: existence-check, classify sections,
// drive both heaps, feed indexobj.Builder, and upload the result.
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

	// Apply TTL timeout if specified
	if node.TaskTTL > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, node.TaskTTL)
		defer cancel()
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

	// Flush builder and upload result
	obj, closer, err := builder.Flush()
	if err != nil {
		if errors.Is(err, indexobj.ErrBuilderEmpty) {
			// Upload a zero-byte sentinel
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
	return nil
}

// classifyRuns scans all sections of each referenced source object and classifies
// them as postings or stats. Deduplicates objects by path; skips unknown section
// types silently (streams, pointers, indexPointers are not the index merger's concern).
// Validates that all stats sections share the same SortSchema.
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
				statsSections = append(statsSections, runSection{
					section: sec,
					runIdx:  entry.runIdx,
				})
				// default: skip silently (streams/pointers/indexPointers etc.)
			}
		}
	}

	// Validate that all stats sections share the same SortSchema. Read the
	// first row of each to check. Catches misconfigured cross-tenant merges or
	// upstream schema-evolution bugs.
	if len(statsSections) > 0 {
		var firstSortSchema string
		for i, rs := range statsSections {
			sec, openErr := stats.Open(ctx, rs.section)
			if openErr != nil {
				return nil, nil, fmt.Errorf("opening stats section for validation: %w", openErr)
			}
			reader := newStatsPileReader(sec)
			row, readErr := reader.Next(ctx)
			reader.Close()
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return nil, nil, fmt.Errorf("reading stats section %d for validation: %w", i, readErr)
			}
			if i == 0 {
				firstSortSchema = row.SortSchema
			} else if row.SortSchema != firstSortSchema {
				return nil, nil, fmt.Errorf(
					"stats sections have mismatched SortSchema: section 0 has %q, section %d has %q",
					firstSortSchema, i, row.SortSchema,
				)
			}
		}
	}

	return postingsSections, statsSections, nil
}

type runSection struct {
	section *dataobj.Section
	runIdx  int
}

// mergePostingsIntoBuilder merges postings sections using a K-way merge heap and
// feeds results into the builder. Uses D2 last-wins reduction.
func (c *Context) mergePostingsIntoBuilder(ctx context.Context, tenant string, sections []runSection, builder *indexobj.MergeBuilder) error {
	if len(sections) == 0 {
		return nil
	}

	// Open all postings sections and create pile readers.
	// Pre-allocate with known capacity to avoid slice growth allocations.
	pileReaders := make([]pileReader[postingsRow], 0, len(sections))

	for _, rs := range sections {
		sec, err := postings.Open(ctx, rs.section)
		if err != nil {
			return fmt.Errorf("opening postings section from run %d: %w", rs.runIdx, err)
		}

		reader := newPostingsPileReader(sec)
		pileReaders = append(pileReaders, reader)
	}

	// last-wins reducer
	reducer := func(_, next postingsRow) postingsRow {
		level.Warn(c.logger).Log(
			"msg", "IndexMerge: postings full-key collision; last-wins reducer fired",
			"tenant", tenant,
			"kind", next.Kind,
			"object_path", next.ObjectPath,
			"section_index", next.SectionIndex,
			"column_name", next.ColumnName,
			"label_value", next.LabelValue,
		)
		return next
	}

	// Run merge heap
	seq := mergeHeap(ctx, pileReaders, comparePostingsRow, reducer)

	var emitErr error
	err := seq(func(row postingsRow) bool {
		if e := c.writePostingsRow(builder, tenant, row); e != nil {
			emitErr = e
			return false
		}
		return true
	})
	if err != nil {
		return err
	}
	return emitErr
}

func (c *Context) writePostingsRow(builder *indexobj.MergeBuilder, tenant string, row postingsRow) error {
	switch row.Kind {
	case postings.KindLabel:
		entry := postings.LabelEntry{
			ObjectPath:       row.ObjectPath,
			SectionIndex:     row.SectionIndex,
			ColumnName:       row.ColumnName,
			LabelValue:       row.LabelValue,
			StreamIDBitmap:   row.StreamIDBitmap,
			MinTimestamp:     row.MinTimestamp,
			MaxTimestamp:     row.MaxTimestamp,
			UncompressedSize: row.UncompressedSize,
		}
		return builder.AppendPostingsLabelEntry(tenant, entry)

	case postings.KindBloom:
		entry := postings.BloomEntry{
			ObjectPath:       row.ObjectPath,
			SectionIndex:     row.SectionIndex,
			ColumnName:       row.ColumnName,
			BloomFilter:      row.BloomFilter,
			StreamIDBitmap:   row.StreamIDBitmap,
			MinTimestamp:     row.MinTimestamp,
			MaxTimestamp:     row.MaxTimestamp,
			UncompressedSize: row.UncompressedSize,
		}
		return builder.AppendPostingsBloomEntry(tenant, entry)

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

	// Open all stats sections and create pile readers.
	// Pre-allocate with known capacity to avoid slice growth allocations.
	pileReaders := make([]pileReader[statsRow], 0, len(sections))

	for _, rs := range sections {
		sec, err := stats.Open(ctx, rs.section)
		if err != nil {
			return fmt.Errorf("opening stats section from run %d: %w", rs.runIdx, err)
		}

		reader := newStatsPileReader(sec)
		pileReaders = append(pileReaders, reader)
	}

	// D3: aggregate with schema/labels validation
	var reducerErr error

	reducer := func(acc, next statsRow) statsRow {
		// Check schema match
		if acc.SortSchema != next.SortSchema {
			reducerErr = fmt.Errorf(
				"stats rows with equal key have mismatched SortSchema: %q vs %q",
				acc.SortSchema, next.SortSchema,
			)
			return acc
		}

		// Check labels match
		if !labelsEqual(acc.Labels, next.Labels) {
			reducerErr = fmt.Errorf(
				"stats rows with equal key have mismatched Labels: %v vs %v",
				acc.Labels, next.Labels,
			)
			return acc
		}

		// Aggregate timestamps and counts
		if next.MinTimestamp < acc.MinTimestamp {
			acc.MinTimestamp = next.MinTimestamp
		}
		if next.MaxTimestamp > acc.MaxTimestamp {
			acc.MaxTimestamp = next.MaxTimestamp
		}
		acc.RowCount += next.RowCount
		acc.UncompressedSize += next.UncompressedSize

		return acc
	}

	// Run merge heap
	seq := mergeHeap(ctx, pileReaders, compareStatsRow, reducer)

	var emitErr error
	err := seq(func(row statsRow) bool {
		// Stop iteration immediately if the reducer encountered an error.
		if reducerErr != nil {
			return false
		}
		if e := builder.AppendStat(tenant, row.ObjectPath, row.SectionIndex,
			row.SortSchema, row.Labels,
			time.Unix(0, row.MinTimestamp),
			time.Unix(0, row.MaxTimestamp),
			int(row.RowCount),
			row.UncompressedSize); e != nil {
			emitErr = e
			return false
		}
		return true
	})
	if err != nil {
		return err
	}

	// Check if reducer set an error
	if reducerErr != nil {
		return reducerErr
	}

	return emitErr
}

func labelsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		vb, ok := b[k]
		if !ok || vb != v {
			return false
		}
	}
	return true
}
