package executor

import (
	"context"
	"errors"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log/level"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/deletion"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// executeDeletionSweep returns a pipeline that sweeps the tenant of node when
// it opens. It emits one record that reports the path of the tenant's file
// list, whatever the outcome of the sweep.
//
// The sweep stores the objects of the tenant that no ToC can reach in the
// file list of the tenant. It logs a summary and examples of the deletion
// candidates, and deletes nothing.
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
	// Use the data bucket without a fallback to the index bucket. A sweep of
	// the index bucket would scan nothing and store its file list there.
	bucket := c.dataBucket
	if bucket == nil {
		return nil, errors.New("DeletionSweep: no data object bucket configured")
	}

	// Pass c.logger without a tenant label, because the sweeper adds the
	// tenant to its log lines.
	sweeper, err := deletion.NewSweeper(bucket, c.logger)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	outcome, err := sweeper.Sweep(ctx, node.Tenant, start)
	if err != nil {
		return nil, err
	}
	level.Info(c.logger).Log("msg", "deletion sweep finished", "tenant", node.Tenant, "outcome", outcome, "duration", time.Since(start))
	return &v2.ResultArtifact{Path: deletion.FileList{Tenant: node.Tenant}.Path()}, nil
}
