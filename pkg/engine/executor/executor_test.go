package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestExecutor(t *testing.T) {
	t.Run("pipeline fails if plan is nil", func(t *testing.T) {
		ctx := t.Context()
		pipeline := Run(ctx, Config{}, nil, log.NewNopLogger())
		err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "failed to execute pipeline: plan is nil")
	})

	t.Run("pipeline fails if plan has no root node", func(t *testing.T) {
		ctx := t.Context()
		pipeline := Run(ctx, Config{}, &physical.Plan{}, log.NewNopLogger())
		err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "failed to execute pipeline: plan has no root node")
	})
}

func TestExecutor_SortMerge(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeSortMerge(ctx, &physical.SortMerge{}, nil)
		err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})
}

func TestExecutor_Limit(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeLimit(ctx, &physical.Limit{}, nil)
		err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeLimit(ctx, &physical.Limit{}, []Pipeline{emptyPipeline(), emptyPipeline()})
		err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "limit expects exactly one input, got 2")
	})
}

func TestExecutor_Filter(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeFilter(ctx, &physical.Filter{}, nil)
		err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeFilter(ctx, &physical.Filter{}, []Pipeline{emptyPipeline(), emptyPipeline()})
		err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "filter expects exactly one input, got 2")
	})
}

func TestExecutor_Projection(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeProjection(ctx, &physical.Projection{}, nil)
		err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("missing column expression results in error", func(t *testing.T) {
		ctx := t.Context()
		cols := []physical.ColumnExpression{}
		c := &Context{}
		pipeline := c.executeProjection(ctx, &physical.Projection{Columns: cols}, []Pipeline{emptyPipeline()})
		err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "projection expects at least one column, got 0")
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeProjection(ctx, &physical.Projection{}, []Pipeline{emptyPipeline(), emptyPipeline()})
		err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "projection expects exactly one input, got 2")
	})
}

func TestExecutor_Parse(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeParse(ctx, &physical.ParseNode{
			Kind:          logical.ParserLogfmt,
			RequestedKeys: []string{"level", "status"},
		}, nil)
		err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeParse(ctx, &physical.ParseNode{
			Kind:          logical.ParserLogfmt,
			RequestedKeys: []string{"level"},
		}, []Pipeline{emptyPipeline(), emptyPipeline()})
		err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "parse expects exactly one input, got 2")
	})

	t.Run("parse stage transforms records, adding columns parsed from message", func(t *testing.T) {
		// Create input data with message column containing logfmt
		schema := arrow.NewSchema([]arrow.Field{
			{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
		}, nil)

		input := NewArrowtestPipeline(
			memory.DefaultAllocator,
			schema,
			arrowtest.Rows{
				{"message": "level=error status=500"},
				{"message": "level=info status=200"},
			},
		)

		// Create ParseNode requesting "level" field
		parseNode := &physical.ParseNode{
			Kind:          logical.ParserLogfmt,
			RequestedKeys: []string{"level"},
		}

		// Execute parse
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeParse(ctx, parseNode, []Pipeline{input})

		// Read first record
		err := pipeline.Read(ctx)
		require.NoError(t, err)

		record, err := pipeline.Value()
		require.NoError(t, err)
		defer record.Release()

		// Verify the output has both original message column and new level column
		outputSchema := record.Schema()
		require.Equal(t, 2, outputSchema.NumFields(), "Expected 2 columns: message and level")

		// Check column names
		messageFieldIdx := -1
		levelFieldIdx := -1
		for i := 0; i < outputSchema.NumFields(); i++ {
			field := outputSchema.Field(i)
			if field.Name == types.ColumnNameBuiltinMessage {
				messageFieldIdx = i
				require.Equal(t, arrow.BinaryTypes.String, field.Type)
			}
			if field.Name == "level" {
				levelFieldIdx = i
				require.Equal(t, arrow.BinaryTypes.String, field.Type)
			}
		}
		require.NotEqual(t, -1, messageFieldIdx, "message column should exist")
		require.NotEqual(t, -1, levelFieldIdx, "level column should be added")

		// Check values in the level column
		levelCol := record.Column(levelFieldIdx)
		require.Equal(t, 2, levelCol.Len())

		levelStringCol := levelCol.(*array.String)
		require.Equal(t, "error", levelStringCol.Value(0))
		require.Equal(t, "info", levelStringCol.Value(1))
	})
}
