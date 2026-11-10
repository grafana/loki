package executor

import (
	"context"
	"errors"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// errDeletionSweepNotImplemented fails every well-formed DeletionSweep task.
// Failing is safer than reporting success for a sweep that deleted nothing.
var errDeletionSweepNotImplemented = errors.New("DeletionSweep: not implemented")

// executeDeletionSweep returns a pipeline that fails when it opens. It fails
// with a malformed-plan error if the node has no tenant, and with
// errDeletionSweepNotImplemented otherwise.
func (c *Context) executeDeletionSweep(node *physical.DeletionSweep) Pipeline {
	return newLazyPipeline(func(ctx context.Context) Pipeline {
		if node.Tenant == "" {
			return errorPipeline(ctx, errors.New("DeletionSweep: malformed plan: empty tenant"))
		}
		return errorPipeline(ctx, errDeletionSweepNotImplemented)
	})
}
