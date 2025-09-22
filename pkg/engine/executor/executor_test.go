package executor

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
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
			Kind:          physical.ParserLogfmt,
			RequestedKeys: []string{"level", "status"},
		}, nil)
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeParse(ctx, &physical.ParseNode{
			Kind:          physical.ParserLogfmt,
			RequestedKeys: []string{"level"},
		}, []Pipeline{emptyPipeline(), emptyPipeline()})
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "parse expects exactly one input, got 2")
	})
}

func TestNewParsePipeline(t *testing.T) {
	for _, tt := range []struct {
		name           string
		schema         *arrow.Schema
		input          arrowtest.Rows
		requestedKeys  []string
		expectedFields int
		expectedOutput arrowtest.Rows
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
			requestedKeys:  []string{"level", "status"},
			expectedFields: 3, // 3 columns: message, level, status
			expectedOutput: arrowtest.Rows{
				{"message": "level=error status=500", "level": "error", "status": "500"},
				{"message": "level=info status=200", "level": "info", "status": "200"},
				{"message": "level=debug status=201", "level": "debug", "status": "201"},
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
			requestedKeys:  []string{"level", "status"},
			expectedFields: 5, // 5 columns: timestamp, message, app, level, status
			expectedOutput: arrowtest.Rows{
				{"timestamp": time.Unix(1, 0).UTC(), "message": "level=error status=500", "app": "frontend", "level": "error", "status": "500"},
				{"timestamp": time.Unix(2, 0).UTC(), "message": "level=info status=200", "app": "backend", "level": "info", "status": "200"},
			},
		},
		{
			name: "handle missing keys with NULL",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "level=error"},
				{"message": "status=200"},
				{"message": "level=info"},
			},
			requestedKeys:  []string{"level"},
			expectedFields: 2, // 2 columns: message, level
			expectedOutput: arrowtest.Rows{
				{"message": "level=error", "level": "error"},
				{"message": "status=200", "level": nil},
				{"message": "level=info", "level": "info"},
			},
		},
		{
			name: "handle errors with error columns",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "level=info status=200"},       // No errors
				{"message": "status==value level=error"},   // Double equals error on requested key
				{"message": "level=\"unclosed status=500"}, // Unclosed quote error
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 5, // 5 columns: message, level, status, __error__, __error_details__
			expectedOutput: arrowtest.Rows{
				{"message": "level=info status=200", "level": "info", "status": "200", types.ColumnNameParsedError: nil, types.ColumnNameParsedErrorDetails: nil},
				{"message": "status==value level=error", "level": nil, "status": nil, types.ColumnNameParsedError: types.LogfmtParserErrorType, types.ColumnNameParsedErrorDetails: "logfmt syntax error at pos 8 : unexpected '='"},
				{"message": "level=\"unclosed status=500", "level": nil, "status": nil, types.ColumnNameParsedError: types.LogfmtParserErrorType, types.ColumnNameParsedErrorDetails: "logfmt syntax error at pos 27 : unterminated quoted value"},
			},
		},
		{
			name: "extract all keys when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "level=info status=200 method=GET"},
				{"message": "level=warn code=304"},
				{"message": "level=error status=500 method=POST duration=123ms"},
			},
			requestedKeys:  nil, // nil means extract all keys
			expectedFields: 6,   // 6 columns: message, code, duration, level, method, status
			expectedOutput: arrowtest.Rows{
				{"message": "level=info status=200 method=GET", "code": nil, "duration": nil, "level": "info", "method": "GET", "status": "200"},
				{"message": "level=warn code=304", "code": "304", "duration": nil, "level": "warn", "method": nil, "status": nil},
				{"message": "level=error status=500 method=POST duration=123ms", "code": nil, "duration": "123ms", "level": "error", "method": "POST", "status": "500"},
			},
		},
		{
			name: "extract all keys with errors when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "level=info status=200 method=GET"},       // Valid line
				{"message": "level==error code=500"},                  // Double equals error
				{"message": "msg=\"unclosed duration=100ms code=400"}, // Unclosed quote error
				{"message": "level=debug method=POST"},                // Valid line
			},
			requestedKeys:  nil, // nil means extract all keys
			expectedFields: 6,   // 6 columns: message, level, method, status, __error__, __error_details__
			expectedOutput: arrowtest.Rows{
				{"message": "level=info status=200 method=GET", "level": "info", "method": "GET", "status": "200", types.ColumnNameParsedError: nil, types.ColumnNameParsedErrorDetails: nil},
				{"message": "level==error code=500", "level": nil, "method": nil, "status": nil, types.ColumnNameParsedError: types.LogfmtParserErrorType, types.ColumnNameParsedErrorDetails: "logfmt syntax error at pos 7 : unexpected '='"},
				{"message": "msg=\"unclosed duration=100ms code=400", "level": nil, "method": nil, "status": nil, types.ColumnNameParsedError: types.LogfmtParserErrorType, types.ColumnNameParsedErrorDetails: "logfmt syntax error at pos 38 : unterminated quoted value"},
				{"message": "level=debug method=POST", "level": "debug", "method": "POST", "status": nil, types.ColumnNameParsedError: nil, types.ColumnNameParsedErrorDetails: nil},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer alloc.AssertSize(t, 0) // Assert empty on test exit

			// Create input data with message column containing logfmt
			input := NewArrowtestPipeline(
				alloc,
				tt.schema,
				tt.input,
			)

			// Create ParseNode requesting "level" field
			parseNode := &physical.ParseNode{
				Kind:          physical.ParserLogfmt,
				RequestedKeys: tt.requestedKeys,
			}

			pipeline := NewParsePipeline(parseNode, input, alloc)

			// Read first record
			ctx := t.Context()
			record, err := pipeline.Read(ctx)
			require.NoError(t, err)
			defer record.Release()

			// Verify the output has the expected number of fields
			outputSchema := record.Schema()
			require.Equal(t, tt.expectedFields, outputSchema.NumFields())

			// Convert record to rows for comparison
			actual, err := arrowtest.RecordRows(record)
			require.NoError(t, err)
			require.Equal(t, tt.expectedOutput, actual)
		})
	}
}
