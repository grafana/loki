package executor

import (
	"context"
	"errors"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log/level"
	"github.com/thanos-io/objstore"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/deletion"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// executeDeletionSweep returns a pipeline that sweeps the tenant of node when
// it opens. It emits one record that reports the path of the tenant's file
// list, whatever the outcome of the sweep.
//
// The sweep stores the objects of the tenant that no ToC can reach in the
// file list of the tenant. If node.Delete is true, it deletes the deletion
// candidates. Otherwise it logs a summary and examples of them, and deletes
// nothing.
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
	// Use the data bucket without a fallback to the index bucket. A sweep of
	// the index bucket would scan nothing and store its file list there.
	bucket := c.dataBucket
	if bucket == nil {
		return nil, errors.New("DeletionSweep: no data object bucket configured")
	}

	// Pass c.logger without a tenant label, because the sweeper adds the
	// tenant to its log lines.
	sweeper, err := c.newSweeper(bucket, node)
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

// newSweeper returns a Sweeper that deletes the deletion candidates if
// node.Delete is true, and a dry run Sweeper otherwise.
func (c *Context) newSweeper(bucket objstore.Bucket, node *physical.DeletionSweep) (*deletion.Sweeper, error) {
	if node.Delete {
		return deletion.NewDeletingSweeper(bucket, c.deletionSweepMetrics, c.logger)
	}
	return deletion.NewSweeper(bucket, c.logger)
}
