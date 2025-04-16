package executor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

func TestExecutor(t *testing.T) {
	t.Run("pipeline fails if plan is nil", func(t *testing.T) {
		pipeline := Run(context.TODO(), Config{}, nil)
		err := pipeline.Read()
		require.ErrorContains(t, err, "failed to execute pipeline: plan is nil")
	})

	t.Run("pipeline fails if plan has no root node", func(t *testing.T) {
		pipeline := Run(context.TODO(), Config{}, &physical.Plan{})
		err := pipeline.Read()
		require.ErrorContains(t, err, "failed to execute pipeline: plan has no root node")
	})
}

func TestExecutor_DataObjScan(t *testing.T) {
	t.Run("is not implemented", func(t *testing.T) {
		c := &Context{}
		pipeline := c.executeDataObjScan(context.TODO(), &physical.DataObjScan{})
		err := pipeline.Read()
		require.ErrorContains(t, err, errNotImplemented.Error())
	})
}

func TestExecutor_SortMerge(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		c := &Context{}
		pipeline := c.executeSortMerge(context.TODO(), &physical.SortMerge{}, nil)
		err := pipeline.Read()
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("is not implemented", func(t *testing.T) {
		c := &Context{}
		pipeline := c.executeSortMerge(context.TODO(), &physical.SortMerge{}, []Pipeline{emptyPipeline()})
		err := pipeline.Read()
		require.ErrorContains(t, err, errNotImplemented.Error())
	})
}

func TestExecutor_Limit(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		c := &Context{}
		pipeline := c.executeLimit(context.TODO(), &physical.Limit{}, nil)
		err := pipeline.Read()
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("is not implemented", func(t *testing.T) {
		c := &Context{}
		pipeline := c.executeLimit(context.TODO(), &physical.Limit{}, []Pipeline{emptyPipeline()})
		err := pipeline.Read()
		require.ErrorContains(t, err, errNotImplemented.Error())
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		c := &Context{}
		pipeline := c.executeLimit(context.TODO(), &physical.Limit{}, []Pipeline{emptyPipeline(), emptyPipeline()})
		err := pipeline.Read()
		require.ErrorContains(t, err, "limit expects exactly one input, got 2")
	})
}

func TestExecutor_Filter(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		c := &Context{}
		pipeline := c.executeFilter(context.TODO(), &physical.Filter{}, nil)
		err := pipeline.Read()
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("is not implemented", func(t *testing.T) {
		c := &Context{}
		pipeline := c.executeFilter(context.TODO(), &physical.Filter{}, []Pipeline{emptyPipeline()})
		err := pipeline.Read()
		require.ErrorContains(t, err, errNotImplemented.Error())
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		c := &Context{}
		pipeline := c.executeFilter(context.TODO(), &physical.Filter{}, []Pipeline{emptyPipeline(), emptyPipeline()})
		err := pipeline.Read()
		require.ErrorContains(t, err, "filter expects exactly one input, got 2")
	})
}

func TestExecutor_Projection(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		c := &Context{}
		pipeline := c.executeProjection(context.TODO(), &physical.Projection{}, nil)
		err := pipeline.Read()
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("missing column expression results in error", func(t *testing.T) {
		cols := []physical.ColumnExpression{}
		c := &Context{}
		pipeline := c.executeProjection(context.TODO(), &physical.Projection{Columns: cols}, []Pipeline{emptyPipeline()})
		err := pipeline.Read()
		require.ErrorContains(t, err, "projection expects at least one column, got 0")
	})

	t.Run("is not implemented", func(t *testing.T) {
		cols := []physical.ColumnExpression{
			&physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "a",
					Type:   types.ColumnTypeBuiltin,
				},
			},
		}
		c := &Context{}
		pipeline := c.executeProjection(context.TODO(), &physical.Projection{Columns: cols}, []Pipeline{emptyPipeline()})
		err := pipeline.Read()
		require.ErrorContains(t, err, errNotImplemented.Error())
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		c := &Context{}
		pipeline := c.executeProjection(context.TODO(), &physical.Projection{}, []Pipeline{emptyPipeline(), emptyPipeline()})
		err := pipeline.Read()
		require.ErrorContains(t, err, "projection expects exactly one input, got 2")
	})
}
