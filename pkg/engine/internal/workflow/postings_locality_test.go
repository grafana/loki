package workflow

import (
	"bytes"
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func TestRatio(t *testing.T) {
	require.Equal(t, 0.0, ratio(0, 0), "no pages -> zero, not NaN")
	require.Equal(t, 0.0, ratio(5, 0), "zero total is guarded against divide-by-zero")
	require.Equal(t, 0.25, ratio(1, 4))
	require.Equal(t, 1.0, ratio(4, 4))
	// Relevant pages accumulate across predicate builds while total does not, so
	// the relevance ratio can legitimately exceed 1.0.
	require.Equal(t, 2.5, ratio(5, 2))
}

func TestIsPostingsScanTask(t *testing.T) {
	t.Run("PointersScan node is a postings scan", func(t *testing.T) {
		g := dag.Graph[physical.Node]{}
		g.Add(&physical.PointersScan{})
		task := &Task{ULID: ulid.Make(), Fragment: physical.FromGraph(g)}
		require.True(t, isPostingsScanTask(task))
	})

	t.Run("DataObjScan node is not a postings scan", func(t *testing.T) {
		g := dag.Graph[physical.Node]{}
		g.Add(&physical.DataObjScan{})
		task := &Task{ULID: ulid.Make(), Fragment: physical.FromGraph(g)}
		require.False(t, isPostingsScanTask(task))
	})

	t.Run("empty fragment is not a postings scan", func(t *testing.T) {
		task := &Task{ULID: ulid.Make(), Fragment: physical.FromGraph(dag.Graph[physical.Node]{})}
		require.False(t, isPostingsScanTask(task))
	})
}

// TestPrintTaskSummary_LocalityRouting verifies that printTaskSummary routes a
// task to the correct locality summary. A PointersScan task must produce the
// postings-locality line, not the log-locality line — a PointersScan also
// satisfies isScanTask, so this guards the branch ordering in printTaskSummary.
func TestPrintTaskSummary_LocalityRouting(t *testing.T) {
	t.Run("PointersScan routes to postings locality", func(t *testing.T) {
		out := runTaskSummary(t, &physical.PointersScan{})
		require.Contains(t, out, "task-postings-locality-summary")
		require.NotContains(t, out, "task-log-locality-summary")
	})

	t.Run("DataObjScan routes to log locality", func(t *testing.T) {
		out := runTaskSummary(t, &physical.DataObjScan{})
		require.Contains(t, out, "task-log-locality-summary")
		require.NotContains(t, out, "task-postings-locality-summary")
	})
}

// runTaskSummary drives printTaskSummary for a single-node fragment and returns
// the emitted log output.
func runTaskSummary(t *testing.T, node physical.Node) string {
	t.Helper()

	g := dag.Graph[physical.Node]{}
	g.Add(node)
	task := &Task{ULID: ulid.Make(), Fragment: physical.FromGraph(g)}

	var buf bytes.Buffer
	wf := &Workflow{
		logger: log.NewLogfmtLogger(&buf),
		graph:  dag.Graph[*Task]{},
	}

	_, capture := xcap.NewCapture(context.Background(), nil)
	summary := pendingSummary{
		result:         TaskResult{Outcome: TaskOutcomeCompleted, Capture: capture},
		operatorType:   taskOperatorType(task),
		isScanTask:     isScanTask(task),
		isPostingsScan: isPostingsScanTask(task),
	}
	wf.printTaskSummary(task, summary, false)

	return buf.String()
}
