package executor

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// =============================================================================
// STUB — replaced by A12.
//
// This is a placeholder implementation of the TableOfContentsConsolidate
// executor. It exists solely to let the dataobj-compactor end-to-end workflow
// test in A11 run against a complete physical-plan graph: the stub logs a
// single message and drains immediately, mimicking task Completed without
// invoking any ToC mutation.
//
// In v1.0 the real executor's sole responsibility is one
// `metastoreWriter.ReplaceIndexPointers` call that removes
// `RemoveIndexPaths` and adds `AddIndexPaths` on the primary ToC for the
// (Tenant, ToCWindowStart) window. The stub deliberately does NOT make that
// call.
//
// The stub also deliberately does NOT:
//   - read any compacted log object,
//   - build any index,
//   - upload anything to the bucket,
//   - delete any marker (the v1.0 design has no in-flight markers).
//
// PR A12 (table-of-contents-consolidate-executor) replaces this function
// with the real implementation per spec § "v1.0 milestone: index-only
// compaction".
// =============================================================================
func (c *Context) executeTableOfContentsConsolidateStub(_ context.Context, node *physical.TableOfContentsConsolidate) Pipeline {
	level.Debug(c.logger).Log(
		"msg", "STUB: TableOfContentsConsolidate executor invoked — no-op",
		"tenant", node.Tenant,
		"toc_window_start", node.ToCWindowStart,
		"num_remove", len(node.RemoveIndexPaths),
		"num_add", len(node.AddIndexPaths),
	)

	// Return EOF on the first Read so the framework records Completed.
	return newGenericPipeline(func(_ context.Context, _ []Pipeline) (arrow.RecordBatch, error) {
		return nil, EOF
	})
}
