package executor

import (
	"testing"
	"time"

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
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "failed to execute pipeline: plan is nil")
	})

	t.Run("pipeline fails if plan has no root node", func(t *testing.T) {
		ctx := t.Context()
		pipeline := Run(ctx, Config{}, &physical.Plan{}, log.NewNopLogger())
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "failed to execute pipeline: plan has no root node")
	})
}

func TestExecutor_SortMerge(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeSortMerge(ctx, &physical.SortMerge{}, nil)
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})
}

func TestExecutor_Limit(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeLimit(ctx, &physical.Limit{}, nil)
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeLimit(ctx, &physical.Limit{}, []Pipeline{emptyPipeline(), emptyPipeline()})
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "limit expects exactly one input, got 2")
	})
}

func TestExecutor_Filter(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeFilter(ctx, &physical.Filter{}, nil)
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeFilter(ctx, &physical.Filter{}, []Pipeline{emptyPipeline(), emptyPipeline()})
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "filter expects exactly one input, got 2")
	})
}

func TestExecutor_Projection(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeProjection(ctx, &physical.Projection{}, nil)
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("missing column expression results in error", func(t *testing.T) {
		ctx := t.Context()
		cols := []physical.ColumnExpression{}
		c := &Context{}
		pipeline := c.executeProjection(ctx, &physical.Projection{Columns: cols}, []Pipeline{emptyPipeline()})
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "projection expects at least one column, got 0")
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeProjection(ctx, &physical.Projection{}, []Pipeline{emptyPipeline(), emptyPipeline()})
		_, err := pipeline.Read(ctx)
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

	t.Run("parse stage extracts multiple columns from logfmt", func(t *testing.T) {
		// Create input data with message column containing logfmt with multiple fields
		schema := arrow.NewSchema([]arrow.Field{
			{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
		}, nil)

		input := NewArrowtestPipeline(
			memory.DefaultAllocator,
			schema,
			arrowtest.Rows{
				{"message": "level=error status=500 method=POST duration=123ms"},
				{"message": "level=info status=200 method=GET"},
				{"message": "level=debug status=201 method=PUT duration=45ms"},
			},
		)

		// Create ParseNode requesting multiple fields
		parseNode := &physical.ParseNode{
			Kind:          logical.ParserLogfmt,
			RequestedKeys: []string{"level", "status", "method", "duration"},
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

		// Verify the output has message + 4 parsed columns
		outputSchema := record.Schema()
		require.Equal(t, 5, outputSchema.NumFields(), "Expected 5 columns: message, level, status, method, duration")

		// Find column indices
		columnIndices := make(map[string]int)
		for i := 0; i < outputSchema.NumFields(); i++ {
			field := outputSchema.Field(i)
			columnIndices[field.Name] = i
		}

		// Check all expected columns exist
		for _, colName := range []string{"message", "level", "status", "method", "duration"} {
			_, ok := columnIndices[colName]
			require.True(t, ok, "Column %s should exist", colName)
		}

		// Verify values in parsed columns
		levelCol := record.Column(columnIndices["level"]).(*array.String)
		require.Equal(t, "error", levelCol.Value(0))
		require.Equal(t, "info", levelCol.Value(1))
		require.Equal(t, "debug", levelCol.Value(2))

		statusCol := record.Column(columnIndices["status"]).(*array.String)
		require.Equal(t, "500", statusCol.Value(0))
		require.Equal(t, "200", statusCol.Value(1))
		require.Equal(t, "201", statusCol.Value(2))

		methodCol := record.Column(columnIndices["method"]).(*array.String)
		require.Equal(t, "POST", methodCol.Value(0))
		require.Equal(t, "GET", methodCol.Value(1))
		require.Equal(t, "PUT", methodCol.Value(2))

		durationCol := record.Column(columnIndices["duration"]).(*array.String)
		require.Equal(t, "123ms", durationCol.Value(0))
		require.True(t, durationCol.IsNull(1), "Duration should be NULL for second row")
		require.Equal(t, "45ms", durationCol.Value(2))
	})

	t.Run("parse stage preserves existing columns", func(t *testing.T) {
		// Section 6.3 test - create input with multiple existing columns
		schema := arrow.NewSchema([]arrow.Field{
			{Name: types.ColumnNameBuiltinTimestamp, Type: arrow.FixedWidthTypes.Timestamp_ns},
			{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			{Name: "app", Type: arrow.BinaryTypes.String},
		}, nil)

		input := NewArrowtestPipeline(
			memory.DefaultAllocator,
			schema,
			arrowtest.Rows{
				{"timestamp": time.Unix(1, 0).UTC(), "message": "level=error status=500", "app": "frontend"},
				{"timestamp": time.Unix(2, 0).UTC(), "message": "level=info status=200", "app": "backend"},
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

		// Verify all original columns are preserved plus the new parsed column
		outputSchema := record.Schema()
		require.Equal(t, 4, outputSchema.NumFields(), "Expected 4 columns: timestamp, message, app, level")

		// Find column indices
		columnIndices := make(map[string]int)
		for i := 0; i < outputSchema.NumFields(); i++ {
			field := outputSchema.Field(i)
			columnIndices[field.Name] = i
		}

		// Check all original columns exist
		_, hasTimestamp := columnIndices[types.ColumnNameBuiltinTimestamp]
		require.True(t, hasTimestamp, "timestamp column should be preserved")
		_, hasMessage := columnIndices[types.ColumnNameBuiltinMessage]
		require.True(t, hasMessage, "message column should be preserved")
		_, hasApp := columnIndices["app"]
		require.True(t, hasApp, "app column should be preserved")
		_, hasLevel := columnIndices["level"]
		require.True(t, hasLevel, "level column should be added")

		// Verify original column values are unchanged
		timestampCol := record.Column(columnIndices[types.ColumnNameBuiltinTimestamp]).(*array.Timestamp)
		require.Equal(t, arrow.Timestamp(time.Unix(1, 0).UTC().UnixNano()), timestampCol.Value(0))
		require.Equal(t, arrow.Timestamp(time.Unix(2, 0).UTC().UnixNano()), timestampCol.Value(1))

		messageCol := record.Column(columnIndices[types.ColumnNameBuiltinMessage]).(*array.String)
		require.Equal(t, "level=error status=500", messageCol.Value(0))
		require.Equal(t, "level=info status=200", messageCol.Value(1))

		appCol := record.Column(columnIndices["app"]).(*array.String)
		require.Equal(t, "frontend", appCol.Value(0))
		require.Equal(t, "backend", appCol.Value(1))

		// Verify parsed column
		levelCol := record.Column(columnIndices["level"]).(*array.String)
		require.Equal(t, "error", levelCol.Value(0))
		require.Equal(t, "info", levelCol.Value(1))
	})
}
