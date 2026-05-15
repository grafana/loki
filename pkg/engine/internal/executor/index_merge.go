package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
func (c *Context) executeIndexMerge(ctx context.Context, node *physical.IndexMerge) Pipeline {
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
	builder, err := indexobj.NewBuilder(c.indexobjCfg, c.scratchStore)
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

// classifyRuns opens each source object and classifies sections as postings or stats.
// Returns lists of (section, runIdx) tuples for each kind, and validates that no run
// mixes both kinds.
func (c *Context) classifyRuns(ctx context.Context, node *physical.IndexMerge) (
	postingsSections []runSection,
	statsSections []runSection,
	err error,
) {
	// Cache objects by path to avoid reopening
	objectCache := make(map[string]*dataobj.Object)

	for runIdx, runRef := range node.Runs {
		hasPostings := false
		hasStats := false

		for secIdx, sectionRef := range runRef.Sections {
			// Open object if not cached
			obj, ok := objectCache[sectionRef.ObjectPath]
			if !ok {
				var openErr error
				obj, openErr = dataobj.FromBucket(ctx, c.bucket, sectionRef.ObjectPath, 0)
				if openErr != nil {
					return nil, nil, fmt.Errorf("opening object %q: %w", sectionRef.ObjectPath, openErr)
				}
				objectCache[sectionRef.ObjectPath] = obj
			}

			// Validate bounds: check for negative or out-of-range indices.
			sections := obj.Sections()
			if sectionRef.SectionIndex < 0 || int(sectionRef.SectionIndex) >= len(sections) {
				return nil, nil, fmt.Errorf(
					"run %d section %d: SectionIndex %d out of range [0, %d)",
					runIdx, secIdx, sectionRef.SectionIndex, len(sections),
				)
			}

			sec := sections[sectionRef.SectionIndex]

			// Classify the section and track what kinds we've seen in this run
			isPostings := postings.CheckSection(sec)
			isStats := stats.CheckSection(sec)

			if !isPostings && !isStats {
				return nil, nil, fmt.Errorf(
					"run %d section %d: unknown section type %q",
					runIdx, sectionRef.SectionIndex, sec.Type,
				)
			}

			if isPostings {
				hasPostings = true
				postingsSections = append(postingsSections, runSection{
					section: sec,
					runIdx:  runIdx,
				})
			} else {
				hasStats = true
				statsSections = append(statsSections, runSection{
					section: sec,
					runIdx:  runIdx,
				})
			}
		}

		// Validate that this run doesn't mix both kinds
		if hasPostings && hasStats {
			return nil, nil, fmt.Errorf("run %d: heterogeneous section kinds (both postings and stats)", runIdx)
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
func (c *Context) mergePostingsIntoBuilder(ctx context.Context, tenant string, sections []runSection, builder *indexobj.Builder) error {
	if len(sections) == 0 {
		return nil
	}

	// Open all postings sections and create pile readers
	var pileReaders []pileReader[postingsRow]

	for _, rs := range sections {
		sec, err := postings.Open(ctx, rs.section)
		if err != nil {
			return fmt.Errorf("opening postings section from run %d: %w", rs.runIdx, err)
		}

		reader := newPostingsPileReader(sec)
		pileReaders = append(pileReaders, reader)
	}

	// D2: last-wins reducer
	reducer := func(acc, next postingsRow) postingsRow {
		level.Warn(c.logger).Log(
			"msg", "IndexMerge: postings full-key collision; D2 last-wins reducer fired",
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

func (c *Context) writePostingsRow(builder *indexobj.Builder, tenant string, row postingsRow) error {
	switch row.Kind {
	case postings.KindLabel:
		entry := postings.LabelEntry{
			ObjectPath:       row.ObjectPath,
			SectionIndex:     row.SectionIndex,
			ColumnName:       row.ColumnName,
			LabelValue:       row.LabelValue,
			StreamIDBitmap:   row.StreamIDBitmap,
			MinTimestamp:     time.Unix(0, row.MinTimestamp),
			MaxTimestamp:     time.Unix(0, row.MaxTimestamp),
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
			MinTimestamp:     time.Unix(0, row.MinTimestamp),
			MaxTimestamp:     time.Unix(0, row.MaxTimestamp),
			UncompressedSize: row.UncompressedSize,
		}
		return builder.AppendPostingsBloomEntry(tenant, entry)

	default:
		return fmt.Errorf("unknown postings kind: %v", row.Kind)
	}
}

// mergeStatsIntoBuilder merges stats sections using a K-way merge heap and
// feeds results into the builder. Uses D3 aggregation with schema/label validation.
func (c *Context) mergeStatsIntoBuilder(ctx context.Context, tenant string, sections []runSection, builder *indexobj.Builder) error {
	if len(sections) == 0 {
		return nil
	}

	// Open all stats sections and create pile readers
	var pileReaders []pileReader[statsRow]

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
