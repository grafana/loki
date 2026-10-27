package executor

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// closeSpyPipeline counts the calls to Close.
type closeSpyPipeline struct {
	closed int
}

func (p *closeSpyPipeline) Open(context.Context) error { return nil }

func (p *closeSpyPipeline) Read(context.Context) (arrow.RecordBatch, error) { return nil, EOF }

func (p *closeSpyPipeline) Close() { p.closed++ }

func TestExecutor(t *testing.T) {
	t.Run("pipeline fails if plan is nil", func(t *testing.T) {
		ctx := t.Context()
		pipeline := Run(ctx, Config{}, nil, log.NewNopLogger())
		_, err := pipeline.Read(ctx)
		require.EqualError(t, err, "failed to execute pipeline: plan is nil")
	})

	t.Run("pipeline fails if plan has no root node", func(t *testing.T) {
		ctx := t.Context()
		pipeline := Run(ctx, Config{}, &physical.Plan{}, log.NewNopLogger())
		_, err := pipeline.Read(ctx)
		require.EqualError(t, err, "failed to execute pipeline: plan has no root node")
	})

	t.Run("pipeline fails if a compaction node has child nodes", func(t *testing.T) {
		var graph dag.Graph[physical.Node]
		parent := graph.Add(&physical.IndexFilter{})
		child := graph.Add(&physical.IndexMerge{})
		require.NoError(t, graph.AddEdge(dag.Edge[physical.Node]{Parent: parent, Child: child}))

		ctx := t.Context()
		pipeline := Run(ctx, Config{}, physical.FromGraph(graph), log.NewNopLogger())
		_, err := pipeline.Read(ctx)
		require.EqualError(t, err, "failed to execute pipeline: IndexFilter does not accept inputs, got 1")
	})

	t.Run("pipeline fails and closes the input if a compaction node has an external input", func(t *testing.T) {
		var graph dag.Graph[physical.Node]
		graph.Add(&physical.LogMerge{})

		input := &closeSpyPipeline{}
		cfg := Config{
			GetExternalInputs: func(context.Context, physical.Node) []Pipeline { return []Pipeline{input} },
		}

		ctx := t.Context()
		pipeline := Run(ctx, cfg, physical.FromGraph(graph), log.NewNopLogger())
		_, err := pipeline.Read(ctx)
		require.EqualError(t, err, "failed to execute pipeline: LogMerge does not accept inputs, got 1")
		require.Equal(t, 1, input.closed)
	})
}
