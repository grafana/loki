package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
)

// executeIndexFilter copies the rows of the node's log objects from the
// source index into a new index object, and emits a record that reports the
// content-hash path of the new index.
func (c *Context) executeIndexFilter(node *physical.IndexFilter) Pipeline {
	return newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
		artifact, err := c.doIndexFilter(ctx, node)
		if err != nil {
			return errorPipeline(ctx, err)
		}
		rec, err := artifact.ToRecordBatch(memory.DefaultAllocator)
		if err != nil {
			return errorPipeline(ctx, err)
		}
		return NewBufferedPipeline(rec)
	}, nil)
}

func (c *Context) doIndexFilter(ctx context.Context, node *physical.IndexFilter) (*v2.ResultArtifact, error) {
	if c.bucket == nil {
		return nil, errors.New("no object store bucket configured")
	}
	if node.Tenant == "" || node.SourceIndexPath == "" {
		return nil, fmt.Errorf("IndexFilter: malformed plan: tenant %q, source index %q", node.Tenant, node.SourceIndexPath)
	}
	if len(node.ObjectPaths) == 0 {
		return nil, errors.New("IndexFilter: no object paths to keep")
	}

	keep := make(map[string]struct{}, len(node.ObjectPaths))
	for _, path := range node.ObjectPaths {
		keep[path] = struct{}{}
	}

	source, err := dataobj.FromBucket(ctx, c.bucket, node.SourceIndexPath, 0)
	if err != nil {
		return nil, fmt.Errorf("opening source index %q: %w", node.SourceIndexPath, err)
	}

	builder, err := indexobj.NewMergeBuilder(c.indexobjCfg, c.scratchStore)
	if err != nil {
		return nil, fmt.Errorf("creating index builder: %w", err)
	}
	defer builder.Reset()

	foundPostings := make(map[string]struct{}, len(keep))
	foundStats := make(map[string]struct{}, len(keep))
	for _, sec := range source.Sections() {
		if sec.Tenant != node.Tenant {
			continue
		}
		switch {
		case postings.CheckSection(sec):
			err = copyRows(ctx, sec, openPostingsReader, keep, foundPostings, func(row postings.Row) string { return row.ObjectPath },
				func(row postings.Row) error { return c.writePostingsRow(builder, node.Tenant, row) })
		case stats.CheckSection(sec):
			err = copyRows(ctx, sec, openStatsReader, keep, foundStats, func(row stats.Stat) string { return row.ObjectPath },
				func(row stats.Stat) error { return builder.AppendStat(node.Tenant, row) })
		case pointers.CheckSection(sec):
			// Pointers sections may exist for fresh indexes until this section is no longer built.
			// The compactor does not support them.
			continue
		case streams.CheckSection(sec):
			// Streams sections may exist for fresh indexes until this section is no longer built.
			// The compactor does not support them.
			continue
		default:
			return nil, fmt.Errorf("filtering source index %q: unknown section %q", node.SourceIndexPath, sec)
		}
		if err != nil {
			return nil, fmt.Errorf("filtering source index %q: %w", node.SourceIndexPath, err)
		}
	}

	// A kept object without postings rows or without stats rows means the
	// planner and the source index disagree. Fail rather than publish an
	// index that cannot find the object, or cannot plan it.
	for _, path := range node.ObjectPaths {
		if _, ok := foundPostings[path]; !ok {
			return nil, fmt.Errorf("source index %q has no postings rows for object %q", node.SourceIndexPath, path)
		}
		if _, ok := foundStats[path]; !ok {
			return nil, fmt.Errorf("source index %q has no stats rows for object %q", node.SourceIndexPath, path)
		}
	}

	artifact, _, _, err := c.uploadIndex(ctx, node.Tenant, builder)
	if err != nil {
		return nil, fmt.Errorf("index filter output for source index %q: %w", node.SourceIndexPath, err)
	}
	return artifact, nil
}

// copyRows passes each row of sec whose object path is in keep to write, and
// records that object path in found.
func copyRows[R any](
	ctx context.Context,
	sec *dataobj.Section,
	open func(context.Context, *dataobj.Section) (iter.CloseIterator[R], error),
	keep, found map[string]struct{},
	objectPath func(R) string,
	write func(R) error,
) error {
	rows, err := open(ctx, sec)
	if err != nil {
		return err
	}
	for rows.Next() {
		row := rows.At()
		path := objectPath(row)
		if _, ok := keep[path]; !ok {
			continue
		}
		found[path] = struct{}{}
		if err := write(row); err != nil {
			return errors.Join(err, rows.Close())
		}
	}
	return errors.Join(rows.Err(), rows.Close())
}
