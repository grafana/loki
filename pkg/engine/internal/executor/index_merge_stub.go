// index_merge_stub.go
//
// STUB IMPLEMENTATION — DO NOT USE FOR PRODUCTION COMPACTION.
//
// Placeholder executor for the IndexMerge physical plan node. Uploads a
// zero-byte object at OutputIndexPath and returns an empty pipeline so
// the engine workflow framework reports the task as completed. The real
// K-way merge over index sections (postings + stats) comes later.
//
// No default code path constructs an IndexMerge node, so this stub is
// only reachable from the compactor coordinator (opt-in via target).

package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

func (c *Context) executeIndexMergeStub(node *physical.IndexMerge) Pipeline {
	return newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
		if c.bucket == nil {
			return errorPipeline(ctx, errors.New("index_merge_stub: no object store bucket configured"))
		}
		if err := c.bucket.Upload(ctx, node.OutputIndexPath, bytes.NewReader(nil)); err != nil {
			return errorPipeline(ctx, fmt.Errorf("index_merge_stub: upload %q: %w", node.OutputIndexPath, err))
		}
		return emptyPipeline()
	}, nil)
}
