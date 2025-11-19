package executor

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestNewProjectPipeline(t *testing.T) {
	fields := []arrow.Field{
		semconv.FieldFromFQN("utf8.builtin.name", false),
		semconv.FieldFromFQN("int64.builtin.age", false),
		semconv.FieldFromFQN("utf8.builtin.city", false),
	}

	t.Run("project single column", func(t *testing.T) {
		// Create input data
		inputCSV := "Alice,30,New York\nBob,25,Boston\nCharlie,35,Seattle"
		inputRecord, err := CSVToArrow(fields, inputCSV)
		require.NoError(t, err)

		// Create input pipeline
		inputPipeline := NewBufferedPipeline(inputRecord)

		// Create projection columns (just the "name" column)
		columns := []physical.Expression{
			&physical.ColumnExpr{
				Ref: createColumnRef("name"),
			},
		}

		// Create project pipeline
		e := newExpressionEvaluator()
		projectPipeline, err := NewProjectPipeline(inputPipeline, &physical.Projection{Expressions: columns}, &e, nil)
		require.NoError(t, err)

		// Create expected output
		expectedCSV := "Alice\nBob\nCharlie"
		expectedFields := []arrow.Field{
			semconv.FieldFromFQN("utf8.builtin.name", false),
		}
		expectedRecord, err := CSVToArrow(expectedFields, expectedCSV)
		require.NoError(t, err)

		expectedPipeline := NewBufferedPipeline(expectedRecord)

		// Assert that the pipelines produce equal results
		AssertPipelinesEqual(t, projectPipeline, expectedPipeline)
	})

	t.Run("project multiple columns", func(t *testing.T) {
		// Create input data
		inputCSV := "Alice,30,New York\nBob,25,Boston\nCharlie,35,Seattle"
		inputRecord, err := CSVToArrow(fields, inputCSV)
		require.NoError(t, err)

		// Create input pipeline
		inputPipeline := NewBufferedPipeline(inputRecord)

		// Create projection columns (both "name" and "city" columns)
		columns := []physical.Expression{
			&physical.ColumnExpr{
				Ref: createColumnRef("name"),
			},
			&physical.ColumnExpr{
				Ref: createColumnRef("city"),
			},
		}

		// Create project pipeline
		e := newExpressionEvaluator()
		projectPipeline, err := NewProjectPipeline(inputPipeline, &physical.Projection{Expressions: columns}, &e, nil)
		require.NoError(t, err)

		// Create expected output
		expectedCSV := "Alice,New York\nBob,Boston\nCharlie,Seattle"
		expectedFields := []arrow.Field{
			semconv.FieldFromFQN("utf8.builtin.name", false),
			semconv.FieldFromFQN("utf8.builtin.city", false),
		}
		expectedRecord, err := CSVToArrow(expectedFields, expectedCSV)
		require.NoError(t, err)

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

		inputRecord2, err := CSVToArrow(fields, inputCSV2)
		require.NoError(t, err)

		// Create input pipeline with multiple batches
		inputPipeline := NewBufferedPipeline(inputRecord1, inputRecord2)

		// Create projection columns
		columns := []physical.Expression{
			&physical.ColumnExpr{
				Ref: createColumnRef("name"),
			},
			&physical.ColumnExpr{
				Ref: createColumnRef("age"),
			},
		}

		// Create project pipeline
		e := newExpressionEvaluator()
		projectPipeline, err := NewProjectPipeline(inputPipeline, &physical.Projection{Expressions: columns}, &e, nil)
		require.NoError(t, err)

		// Create expected output also split across multiple records
		expectedFields := []arrow.Field{
			semconv.FieldFromFQN("utf8.builtin.name", false),
			semconv.FieldFromFQN("int64.builtin.age", false),
		}

		expected := `
Alice,30
Bob,25
Charlie,35
Dave,40
		`

		expectedRecord, err := CSVToArrow(expectedFields, expected)
		require.NoError(t, err)

		expectedPipeline := NewBufferedPipeline(expectedRecord)

		// Assert that the pipelines produce equal results
		AssertPipelinesEqual(t, projectPipeline, expectedPipeline)
	})

	t.Run("drop", func(t *testing.T) {
		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.service", false),
				semconv.FieldFromFQN("int64.metadata.count", false),
				semconv.FieldFromFQN("int64.parsed.count", false),
			}, nil)

		rows := arrowtest.Rows{
			{"utf8.builtin.service": "loki", "int64.metadata.count": 1, "int64.parsed.count": 1},
			{"utf8.builtin.service": "loki", "int64.metadata.count": 2, "int64.parsed.count": 4},
			{"utf8.builtin.service": "loki", "int64.metadata.count": 3, "int64.parsed.count": 9},
		}

		for _, tc := range []struct {
			name           string
			columns        []physical.Expression
			expectedFields []arrow.Field
		}{
			{
				name: "single column",
				columns: []physical.Expression{
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeAmbiguous}},
				},
				expectedFields: []arrow.Field{
					semconv.FieldFromFQN("int64.metadata.count", false),
					semconv.FieldFromFQN("int64.parsed.count", false),
				},
			},
			{
				name: "single ambiguous column",
				columns: []physical.Expression{
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "count", Type: types.ColumnTypeAmbiguous}},
				},
				expectedFields: []arrow.Field{
					semconv.FieldFromFQN("utf8.builtin.service", false),
				},
			},
			{
				name: "single non-ambiguous column",
				columns: []physical.Expression{
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "count", Type: types.ColumnTypeMetadata}},
				},
				expectedFields: []arrow.Field{
					semconv.FieldFromFQN("utf8.builtin.service", false),
					semconv.FieldFromFQN("int64.parsed.count", false),
				},
			},
			{
				name: "multiple columns",
				columns: []physical.Expression{
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeBuiltin}},
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "count", Type: types.ColumnTypeParsed}},
				},
				expectedFields: []arrow.Field{
					semconv.FieldFromFQN("int64.metadata.count", false),
				},
			},
			{
				name: "non existent columns",
				columns: []physical.Expression{
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "__error__", Type: types.ColumnTypeAmbiguous}},
				},
				expectedFields: []arrow.Field{
					semconv.FieldFromFQN("utf8.builtin.service", false),
					semconv.FieldFromFQN("int64.metadata.count", false),
					semconv.FieldFromFQN("int64.parsed.count", false),
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				// Create input data with message column containing logfmt
				input := NewArrowtestPipeline(schema, rows)

				// Create project pipeline
				proj := &physical.Projection{
					Expressions: tc.columns,
					All:         true,
					Drop:        true,
				}
				pipeline, err := NewProjectPipeline(input, proj, &expressionEvaluator{}, nil)
				require.NoError(t, err)

				ctx := t.Context()
				record, err := pipeline.Read(ctx)
				require.NoError(t, err)

				// Verify the output has the expected number of fields
				outputSchema := record.Schema()
				require.Equal(t, len(tc.expectedFields), outputSchema.NumFields())
				require.Equal(t, tc.expectedFields, outputSchema.Fields())
			})
		}
	})
}

func TestNewProjectPipeline_ProjectionFunction_ExpandWithCast(t *testing.T) {
	for _, tt := range []struct {
		name           string
		schema         *arrow.Schema
		input          arrowtest.Rows
		columnExprs    []physical.Expression
		expectedFields int
		expectedOutput arrowtest.Rows
	}{
		{
			name: "cast numeric value from label",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromIdent(semconv.ColumnIdentMessage, false),
				semconv.FieldFromFQN("utf8.metadata.status_code", true),
				semconv.FieldFromFQN("utf8.label.response_time", true),
			}, nil),
			input: arrowtest.Rows{
				{"utf8.builtin.message": "request processed", "utf8.metadata.status_code": "200", "utf8.label.response_time": "150"},
				{"utf8.builtin.message": "slow request", "utf8.metadata.status_code": "200", "utf8.label.response_time": "500"},
				{"utf8.builtin.message": "error occurred", "utf8.metadata.status_code": "500", "utf8.label.response_time": "100"},
			},
			columnExprs: []physical.Expression{
				&physical.UnaryExpr{
					Op:   types.UnaryOpCastFloat,
					Left: &physical.ColumnExpr{Ref: createAmbiguousColumnRef("response_time")},
				},
			},
			expectedFields: 4, // 4 columns: message, status_code, response_time, value
			expectedOutput: arrowtest.Rows{
				{"utf8.builtin.message": "request processed", "utf8.metadata.status_code": "200", "utf8.label.response_time": "150", "float64.generated.value": 150.0},
				{"utf8.builtin.message": "slow request", "utf8.metadata.status_code": "200", "utf8.label.response_time": "500", "float64.generated.value": 500.0},
				{"utf8.builtin.message": "error occurred", "utf8.metadata.status_code": "500", "utf8.label.response_time": "100", "float64.generated.value": 100.0},
			},
		},
		{
			name: "cast bytes value from label",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromIdent(semconv.ColumnIdentMessage, false),
				semconv.FieldFromFQN("utf8.parsed.data_size", true),
			}, nil),
			input: arrowtest.Rows{
				{"utf8.builtin.message": "data uploaded", "utf8.parsed.data_size": "1KiB"},
				{"utf8.builtin.message": "large upload", "utf8.parsed.data_size": "5MiB"},
				{"utf8.builtin.message": "small file", "utf8.parsed.data_size": "512B"},
			},
			columnExprs: []physical.Expression{
				&physical.UnaryExpr{
					Op:   types.UnaryOpCastBytes,
					Left: &physical.ColumnExpr{Ref: createAmbiguousColumnRef("data_size")},
				},
			},
			expectedFields: 3, // 4 columns: message, data_size, value
			expectedOutput: arrowtest.Rows{
				{"utf8.builtin.message": "data uploaded", "utf8.parsed.data_size": "1KiB", "float64.generated.value": 1024.0},
				{"utf8.builtin.message": "large upload", "utf8.parsed.data_size": "5MiB", "float64.generated.value": 5242880.0},
				{"utf8.builtin.message": "small file", "utf8.parsed.data_size": "512B", "float64.generated.value": 512.0},
			},
		},
		{
			name: "cast duration value from parsed field",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromIdent(semconv.ColumnIdentMessage, false),
				semconv.FieldFromFQN("utf8.metadata.status_code", true),
				semconv.FieldFromFQN("utf8.parsed.request_duration", true),
			}, nil),
			input: arrowtest.Rows{
				{"utf8.builtin.message": "request completed", "utf8.metadata.status_code": "200", "utf8.parsed.request_duration": "1.5s"},
				{"utf8.builtin.message": "fast request", "utf8.metadata.status_code": "200", "utf8.parsed.request_duration": "250ms"},
				{"utf8.builtin.message": "slow request", "utf8.metadata.status_code": "500", "utf8.parsed.request_duration": "30s"},
			},
			columnExprs: []physical.Expression{
				&physical.UnaryExpr{
					Op:   types.UnaryOpCastDuration,
					Left: &physical.ColumnExpr{Ref: createAmbiguousColumnRef("request_duration")},
				},
			},
			expectedFields: 4, // 4 columns: message, status_code, request_duration, value
			expectedOutput: arrowtest.Rows{
				{"utf8.builtin.message": "request completed", "utf8.metadata.status_code": "200", "utf8.parsed.request_duration": "1.5s", "float64.generated.value": 1.5},
				{"utf8.builtin.message": "fast request", "utf8.metadata.status_code": "200", "utf8.parsed.request_duration": "250ms", "float64.generated.value": 0.25},
				{"utf8.builtin.message": "slow request", "utf8.metadata.status_code": "500", "utf8.parsed.request_duration": "30s", "float64.generated.value": 30.0},
			},
		},
		{
			name: "cast duration_seconds value from label",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromIdent(semconv.ColumnIdentMessage, false),
				semconv.FieldFromFQN("utf8.metadata.status_code", true),
				semconv.FieldFromFQN("utf8.parsed.timeout", true),
			}, nil),
			input: arrowtest.Rows{
				{"utf8.builtin.message": "timeout set", "utf8.metadata.status_code": "200", "utf8.parsed.timeout": "2m"},
				{"utf8.builtin.message": "short timeout", "utf8.metadata.status_code": "200", "utf8.parsed.timeout": "10s"},
				{"utf8.builtin.message": "long timeout", "utf8.metadata.status_code": "200", "utf8.parsed.timeout": "1h"},
			},
			columnExprs: []physical.Expression{
				&physical.UnaryExpr{
					Op:   types.UnaryOpCastDuration,
					Left: &physical.ColumnExpr{Ref: createAmbiguousColumnRef("timeout")},
				},
			},
			expectedFields: 4, // 4 columns: message, status_code, timeout, value
			expectedOutput: arrowtest.Rows{
				{"utf8.builtin.message": "timeout set", "utf8.metadata.status_code": "200", "utf8.parsed.timeout": "2m", "float64.generated.value": 120.0},
				{"utf8.builtin.message": "short timeout", "utf8.metadata.status_code": "200", "utf8.parsed.timeout": "10s", "float64.generated.value": 10.0},
				{"utf8.builtin.message": "long timeout", "utf8.metadata.status_code": "200", "utf8.parsed.timeout": "1h", "float64.generated.value": 3600.0},
			},
		},
		{
			name: "mixed valid and invalid values with null handling",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromIdent(semconv.ColumnIdentMessage, false),
				semconv.FieldFromFQN("utf8.parsed.mixed_values", true),
			}, nil),
			input: arrowtest.Rows{
				{"utf8.builtin.message": "valid numeric", "utf8.parsed.mixed_values": "42.5"},
				{"utf8.builtin.message": "invalid numeric", "utf8.parsed.mixed_values": "not_a_number"},
				{"utf8.builtin.message": "valid bytes", "utf8.parsed.mixed_values": "1KB"},
				{"utf8.builtin.message": "invalid bytes", "utf8.parsed.mixed_values": "invalid_bytes"},
				{"utf8.builtin.message": "empty string", "utf8.parsed.mixed_values": ""},
			},
			columnExprs: []physical.Expression{
				&physical.UnaryExpr{
					Op:   types.UnaryOpCastFloat,
					Left: &physical.ColumnExpr{Ref: createAmbiguousColumnRef("mixed_values")},
				},
			},
			expectedFields: 5,
			expectedOutput: arrowtest.Rows{
				{"utf8.builtin.message": "valid numeric", "utf8.parsed.mixed_values": "42.5",
					"float64.generated.value":          42.5,
					"utf8.generated.__error__":         nil,
					"utf8.generated.__error_details__": nil},
				{"utf8.builtin.message": "invalid numeric", "utf8.parsed.mixed_values": "not_a_number",
					"float64.generated.value":          0.0,
					"utf8.generated.__error__":         types.SampleExtractionErrorType,
					"utf8.generated.__error_details__": `strconv.ParseFloat: parsing "not_a_number": invalid syntax`}, //invalid
				{"utf8.builtin.message": "valid bytes", "utf8.parsed.mixed_values": "1KB",
					"float64.generated.value":          0.0,
					"utf8.generated.__error__":         types.SampleExtractionErrorType,
					"utf8.generated.__error_details__": `strconv.ParseFloat: parsing "1KB": invalid syntax`}, // 1KB is not a valid float but doesn't error
				{"utf8.builtin.message": "invalid bytes", "utf8.parsed.mixed_values": "invalid_bytes",
					"float64.generated.value":          0.0,
					"utf8.generated.__error__":         types.SampleExtractionErrorType,
					"utf8.generated.__error_details__": `strconv.ParseFloat: parsing "invalid_bytes": invalid syntax`}, // invalid but doesn't error
				{"utf8.builtin.message": "empty string", "utf8.parsed.mixed_values": "",
					"float64.generated.value":          0.0,
					"utf8.generated.__error__":         types.SampleExtractionErrorType,
					"utf8.generated.__error_details__": `strconv.ParseFloat: parsing "": invalid syntax`}, // empty string gets error from previous parsing
			},
		},
		{
			name: "edge cases for numeric parsing",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromIdent(semconv.ColumnIdentMessage, false),
				semconv.FieldFromFQN("utf8.parsed.edge_values", true),
			}, nil),
			input: arrowtest.Rows{
				{"utf8.builtin.message": "scientific notation", "utf8.parsed.edge_values": "1.23e+02"},
				{"utf8.builtin.message": "negative number", "utf8.parsed.edge_values": "-456.78"},
				{"utf8.builtin.message": "only whitespace", "utf8.parsed.edge_values": "   "},
				{"utf8.builtin.message": "mixed text and numbers", "utf8.parsed.edge_values": "123abc"},
			},
			columnExprs: []physical.Expression{
				&physical.UnaryExpr{
					Op:   types.UnaryOpCastFloat,
					Left: &physical.ColumnExpr{Ref: createAmbiguousColumnRef("edge_values")},
				},
			},
			expectedFields: 5,
			expectedOutput: arrowtest.Rows{
				{"utf8.builtin.message": "scientific notation",
					"utf8.parsed.edge_values":          "1.23e+02",
					"float64.generated.value":          123.0,
					"utf8.generated.__error__":         nil,
					"utf8.generated.__error_details__": nil}, // empty string gets error from previous parsing
				{"utf8.builtin.message": "negative number",
					"utf8.parsed.edge_values":          "-456.78",
					"float64.generated.value":          -456.78,
					"utf8.generated.__error__":         nil,
					"utf8.generated.__error_details__": nil}, // empty string gets error from previous parsing
				{"utf8.builtin.message": "only whitespace", "utf8.parsed.edge_values": "   ",
					"float64.generated.value":          0.0,
					"utf8.generated.__error__":         types.SampleExtractionErrorType,
					"utf8.generated.__error_details__": `strconv.ParseFloat: parsing "   ": invalid syntax`}, // empty string gets error from previous parsing
				{"utf8.builtin.message": "mixed text and numbers",
					"utf8.parsed.edge_values":          "123abc",
					"float64.generated.value":          0.0,
					"utf8.generated.__error__":         types.SampleExtractionErrorType,
					"utf8.generated.__error_details__": `strconv.ParseFloat: parsing "123abc": invalid syntax`}, // empty string gets error from previous parsing
			},
		},
		{
			name: "negative durations and edge cases",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromIdent(semconv.ColumnIdentMessage, false),
				semconv.FieldFromFQN("utf8.parsed.duration_values", true),
			}, nil),
			input: arrowtest.Rows{
				{"utf8.builtin.message": "negative duration", "utf8.parsed.duration_values": "-5s"},
				{"utf8.builtin.message": "zero duration", "utf8.parsed.duration_values": "0s"},
				{"utf8.builtin.message": "fractional duration", "utf8.parsed.duration_values": "1.5s"},
				{"utf8.builtin.message": "invalid duration", "utf8.parsed.duration_values": "5 seconds"}, // space makes it invalid
			},
			columnExprs: []physical.Expression{
				&physical.UnaryExpr{
					Op:   types.UnaryOpCastDuration,
					Left: &physical.ColumnExpr{Ref: createAmbiguousColumnRef("duration_values")},
				},
			},
			expectedFields: 5,
			expectedOutput: arrowtest.Rows{
				{"utf8.builtin.message": "negative duration",
					"utf8.parsed.duration_values":      "-5s",
					"float64.generated.value":          -5.0,
					"utf8.generated.__error__":         nil,
					"utf8.generated.__error_details__": nil},
				{"utf8.builtin.message": "zero duration",
					"utf8.parsed.duration_values":      "0s",
					"float64.generated.value":          0.0,
					"utf8.generated.__error__":         nil,
					"utf8.generated.__error_details__": nil},
				{"utf8.builtin.message": "fractional duration",
					"utf8.parsed.duration_values":      "1.5s",
					"float64.generated.value":          1.5,
					"utf8.generated.__error__":         nil,
					"utf8.generated.__error_details__": nil},
				{"utf8.builtin.message": "invalid duration",
					"utf8.parsed.duration_values":      "5 seconds",
					"float64.generated.value":          0.0,
					"utf8.generated.__error__":         types.SampleExtractionErrorType,
					"utf8.generated.__error_details__": `time: unknown unit " seconds" in duration "5 seconds"`}, // empty string gets error from previous parsing
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Create input data
			input := NewArrowtestPipeline(
				tt.schema,
				tt.input,
			)

			e := newExpressionEvaluator()
			pipeline, err := NewProjectPipeline(
				input,
				&physical.Projection{
					Expressions: tt.columnExprs,
					Expand:      true,
					All:         true,
				},
				&e,
				nil)
			require.NoError(t, err)
			defer pipeline.Close()

			// Read first record
			ctx := t.Context()
			record, err := pipeline.Read(ctx)
			require.NoError(t, err)

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

func TestNewProjectPipeline_ProjectionFunction_ExpandWithBinOn(t *testing.T) {
	t.Run("calculates a simple expression with 1 input", func(t *testing.T) {
		colTs := "timestamp_ns.builtin.timestamp"
		colVal := "float64.generated.value"
		colEnv := "utf8.label.env"
		colSvc := "utf8.label.service"

		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN(colTs, false),
			semconv.FieldFromFQN(colVal, false),
			semconv.FieldFromFQN(colEnv, false),
			semconv.FieldFromFQN(colSvc, false),
		}, nil)

		rowsPipeline1 := []arrowtest.Rows{
			{
				{colTs: time.Unix(20, 0).UTC(), colVal: float64(230), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(15, 0).UTC(), colVal: float64(120), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(10, 0).UTC(), colVal: float64(260), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(12, 0).UTC(), colVal: float64(250), colEnv: "dev", colSvc: "distributor"},
			},
		}
		input1 := NewArrowtestPipeline(schema, rowsPipeline1...)

		// value / 10
		projection := &physical.Projection{
			Expressions: []physical.Expression{
				&physical.BinaryExpr{
					Left: &physical.ColumnExpr{
						Ref: types.ColumnRef{
							Column: types.ColumnNameGeneratedValue,
							Type:   types.ColumnTypeGenerated,
						},
					},
					Right: physical.NewLiteral(float64(10)),
					Op:    types.BinaryOpDiv,
				},
			},
			All:    true,
			Expand: true,
		}

		pipeline, err := NewProjectPipeline(input1, projection, &expressionEvaluator{}, nil)
		require.NoError(t, err)
		defer pipeline.Close()

		// Read the pipeline output
		record, err := pipeline.Read(t.Context())
		require.NoError(t, err)

		expect := arrowtest.Rows{
			{colTs: time.Unix(20, 0).UTC(), colVal: float64(23), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(15, 0).UTC(), colVal: float64(12), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(10, 0).UTC(), colVal: float64(26), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(12, 0).UTC(), colVal: float64(25), colEnv: "dev", colSvc: "distributor"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("calculates a complex expression with 1 input", func(t *testing.T) {
		colTs := "timestamp_ns.builtin.timestamp"
		colVal := "float64.generated.value"
		colEnv := "utf8.label.env"
		colSvc := "utf8.label.service"

		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN(colTs, false),
			semconv.FieldFromFQN(colVal, false),
			semconv.FieldFromFQN(colEnv, false),
			semconv.FieldFromFQN(colSvc, false),
		}, nil)

		rowsPipeline1 := []arrowtest.Rows{
			{
				{colTs: time.Unix(20, 0).UTC(), colVal: float64(230), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(15, 0).UTC(), colVal: float64(120), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(10, 0).UTC(), colVal: float64(260), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(12, 0).UTC(), colVal: float64(250), colEnv: "dev", colSvc: "distributor"},
			},
		}
		input1 := NewArrowtestPipeline(schema, rowsPipeline1...)

		// value * 10 + 100 / 10
		projection := &physical.Projection{
			Expressions: []physical.Expression{
				&physical.BinaryExpr{
					Left: &physical.BinaryExpr{
						Left: &physical.ColumnExpr{
							Ref: types.ColumnRef{
								Column: types.ColumnNameGeneratedValue,
								Type:   types.ColumnTypeGenerated,
							},
						},
						Right: physical.NewLiteral(float64(10)),
						Op:    types.BinaryOpMul,
					},
					Right: &physical.BinaryExpr{
						Left:  physical.NewLiteral(float64(100)),
						Right: physical.NewLiteral(float64(10)),
						Op:    types.BinaryOpDiv,
					},
					Op: types.BinaryOpAdd,
				},
			},
			All:    true,
			Expand: true,
		}

		pipeline, err := NewProjectPipeline(input1, projection, &expressionEvaluator{}, nil)
		require.NoError(t, err)
		defer pipeline.Close()

		// Read the pipeline output
		record, err := pipeline.Read(t.Context())
		require.NoError(t, err)

		expect := arrowtest.Rows{
			{colTs: time.Unix(20, 0).UTC(), colVal: float64(2310), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(15, 0).UTC(), colVal: float64(1210), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(10, 0).UTC(), colVal: float64(2610), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(12, 0).UTC(), colVal: float64(2510), colEnv: "dev", colSvc: "distributor"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("calculates a complex ex", func(t *testing.T) {
		colTs := "timestamp_ns.builtin.timestamp"
		colVal := "float64.generated.value"
		colValLeft := "float64.generated.value_left"
		colValRight := "float64.generated.value_right"
		colEnv := "utf8.label.env"
		colSvc := "utf8.label.service"

		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN(colTs, false),
			semconv.FieldFromFQN(colValLeft, false),
			semconv.FieldFromFQN(colValRight, false),
			semconv.FieldFromFQN(colEnv, false),
			semconv.FieldFromFQN(colSvc, false),
		}, nil)

		rowsPipeline1 := []arrowtest.Rows{
			{
				{colTs: time.Unix(20, 0).UTC(), colValLeft: float64(230), colValRight: float64(2), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(15, 0).UTC(), colValLeft: float64(120), colValRight: float64(10), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(10, 0).UTC(), colValLeft: float64(260), colValRight: float64(4), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(12, 0).UTC(), colValLeft: float64(250), colValRight: float64(20), colEnv: "dev", colSvc: "distributor"},
			},
		}
		input1 := NewArrowtestPipeline(schema, rowsPipeline1...)

		// value_left / value_right
		projection := &physical.Projection{
			Expressions: []physical.Expression{
				&physical.BinaryExpr{
					Left: &physical.ColumnExpr{
						Ref: types.ColumnRef{
							Column: "value_left",
							Type:   types.ColumnTypeGenerated,
						},
					},
					Right: &physical.ColumnExpr{
						Ref: types.ColumnRef{
							Column: "value_right",
							Type:   types.ColumnTypeGenerated,
						},
					},
					Op: types.BinaryOpDiv,
				},
			},
			All:    true,
			Expand: true,
		}

		pipeline, err := NewProjectPipeline(input1, projection, &expressionEvaluator{}, nil)
		require.NoError(t, err)
		defer pipeline.Close()

		// Read the pipeline output
		record, err := pipeline.Read(t.Context())
		require.NoError(t, err)

		expect := arrowtest.Rows{
			{colTs: time.Unix(20, 0).UTC(), colValLeft: float64(230), colValRight: float64(2), colVal: float64(115), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(15, 0).UTC(), colValLeft: float64(120), colValRight: float64(10), colVal: float64(12), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(10, 0).UTC(), colValLeft: float64(260), colValRight: float64(4), colVal: float64(65), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(12, 0).UTC(), colValLeft: float64(250), colValRight: float64(20), colVal: float64(12.5), colEnv: "dev", colSvc: "distributor"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})
}

// Helper to create a column reference
func createColumnRef(name string) types.ColumnRef {
	return types.ColumnRef{
		Column: name,
		Type:   types.ColumnTypeBuiltin,
	}
}

// Helper to create a column reference
func createAmbiguousColumnRef(name string) types.ColumnRef {
	return types.ColumnRef{
		Column: name,
		Type:   types.ColumnTypeAmbiguous,
	}
}
