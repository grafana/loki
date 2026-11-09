package executor

import (
	"context"
	"errors"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/deletion"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// executeDeletionSweep returns a pipeline that sweeps the tenant of node when
// it opens. It emits one record that reports the path of the tenant's file
// list, whether the sweep scanned or skipped.
//
// The sweep only records the objects of the tenant in its file list. It does
// not apply delete requests yet.
func (c *Context) executeDeletionSweep(node *physical.DeletionSweep) Pipeline {
	return newLazyPipeline(func(ctx context.Context) Pipeline {
		artifact, err := c.doDeletionSweep(ctx, node)
		if err != nil {
			return errorPipeline(ctx, err)
		}
		rec, err := artifact.ToRecordBatch(memory.DefaultAllocator)
		if err != nil {
			return errorPipeline(ctx, err)
		}
		return NewBufferedPipeline(rec)
	})
}

func (c *Context) doDeletionSweep(ctx context.Context, node *physical.DeletionSweep) (*v2.ResultArtifact, error) {
	if node.Tenant == "" {
		return nil, errors.New("DeletionSweep: malformed plan: empty tenant")
	}
	bucket := c.dataObjectBucket()
	if bucket == nil {
		return nil, errors.New("DeletionSweep: no object store bucket configured")
	}

	logger := log.With(c.logger, "tenant", node.Tenant)
	sweeper, err := deletion.NewSweeper(bucket, logger)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	outcome, err := sweeper.Sweep(ctx, node.Tenant, start)
	if err != nil {
		return nil, err
	}
	level.Info(logger).Log("msg", "deletion sweep finished", "outcome", outcome, "duration", time.Since(start))
	return &v2.ResultArtifact{Path: deletion.FileList{Tenant: node.Tenant}.Path()}, nil
}
