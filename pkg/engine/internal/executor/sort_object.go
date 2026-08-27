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

	objectUploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, c.dataObjectBucket(), c.logger)
	sortedPath, err := objectUploader.Upload(ctx, sorted)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("SortObject: uploading sorted object: %w", err), sortedCloser.Close())
	}

	indexBuilder, err := indexobj.NewBuilder(c.indexobjCfg, c.scratchStore)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("SortObject: creating index builder: %w", err), sortedCloser.Close())
	}
	calculator := dataobjindex.NewCalculator(indexBuilder)
	if err := calculator.Calculate(ctx, c.logger, sorted, sortedPath); err != nil {
		return nil, errors.Join(fmt.Errorf("SortObject: indexing sorted object %q: %w", sortedPath, err), sortedCloser.Close())
	}
	if err := sortedCloser.Close(); err != nil {
		return nil, fmt.Errorf("SortObject: closing sorted object %q: %w", sortedPath, err)
	}

	indexObject, indexCloser, _, err := calculator.Flush()
	if err != nil {
		return nil, fmt.Errorf("SortObject: flushing index: %w", err)
	}
	indexPath, err := dataobjindex.ObjectKey(ctx, indexObject)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("SortObject: generating index path: %w", err), indexCloser.Close())
	}
	if _, err := c.uploadObject(ctx, c.bucket, indexPath, indexObject); err != nil {
		return nil, errors.Join(fmt.Errorf("SortObject: uploading index %q: %w", indexPath, err), indexCloser.Close())
	}
	if err := indexCloser.Close(); err != nil {
		return nil, fmt.Errorf("SortObject: closing index %q: %w", indexPath, err)
	}

	return []v2.ResultArtifact{{Path: indexPath}}, nil
}
