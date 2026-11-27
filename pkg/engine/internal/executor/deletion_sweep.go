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
// list, whatever the outcome of the sweep.
//
// The sweep stores the objects of the tenant that no ToC can reach in the
// file list of the tenant, and logs a summary of the deletion candidates. If
// node.Delete is true, the pipeline then deletes the candidates. Otherwise it
// deletes nothing and logs examples of the candidates.
func (c *Context) executeDeletionSweep(node *physical.DeletionSweep) Pipeline {
	return newLazyPipeline(func(ctx context.Context) Pipeline {
		artifact, err := c.doDeletionSweep(ctx, node)
		if err != nil {
			return errorPipeline(ctx, err)
		}
		rec, err := v2.EncodeResultArtifacts(memory.DefaultAllocator, []v2.ResultArtifact{*artifact})
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
	bucket := c.dataBucket
	if bucket == nil {
		return nil, errors.New("DeletionSweep: no data object bucket configured")
	}

	// Check the metrics before the sweep, so that a task that cannot count its
	// deletes fails before it stores a list.
	if node.Delete && c.deletionSweepMetrics == nil {
		return nil, errors.New("DeletionSweep: metrics are required to delete")
	}

	// Pass c.logger without a tenant label, because the sweeper adds the
	// tenant to its log lines.
	sweeper, err := deletion.NewSweeper(bucket, c.logger)
	if err != nil {
		return nil, err
	}
	logger := log.With(c.logger, "tenant", node.Tenant)
	start := time.Now()
	result, err := sweeper.Sweep(ctx, node.Tenant, start)
	if err != nil {
		return nil, err
	}
	if node.Delete {
		if err := deletion.DeleteObjects(ctx, bucket, result.Candidates, c.deletionSweepMetrics, logger); err != nil {
			return nil, err
		}
	} else {
		deletion.LogCandidates(ctx, bucket, result.Candidates, logger)
	}
	level.Info(logger).Log(
		"msg", "deletion sweep finished",
		"outcome", result.Outcome,
		"dry_run", !node.Delete,
		"candidates", len(result.Candidates),
		"duration", time.Since(start),
	)
	return &v2.ResultArtifact{Path: deletion.FileList{Tenant: node.Tenant}.Path()}, nil
}
