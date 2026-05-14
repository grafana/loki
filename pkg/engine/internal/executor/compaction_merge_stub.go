// compaction_merge_stub.go
//
// STUB IMPLEMENTATION — DO NOT USE FOR PRODUCTION COMPACTION.
//
// This file provides a placeholder executor for the CompactionMerge physical
// plan node. It writes a zero-byte object at the node's OutputPath and
// returns an empty pipeline so the engine workflow framework reports the
// task as completed. The real K-way sort-merge implementation lives
// elsewhere; this stub exists so the compactor coordinator can be wired and
// end-to-end-tested before the real merge code is in place.
//
// No default code path constructs a CompactionMerge node, so this stub is
// only reachable from the compactor coordinator (opt-in via target).

package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// executeCompactionMergeStub is the stub executor for *physical.CompactionMerge.
// It defers work until Open() via newLazyPipeline: on Open it uploads a
// zero-byte object at node.OutputPath and then returns an emptyPipeline()
// whose first Read returns EOF.
//
// STUB: real K-way sort-merge will replace this in a follow-up PR.
func (c *Context) executeCompactionMergeStub(node *physical.CompactionMerge) Pipeline {
	return newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
		if c.bucket == nil {
			return errorPipeline(ctx, errors.New("compaction_merge_stub: no object store bucket configured"))
		}
		if err := c.bucket.Upload(ctx, node.OutputPath, bytes.NewReader(nil)); err != nil {
			return errorPipeline(ctx, fmt.Errorf("compaction_merge_stub: upload %q: %w", node.OutputPath, err))
		}
		return emptyPipeline()
	}, nil)
}
