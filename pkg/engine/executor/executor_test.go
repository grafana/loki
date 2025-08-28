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
}

func TestNewParsePipeline(t *testing.T) {
	for _, tt := range []struct {
		name                 string
		schema               *arrow.Schema
		input                arrowtest.Rows
		expectedFields       int
		expectedParsedOutput map[string][]string
	}{
		{
			name: "parse stage transforms records, adding columns parsed from message",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "level=error status=500"},
				{"message": "level=info status=200"},
				{"message": "level=debug status=201"},
			},
			expectedFields: 3, // 3 columns: message, level, status
			expectedParsedOutput: map[string][]string{
				"level":  {"error", "info", "debug"},
				"status": {"500", "200", "201"},
			},
		},
		{
			name: "parse stage preserves existing columns",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinTimestamp, Type: arrow.FixedWidthTypes.Timestamp_ns},
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
				{Name: "app", Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"timestamp": time.Unix(1, 0).UTC(), "message": "level=error status=500", "app": "frontend"},
				{"timestamp": time.Unix(2, 0).UTC(), "message": "level=info status=200", "app": "backend"},
			},
			expectedFields: 5, // 5 columns: timestamp, message, app, level, status
			expectedParsedOutput: map[string][]string{
				"level":  {"error", "info"},
				"status": {"500", "200"},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Create input data with message column containing logfmt
			input := NewArrowtestPipeline(
				memory.DefaultAllocator,
				tt.schema,
				tt.input,
			)

			// Create ParseNode requesting "level" field
			parseNode := &physical.ParseNode{
				Kind:          logical.ParserLogfmt,
				RequestedKeys: []string{"level", "status"},
			}

			pipeline := NewParsePipeline(parseNode, input, memory.DefaultAllocator)

			// Read first record
			ctx := t.Context()
			err := pipeline.Read(ctx)
			require.NoError(t, err)

			record, err := pipeline.Value()
			require.NoError(t, err)
			defer record.Release()

			// Verify the output has both original message column and new level column
			outputSchema := record.Schema()
			require.Equal(t, tt.expectedFields, outputSchema.NumFields())

			verifyParseOutput(t, record, tt.expectedParsedOutput)
		})
	}
}

func getColumnIndices(schema *arrow.Schema) map[string]int {
	indices := make(map[string]int)
	for i := 0; i < schema.NumFields(); i++ {
		field := schema.Field(i)
		indices[field.Name] = i
	}
	return indices
}

func verifyParseOutput(t *testing.T, record arrow.Record, expectedValues map[string][]string) {
	columnIndices := getColumnIndices(record.Schema())

	for colName, values := range expectedValues {
		idx, ok := columnIndices[colName]
		require.True(t, ok, "Column %s should exist", colName)

		col := record.Column(idx).(*array.String)
		for i, expectedVal := range values {
			require.Equal(t, expectedVal, col.Value(i),
				"Column %s row %d: expected %s", colName, i, expectedVal)
		}
	}
}
