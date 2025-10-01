package executor

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestNewProjectPipeline(t *testing.T) {
	fields := []arrow.Field{
		{Name: "name", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
		{Name: "age", Type: datatype.Arrow.Integer, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.Integer)},
		{Name: "city", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
	}

	t.Run("project single column", func(t *testing.T) {
		// Create input data
		inputCSV := "Alice,30,New York\nBob,25,Boston\nCharlie,35,Seattle"
		inputRecord, err := CSVToArrow(fields, inputCSV)
		require.NoError(t, err)
		defer inputRecord.Release()

		// Create input pipeline
		inputPipeline := NewBufferedPipeline(inputRecord)

		// Create projection columns (just the "name" column)
		projection := &physical.Projection{
			Columns: []physical.ColumnExpression{
				&physical.ColumnExpr{
					Ref: createColumnRef("name"),
				},
			},
			Functions: []physical.ProjectionFunction{},
		}

		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0) // Assert empty on test exit
		// Create project pipeline
		projectPipeline, err := NewProjectPipeline(inputPipeline, projection, &expressionEvaluator{}, alloc)
		require.NoError(t, err)

		// Create expected output
		expectedCSV := "Alice\nBob\nCharlie"
		expectedFields := []arrow.Field{
			{Name: "name", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
		}
		expectedRecord, err := CSVToArrow(expectedFields, expectedCSV)
		require.NoError(t, err)
		defer expectedRecord.Release()

		expectedPipeline := NewBufferedPipeline(expectedRecord)

		// Assert that the pipelines produce equal results
		AssertPipelinesEqual(t, projectPipeline, expectedPipeline)
	})

	t.Run("project multiple columns", func(t *testing.T) {
		// Create input data
		inputCSV := "Alice,30,New York\nBob,25,Boston\nCharlie,35,Seattle"
		inputRecord, err := CSVToArrow(fields, inputCSV)
		require.NoError(t, err)
		defer inputRecord.Release()

		// Create input pipeline
		inputPipeline := NewBufferedPipeline(inputRecord)

		// Create projection columns (both "name" and "city" columns)
		projection := &physical.Projection{
			Columns: []physical.ColumnExpression{
				&physical.ColumnExpr{
					Ref: createColumnRef("name"),
				},
				&physical.ColumnExpr{
					Ref: createColumnRef("city"),
				},
			},
			Functions: []physical.ProjectionFunction{},
		}

		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0) // Assert empty on test exit
		// Create project pipeline
		projectPipeline, err := NewProjectPipeline(inputPipeline, projection, &expressionEvaluator{}, alloc)
		require.NoError(t, err)

		// Create expected output
		expectedCSV := "Alice,New York\nBob,Boston\nCharlie,Seattle"
		expectedFields := []arrow.Field{
			{Name: "name", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
			{Name: "city", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
		}
		expectedRecord, err := CSVToArrow(expectedFields, expectedCSV)
		require.NoError(t, err)
		defer expectedRecord.Release()

		expectedPipeline := NewBufferedPipeline(expectedRecord)

		// Assert that the pipelines produce equal results
		AssertPipelinesEqual(t, projectPipeline, expectedPipeline)
	})

	t.Run("project columns in different order", func(t *testing.T) {
		// Create input data
		inputCSV := "Alice,30,New York\nBob,25,Boston\nCharlie,35,Seattle"
		inputRecord, err := CSVToArrow(fields, inputCSV)
		require.NoError(t, err)
		defer inputRecord.Release()

		// Create input pipeline
		inputPipeline := NewBufferedPipeline(inputRecord)

		// Create projection columns (reordering columns)
		projection := &physical.Projection{
			Columns: []physical.ColumnExpression{
				&physical.ColumnExpr{
					Ref: createColumnRef("city"),
				},
				&physical.ColumnExpr{
					Ref: createColumnRef("age"),
				},
				&physical.ColumnExpr{
					Ref: createColumnRef("name"),
				},
			},
			Functions: []physical.ProjectionFunction{},
		}
		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0) // Assert empty on test exit
		// Create project pipeline
		projectPipeline, err := NewProjectPipeline(inputPipeline, projection, &expressionEvaluator{}, alloc)
		require.NoError(t, err)

		// Create expected output
		expectedCSV := "New York,30,Alice\nBoston,25,Bob\nSeattle,35,Charlie"
		expectedFields := []arrow.Field{
			{Name: "city", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
			{Name: "age", Type: datatype.Arrow.Integer, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.Integer)},
			{Name: "name", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
		}
		expectedRecord, err := CSVToArrow(expectedFields, expectedCSV)
		require.NoError(t, err)
		defer expectedRecord.Release()

		expectedPipeline := NewBufferedPipeline(expectedRecord)

		// Assert that the pipelines produce equal results
		AssertPipelinesEqual(t, projectPipeline, expectedPipeline)
	})

	t.Run("project with multiple input batches", func(t *testing.T) {
		// Create input data split across multiple records
		inputCSV1 := "Alice,30,New York\nBob,25,Boston"
		inputCSV2 := "Charlie,35,Seattle\nDave,40,Portland"

		inputRecord1, err := CSVToArrow(fields, inputCSV1)
		require.NoError(t, err)
		defer inputRecord1.Release()

		inputRecord2, err := CSVToArrow(fields, inputCSV2)
		require.NoError(t, err)
		defer inputRecord2.Release()

		// Create input pipeline with multiple batches
		inputPipeline := NewBufferedPipeline(inputRecord1, inputRecord2)

		// Create projection columns
		projection := &physical.Projection{
			Columns: []physical.ColumnExpression{
				&physical.ColumnExpr{
					Ref: createColumnRef("name"),
				},
				&physical.ColumnExpr{
					Ref: createColumnRef("age"),
				},
			},
			Functions: []physical.ProjectionFunction{},
		}

		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0) // Assert empty on test exit

		// Create project pipeline
		projectPipeline, err := NewProjectPipeline(inputPipeline, projection, &expressionEvaluator{}, alloc)
		require.NoError(t, err)

		// Create expected output also split across multiple records
		expectedFields := []arrow.Field{
			{Name: "name", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
			{Name: "age", Type: datatype.Arrow.Integer, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.Integer)},
		}

		expected := `
Alice,30
Bob,25
Charlie,35
Dave,40
		`

		expectedRecord, err := CSVToArrow(expectedFields, expected)
		require.NoError(t, err)
		defer expectedRecord.Release()

		expectedPipeline := NewBufferedPipeline(expectedRecord)

		// Assert that the pipelines produce equal results
		AssertPipelinesEqual(t, projectPipeline, expectedPipeline)
	})

	t.Run("project with no columns selects all", func(t *testing.T) {
		schema := arrow.NewSchema([]arrow.Field{
			{Name: "name", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
			{Name: "age", Type: datatype.Arrow.Integer, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.Integer)},
			{Name: "city", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.String)},
		}, nil)

		inputRows := arrowtest.Rows{
			{"name": "Alice", "age": int64(30), "city": "New York"},
			{"name": "Bob", "age": int64(25), "city": "Boston"},
			{"name": "Charlie", "age": int64(35), "city": "Seattle"},
		}

		// Create input pipeline
		inputPipeline := NewArrowtestPipeline(memory.DefaultAllocator, schema, inputRows)

		// Create projection with empty columns slice
		projection := &physical.Projection{
			Columns:   []physical.ColumnExpression{},
			Functions: []physical.ProjectionFunction{},
		}

		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0) // Assert empty on test exit

		// Create project pipeline
		projectPipeline, err := NewProjectPipeline(inputPipeline, projection, &expressionEvaluator{}, alloc)
		require.NoError(t, err)

		// Read first record from project pipeline
		ctx := context.Background()
		record, err := projectPipeline.Read(ctx)
		require.NoError(t, err)
		defer record.Release()

		// Convert record to rows for comparison
		actualRows, err := arrowtest.RecordRows(record)
		require.NoError(t, err)

		// Expected output should be identical to input (all columns selected)
		expectedRows := arrowtest.Rows{
			{"name": "Alice", "age": int64(30), "city": "New York"},
			{"name": "Bob", "age": int64(25), "city": "Boston"},
			{"name": "Charlie", "age": int64(35), "city": "Seattle"},
		}

		require.Equal(t, expectedRows, actualRows)
	})

}

// Helper to create a column reference
func createColumnRef(name string) types.ColumnRef {
	return types.ColumnRef{
		Column: name,
		Type:   types.ColumnTypeBuiltin,
	}
}

func TestNewProjectPipeline_ProjectionFunction_Unwrap(t *testing.T) {
	for _, tt := range []struct {
		name           string
		schema         *arrow.Schema
		input          arrowtest.Rows
		function       physical.ProjectionFunction
		expectedFields int
		expectedOutput arrowtest.Rows
	}{
		{
			name: "unwrap numeric value from label",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
				{Name: "status_code", Type: arrow.BinaryTypes.String},
				{Name: "response_time", Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "request processed", "status_code": "200", "response_time": "150"},
				{"message": "slow request", "status_code": "200", "response_time": "500"},
				{"message": "error occurred", "status_code": "500", "response_time": "100"},
			},
			function:       physical.NewUnwrapFunction(physical.UnwrapOperationNone, "response_time"),
			expectedFields: 4, // 4 columns: message, status_code, response_time, value
			expectedOutput: arrowtest.Rows{
				{"message": "request processed", "status_code": "200", "response_time": "150", types.ColumnNameGeneratedValue: 150.0},
				{"message": "slow request", "status_code": "200", "response_time": "500", types.ColumnNameGeneratedValue: 500.0},
				{"message": "error occurred", "status_code": "500", "response_time": "100", types.ColumnNameGeneratedValue: 100.0},
			},
		},
		{
			name: "unwrap bytes value from label",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
				{Name: "data_size", Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "data uploaded", "data_size": "1KiB"},
				{"message": "large upload", "data_size": "5MiB"},
				{"message": "small file", "data_size": "512B"},
			},
			function:       physical.NewUnwrapFunction(physical.UnwrapOperationBytes, "data_size"),
			expectedFields: 3, // 4 columns: message, data_size, value
			expectedOutput: arrowtest.Rows{
				{"message": "data uploaded", "data_size": "1KiB", types.ColumnNameGeneratedValue: 1024.0},
				{"message": "large upload", "data_size": "5MiB", types.ColumnNameGeneratedValue: 5242880.0},
				{"message": "small file", "data_size": "512B", types.ColumnNameGeneratedValue: 512.0},
			},
		},
		{
			name: "unwrap duration value from label",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
				{Name: "status_code", Type: arrow.BinaryTypes.String},
				{Name: "request_duration", Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "request completed", "status_code": "200", "request_duration": "1.5s"},
				{"message": "fast request", "status_code": "200", "request_duration": "250ms"},
				{"message": "slow request", "status_code": "500", "request_duration": "30s"},
			},
			function:       physical.NewUnwrapFunction(physical.UnwrapOperationDuration, "request_duration"),
			expectedFields: 4, // 4 columns: message, status_code, request_duration, value
			expectedOutput: arrowtest.Rows{
				{"message": "request completed", "status_code": "200", "request_duration": "1.5s", types.ColumnNameGeneratedValue: 1.5},
				{"message": "fast request", "status_code": "200", "request_duration": "250ms", types.ColumnNameGeneratedValue: 0.25},
				{"message": "slow request", "status_code": "500", "request_duration": "30s", types.ColumnNameGeneratedValue: 30.0},
			},
		},
		{
			name: "unwrap duration_seconds value from label",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
				{Name: "status_code", Type: arrow.BinaryTypes.String},
				{Name: "timeout", Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "timeout set", "status_code": "200", "timeout": "2m"},
				{"message": "short timeout", "status_code": "200", "timeout": "10s"},
				{"message": "long timeout", "status_code": "200", "timeout": "1h"},
			},
			function:       physical.NewUnwrapFunction(physical.UnwrapOperationDurationSeconds, "timeout"),
			expectedFields: 4, // 4 columns: message, status_code, timeout, value
			expectedOutput: arrowtest.Rows{
				{"message": "timeout set", "status_code": "200", "timeout": "2m", types.ColumnNameGeneratedValue: 120.0},
				{"message": "short timeout", "status_code": "200", "timeout": "10s", types.ColumnNameGeneratedValue: 10.0},
				{"message": "long timeout", "status_code": "200", "timeout": "1h", types.ColumnNameGeneratedValue: 3600.0},
			},
		},
		{
			name: "mixed valid and invalid values with null handling",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
				{Name: "mixed_values", Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "valid numeric", "mixed_values": "42.5"},
				{"message": "invalid numeric", "mixed_values": "not_a_number"},
				{"message": "valid bytes", "mixed_values": "1KB"},
				{"message": "invalid bytes", "mixed_values": "invalid_bytes"},
				{"message": "empty string", "mixed_values": ""},
			},
			function:       physical.NewUnwrapFunction(physical.UnwrapOperationNone, "mixed_values"),
			expectedFields: 5,
			expectedOutput: arrowtest.Rows{
				{"message": "valid numeric", "mixed_values": "42.5",
					types.ColumnNameGeneratedValue: 42.5,
					types.ColumnNameError:          nil,
					types.ColumnNameErrorDetails:   nil},
				{"message": "invalid numeric", "mixed_values": "not_a_number",
					types.ColumnNameGeneratedValue: 0.0,
					types.ColumnNameError:          types.SampleExtractionErrorType,
					types.ColumnNameErrorDetails:   `strconv.ParseFloat: parsing "not_a_number": invalid syntax`}, //invalid
				{"message": "valid bytes", "mixed_values": "1KB",
					types.ColumnNameGeneratedValue: 0.0,
					types.ColumnNameError:          types.SampleExtractionErrorType,
					types.ColumnNameErrorDetails:   `strconv.ParseFloat: parsing "1KB": invalid syntax`}, // 1KB is not a valid float but doesn't error
				{"message": "invalid bytes", "mixed_values": "invalid_bytes",
					types.ColumnNameGeneratedValue: 0.0,
					types.ColumnNameError:          types.SampleExtractionErrorType,
					types.ColumnNameErrorDetails:   `strconv.ParseFloat: parsing "invalid_bytes": invalid syntax`}, // invalid but doesn't error
				{"message": "empty string", "mixed_values": "",
					types.ColumnNameGeneratedValue: 0.0,
					types.ColumnNameError:          types.SampleExtractionErrorType,
					types.ColumnNameErrorDetails:   `strconv.ParseFloat: parsing "": invalid syntax`}, // empty string gets error from previous parsing
			},
		},
		{
			name: "edge cases for numeric parsing",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
				{Name: "edge_values", Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "scientific notation", "edge_values": "1.23e+02"},
				{"message": "negative number", "edge_values": "-456.78"},
				{"message": "only whitespace", "edge_values": "   "},
				{"message": "mixed text and numbers", "edge_values": "123abc"},
			},
			function:       physical.NewUnwrapFunction(physical.UnwrapOperationNone, "edge_values"),
			expectedFields: 5,
			expectedOutput: arrowtest.Rows{
				{"message": "scientific notation",
					"edge_values":                  "1.23e+02",
					types.ColumnNameGeneratedValue: 123.0,
					types.ColumnNameError:          nil,
					types.ColumnNameErrorDetails:   nil}, // empty string gets error from previous parsing
				{"message": "negative number",
					"edge_values":                  "-456.78",
					types.ColumnNameGeneratedValue: -456.78,
					types.ColumnNameError:          nil,
					types.ColumnNameErrorDetails:   nil}, // empty string gets error from previous parsing
				{"message": "only whitespace", "edge_values": "   ",
					types.ColumnNameGeneratedValue: 0.0,
					types.ColumnNameError:          types.SampleExtractionErrorType,
					types.ColumnNameErrorDetails:   `strconv.ParseFloat: parsing "   ": invalid syntax`}, // empty string gets error from previous parsing
				{"message": "mixed text and numbers",
					"edge_values":                  "123abc",
					types.ColumnNameGeneratedValue: 0.0,
					types.ColumnNameError:          types.SampleExtractionErrorType,
					types.ColumnNameErrorDetails:   `strconv.ParseFloat: parsing "123abc": invalid syntax`}, // empty string gets error from previous parsing
			},
		},
		{
			name: "negative durations and edge cases",
			schema: arrow.NewSchema([]arrow.Field{
				{Name: types.ColumnNameBuiltinMessage, Type: arrow.BinaryTypes.String},
				{Name: "duration_values", Type: arrow.BinaryTypes.String},
			}, nil),
			input: arrowtest.Rows{
				{"message": "negative duration", "duration_values": "-5s"},
				{"message": "zero duration", "duration_values": "0s"},
				{"message": "fractional duration", "duration_values": "1.5s"},
				{"message": "invalid duration", "duration_values": "5 seconds"}, // space makes it invalid
			},
			function:       physical.NewUnwrapFunction(physical.UnwrapOperationDuration, "duration_values"),
			expectedFields: 5,
			expectedOutput: arrowtest.Rows{
				{"message": "negative duration",
					"duration_values":              "-5s",
					types.ColumnNameGeneratedValue: -5.0,
					types.ColumnNameError:          nil,
					types.ColumnNameErrorDetails:   nil},
				{"message": "zero duration",
					"duration_values":              "0s",
					types.ColumnNameGeneratedValue: 0.0,
					types.ColumnNameError:          nil,
					types.ColumnNameErrorDetails:   nil},
				{"message": "fractional duration",
					"duration_values":              "1.5s",
					types.ColumnNameGeneratedValue: 1.5,
					types.ColumnNameError:          nil,
					types.ColumnNameErrorDetails:   nil},
				{"message": "invalid duration",
					"duration_values":              "5 seconds",
					types.ColumnNameGeneratedValue: 0.0,
					types.ColumnNameError:          types.SampleExtractionErrorType,
					types.ColumnNameErrorDetails:   `time: unknown unit " seconds" in duration "5 seconds"`}, // empty string gets error from previous parsing
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer alloc.AssertSize(t, 0) // Assert empty on test exit

			// Create input data
			input := NewArrowtestPipeline(
				alloc,
				tt.schema,
				tt.input,
			)

			// TODO: we should rename this to ValueProjection
			projection := &physical.Projection{
				Columns: []physical.ColumnExpression{},
				Functions: []physical.ProjectionFunction{
					tt.function,
				},
			}

			pipeline, err := NewProjectPipeline(input, projection, &expressionEvaluator{}, alloc)
			require.NoError(t, err)

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
