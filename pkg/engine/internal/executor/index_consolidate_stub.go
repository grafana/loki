package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// =============================================================================
// STUB — replaced by A12.
//
// This is a placeholder implementation of the IndexConsolidate executor. It
// exists solely to let the dataobj-compactor end-to-end workflow test in A11
// run against a complete physical-plan graph: the stub performs an empty PUT
// to the output path and drains immediately, mimicking task Completed without
// any of the real semantics.
//
// The stub deliberately does NOT:
//   - read any compacted log objects,
//   - build any covering index,
//   - perform the primary-ToC GetAndReplace swap,
//   - delete the workflow marker.
//
// PR A12 (index-consolidate-executor) replaces this function with the real
// implementation per spec § Compaction unit step 8–10.
// =============================================================================
func (c *Context) executeIndexConsolidateStub(ctx context.Context, node *physical.IndexConsolidate) Pipeline {
	if c.bucket == nil {
		return errorPipeline(ctx, errors.New("no object store bucket configured"))
	}

	level.Debug(c.logger).Log(
		"msg", "STUB: IndexConsolidate executor invoked — performing empty PUT only",
		"tenant", node.Tenant,
		"output_index_path", node.OutputIndexPath,
	)

	// One side-effecting Read that performs the empty PUT, then EOF. The
	// framework treats a clean EOF (no error) as task Completed.
	var uploaded bool
	return newGenericPipeline(func(ctx context.Context, _ []Pipeline) (arrow.RecordBatch, error) {
		if uploaded {
			return nil, EOF
		}
		if err := c.bucket.Upload(ctx, node.OutputIndexPath, bytes.NewReader(nil)); err != nil {
			return nil, fmt.Errorf("stub: upload empty index object: %w", err)
		}
		uploaded = true
		return nil, EOF
	})
}
