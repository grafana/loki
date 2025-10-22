package executor

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
)

func TestExecutor(t *testing.T) {
	t.Run("pipeline fails if plan is nil", func(t *testing.T) {
		ctx := t.Context()
		pipeline := Run(ctx, Config{}, nil, log.NewNopLogger())
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "failed to execute pipeline: plan is nil")
	})

	t.Run("pipeline fails if plan has no root node", func(t *testing.T) {
		ctx := t.Context()
		pipeline := Run(ctx, Config{}, &physicalpb.Plan{}, log.NewNopLogger())
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "failed to execute pipeline: plan has no root node")
	})
}

func TestExecutor_SortMerge(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeSortMerge(ctx, &physicalpb.SortMerge{}, nil)
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})
}

func TestExecutor_Limit(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeLimit(ctx, &physicalpb.Limit{}, nil)
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeLimit(ctx, &physicalpb.Limit{}, []Pipeline{emptyPipeline(), emptyPipeline()})
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "limit expects exactly one input, got 2")
	})
}

func TestExecutor_Filter(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeFilter(ctx, &physicalpb.Filter{}, nil)
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeFilter(ctx, &physicalpb.Filter{}, []Pipeline{emptyPipeline(), emptyPipeline()})
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "filter expects exactly one input, got 2")
	})
}

func TestExecutor_Projection(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeProjection(ctx, &physicalpb.Projection{}, nil)
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("missing column expression results in error", func(t *testing.T) {
		ctx := t.Context()
		cols := []*physicalpb.ColumnExpression{}
		c := &Context{}
		pipeline := c.executeProjection(ctx, &physicalpb.Projection{Columns: cols}, []Pipeline{emptyPipeline()})
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "projection expects at least one column, got 0")
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeProjection(ctx, &physicalpb.Projection{}, []Pipeline{emptyPipeline(), emptyPipeline()})
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "projection expects exactly one input, got 2")
	})
}

func TestExecutor_Parse(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeParse(ctx, &physicalpb.Parse{
			Operation:     physicalpb.PARSE_OP_LOGFMT,
			RequestedKeys: []string{"level", "status"},
		}, nil)
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeParse(ctx, &physicalpb.Parse{
			Operation:     physicalpb.PARSE_OP_LOGFMT,
			RequestedKeys: []string{"level"},
		}, []Pipeline{emptyPipeline(), emptyPipeline()})
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "parse expects exactly one input, got 2")
	})
}
