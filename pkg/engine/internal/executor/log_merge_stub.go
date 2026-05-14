// log_merge_stub.go
//
// STUB IMPLEMENTATION — DO NOT USE FOR PRODUCTION COMPACTION.
//
// Placeholder executor for the LogMerge physical plan node. Uploads a
// zero-byte object at OutputPath and returns an empty pipeline. The real
// K-way merge over log sections lands in v2.0.
//
// No default code path constructs a LogMerge node, so this stub is only
// reachable from the compactor coordinator (opt-in via target).

package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

func (c *Context) executeLogMergeStub(node *physical.LogMerge) Pipeline {
	return newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
		if c.bucket == nil {
			return errorPipeline(ctx, errors.New("log_merge_stub: no object store bucket configured"))
		}
		if err := c.bucket.Upload(ctx, node.OutputPath, bytes.NewReader(nil)); err != nil {
			return errorPipeline(ctx, fmt.Errorf("log_merge_stub: upload %q: %w", node.OutputPath, err))
		}
		return emptyPipeline()
	}, nil)
}
