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
	reader := newStatsPileReader(ctx, statsSec, 0)
	defer reader.Close()
	var row stats.Stat
	if reader.Next() {
		row = reader.Value()
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

	// Open all postings sections and create pile readers.
	pileReaders := make([]pileSequence[postings.Row], 0, len(sections))

	for i, rs := range sections {
		sec, err := postings.Open(ctx, rs.section)
		if err != nil {
			return fmt.Errorf("opening postings section from run %d: %w", rs.runIdx, err)
		}

		reader := newPostingsPileReader(ctx, sec, i)
		pileReaders = append(pileReaders, reader)
	}

	// A collision on the full sort key (Kind, ObjectPath, SectionIndex,
	// ColumnName, LabelValue) means two source indexes reference the same
	// physical section/column/label — this shouldn't happen so log a warning and
	// emit a metric for tracking. The data is logically equivalent so keep one.
	reducer := func(_, next postings.Row) postings.Row {
		if region := xcap.RegionFromContext(ctx); region != nil {
			region.Record(xcap.StatIndexMergeDuplicatePostings.Observe(1))
		}
		level.Warn(c.logger).Log(
			"msg", "IndexMerge: postings full-key collision",
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
	err := seq(func(row postings.Row) bool {
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

	// Open all stats sections and create pile readers.
	// Pre-allocate with known capacity to avoid slice growth allocations.
	pileReaders := make([]pileSequence[stats.Stat], 0, len(sections))

	for i, rs := range sections {
		sec, err := stats.Open(ctx, rs.section)
		if err != nil {
			return fmt.Errorf("opening stats section from run %d: %w", rs.runIdx, err)
		}

		reader := newStatsPileReader(ctx, sec, i)
		pileReaders = append(pileReaders, reader)
	}

	// The comparator includes (ObjectPath, SectionIndex) as final tiebreakers, so
	// an equal-key collision here means two source indexes reference the same
	// physical (ObjectPath, SectionIndex) — which shouldn't happen. SortSchema
	// and Labels are guaranteed to match on such collisions (same source
	// section), as are the aggregate counts; keeping one row, warn, and
	// observe an xcap statistic.
	reducer := func(_, next stats.Stat) stats.Stat {
		if region := xcap.RegionFromContext(ctx); region != nil {
			region.Record(xcap.StatIndexMergeDuplicateStats.Observe(1))
		}
		level.Warn(c.logger).Log(
			"msg", "IndexMerge: stats full-key collision",
			"tenant", tenant,
			"object_path", next.ObjectPath,
			"section_index", next.SectionIndex,
			"min_timestamp", next.MinTimestamp,
			"max_timestamp", next.MaxTimestamp,
		)
		return next
	}

	// Run merge heap
	seq := mergeHeap(ctx, pileReaders, compareStatsRow, reducer)

	var emitErr error
	err := seq(func(row stats.Stat) bool {
		if e := builder.AppendStat(tenant, row); e != nil {
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
