package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	dataobjindex "github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

func (c *Context) executeSortObject(node *physical.SortObject) Pipeline {
	return newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
		artifacts, err := c.doSortObject(ctx, node)
		if err != nil {
			return errorPipeline(ctx, err)
		}
		return NewBufferedPipeline(v2.BuildResultRecord(memory.DefaultAllocator, artifacts))
	}, nil)
}

func (c *Context) doSortObject(ctx context.Context, node *physical.SortObject) ([]v2.ResultArtifact, error) {
	if c.bucket == nil {
		return nil, errors.New("no index object store bucket configured")
	}
	if c.dataObjectBucket() == nil {
		return nil, errors.New("no data object store bucket configured")
	}
	if node.SourceObjectPath == "" {
		return nil, errors.New("SortObject: source object path is empty")
	}

	source, err := dataobj.FromBucket(ctx, c.dataObjectBucket(), node.SourceObjectPath, 0)
	if err != nil {
		return nil, fmt.Errorf("SortObject: opening source %q: %w", node.SourceObjectPath, err)
	}

	builder, err := logsobj.NewBuilder(
		logsobj.BuilderConfig{
			BuilderBaseConfig:    c.logsobjCfg,
			AppendOrderedEnabled: true,
		},
		c.scratchStore,
		logsobj.NewBuilderMetrics(),
		c.logger,
		fixedSortSchema(node.SortSchema),
	)
	if err != nil {
		return nil, fmt.Errorf("SortObject: creating sorter: %w", err)
	}
	sorted, sortedCloser, err := builder.CopyAndSort(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("SortObject: sorting source %q: %w", node.SourceObjectPath, err)
	}

	indexBuilder, err := indexobj.NewBuilder(c.indexobjCfg, c.scratchStore)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("SortObject: creating index builder: %w", err), sortedCloser.Close())
	}
	calculator := dataobjindex.NewCalculator(indexBuilder)

	sortedPath, err := uploader.ObjectKey(ctx, sorted, 2)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("SortObject: generating sorted object path: %w", err), sortedCloser.Close())
	}
	if _, err := c.uploadAndIndexObject(ctx, sorted, sortedCloser, sortedPath, calculator); err != nil {
		return nil, fmt.Errorf("SortObject: writing sorted object: %w", err)
	}

	indexPath, err := c.flushAndUploadIndex(ctx, calculator, dataobjindex.ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("SortObject: writing index: %w", err)
	}

	return []v2.ResultArtifact{{Path: indexPath}}, nil
}
