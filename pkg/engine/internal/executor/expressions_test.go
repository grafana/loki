package executor

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

var (
	fields = []arrow.Field{
		semconv.FieldFromFQN("utf8.builtin.name", false),
		semconv.FieldFromFQN("timestamp_ns.builtin.timestamp", false),
		semconv.FieldFromFQN("float64.builtin.value", false),
		semconv.FieldFromFQN("bool.builtin.valid", false),
	}
	sampledata = `Alice,1745487598764058205,0.2586284611568047,false
Bob,1745487598764058305,0.7823145698741236,true
Charlie,1745487598764058405,0.3451289756123478,false
David,1745487598764058505,0.9217834561278945,true
Eve,1745487598764058605,0.1245789632145789,false
Frank,1745487598764058705,0.5678912345678912,true
Grace,1745487598764058805,0.8912345678912345,false
Hannah,1745487598764058905,0.2345678912345678,true
Ian,1745487598764059005,0.6789123456789123,false
Julia,1745487598764059105,0.4123456789123456,true`
)

func TestEvaluateLiteralExpression(t *testing.T) {
	for _, tt := range []struct {
		name      string
		value     any
		want      any
		arrowType arrow.Type
	}{
		{
			name:      "null",
			value:     nil,
			arrowType: arrow.NULL,
		},
		{
			name:      "bool",
			value:     true,
			arrowType: arrow.BOOL,
		},
		{
			name:      "str",
			value:     "loki",
			arrowType: arrow.STRING,
		},
		{
			name:      "int",
			value:     int64(123456789),
			arrowType: arrow.INT64,
		},
		{
			name:      "float",
			value:     123.456789,
			arrowType: arrow.FLOAT64,
		},
		{
			name:      "timestamp",
			value:     types.Timestamp(3600000000),
			want:      arrow.Timestamp(3600000000),
			arrowType: arrow.TIMESTAMP,
		},
		{
			name:      "duration",
			value:     types.Duration(3600000000),
			want:      int64(3600000000),
			arrowType: arrow.INT64,
		},
		{
			name:      "bytes",
			value:     types.Bytes(1024),
			want:      int64(1024),
			arrowType: arrow.INT64,
		},
		{
			name:      "string list",
			value:     []string{"a", "b", "c"},
			arrowType: arrow.LIST,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			literal := physical.NewLiteral(tt.value)
			e := newExpressionEvaluator()

			n := len(words)
			rec := batch(n, time.Now())
			colVec, err := e.eval(literal, rec)
			require.NoError(t, err)

			require.Equalf(t, tt.arrowType, colVec.DataType().ID(), "expected: %v got: %v", tt.arrowType.String(), colVec.DataType().ID().String())

			for i := range n {
				var val any
				switch arr := colVec.(type) {
				case *array.Null:
					val = arr.Value(i)
				case *array.Boolean:
					val = arr.Value(i)
				case *array.String:
					val = arr.Value(i)
				case *array.Int64:
					val = arr.Value(i)
				case *array.Float64:
					val = arr.Value(i)
				case *array.Timestamp:
					val = arr.Value(i)
				case *array.List:
					val = util.ArrayListValue(arr, i)
				}
				if tt.want != nil {
					require.Equal(t, tt.want, val)
				} else {
					require.Equal(t, tt.value, val)
				}
			}
		})
	}
}

func TestEvaluateColumnExpression(t *testing.T) {
	e := newExpressionEvaluator()

	t.Run("unknown column", func(t *testing.T) {
		colExpr := &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: "does_not_exist",
				Type:   types.ColumnTypeBuiltin,
			},
		}

		n := len(words)
		rec := batch(n, time.Now())
		colVec, err := e.eval(colExpr, rec)
		require.NoError(t, err)

		require.Equal(t, arrow.STRING, colVec.DataType().ID())
	})

	t.Run("string(message)", func(t *testing.T) {
		colExpr := &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: "message",
				Type:   types.ColumnTypeBuiltin,
			},
		}

		n := len(words)
		rec := batch(n, time.Now())
		colVec, err := e.eval(colExpr, rec)
		require.NoError(t, err)
		require.Equal(t, arrow.STRING, colVec.DataType().ID())

		for i := range n {
			val := colVec.(*array.String).Value(i)
			require.Equal(t, words[i%len(words)], val)
		}
	})
}

func TestEvaluateBinaryExpression(t *testing.T) {
	rec, err := CSVToArrow(fields, sampledata)
	require.NoError(t, err)

	e := newExpressionEvaluator()

	t.Run("error if types do not match", func(t *testing.T) {
		expr := &physical.BinaryExpr{
			Left: &physical.ColumnExpr{
				Ref: types.ColumnRef{Column: "name", Type: types.ColumnTypeBuiltin},
			},
			Right: &physical.ColumnExpr{
				Ref: types.ColumnRef{Column: "timestamp", Type: types.ColumnTypeBuiltin},
			},
			Op: types.BinaryOpEq,
		}

		_, err := e.eval(expr, rec)
		require.ErrorContains(t, err, "failed to lookup binary function for signature EQ(utf8,timestamp[ns, tz=UTC]): types do not match")
	})

	t.Run("error if function for signature is not registered", func(t *testing.T) {
		expr := &physical.BinaryExpr{
			Left: &physical.ColumnExpr{
				Ref: types.ColumnRef{Column: "name", Type: types.ColumnTypeBuiltin},
			},
			Right: &physical.ColumnExpr{
				Ref: types.ColumnRef{Column: "name", Type: types.ColumnTypeBuiltin},
			},
			Op: types.BinaryOpXor,
		}

		_, err := e.eval(expr, rec)
		require.ErrorContains(t, err, "failed to lookup binary function for signature XOR(utf8,utf8): not implemented")
	})

	t.Run("EQ(string,string)", func(t *testing.T) {
		expr := &physical.BinaryExpr{
			Left: &physical.ColumnExpr{
				Ref: types.ColumnRef{Column: "name", Type: types.ColumnTypeBuiltin},
			},
			Right: physical.NewLiteral("Charlie"),
			Op:    types.BinaryOpEq,
		}

		res, err := e.eval(expr, rec)
		require.NoError(t, err)
		result := collectBooleanArray(res.(*array.Boolean))
		require.Equal(t, []bool{false, false, true, false, false, false, false, false, false, false}, result)
	})

	t.Run("GT(float,float)", func(t *testing.T) {
		expr := &physical.BinaryExpr{
			Left: &physical.ColumnExpr{
				Ref: types.ColumnRef{Column: "value", Type: types.ColumnTypeBuiltin},
			},
			Right: physical.NewLiteral(0.5),
			Op:    types.BinaryOpGt,
		}

		res, err := e.eval(expr, rec)
		require.NoError(t, err)
		result := collectBooleanArray(res.(*array.Boolean))
		require.Equal(t, []bool{false, true, false, true, false, true, true, false, true, false}, result)
	})
}

func collectBooleanArray(arr *array.Boolean) []bool {
	res := make([]bool, 0, arr.Len())
	for i := range arr.Len() {
		res = append(res, arr.Value(i))
	}
	return res
}

var words = []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}

func batch(n int, now time.Time) arrow.Record {
	// Define the schema
	schema := arrow.NewSchema(
		[]arrow.Field{
			semconv.FieldFromIdent(semconv.ColumnIdentMessage, false),
			semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, false),
		},
		nil, // No metadata
	)

	// Create builders for each column
	logBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	tsBuilder := array.NewTimestampBuilder(memory.DefaultAllocator, &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"})

	// Append data to the builders
	logs := make([]string, n)
	ts := make([]arrow.Timestamp, n)

	for i := range n {
		logs[i] = words[i%len(words)]
		ts[i] = arrow.Timestamp(now.Add(time.Duration(i) * time.Second).UnixNano())
	}

	tsBuilder.AppendValues(ts, nil)
	logBuilder.AppendValues(logs, nil)

	// Build the arrays
	logArray := logBuilder.NewArray()

	tsArray := tsBuilder.NewArray()

	// Create the record
	columns := []arrow.Array{logArray, tsArray}
	record := array.NewRecord(schema, columns, int64(n))

	return record
}

func TestEvaluateAmbiguousColumnExpression(t *testing.T) {
	// Test precedence between generated, metadata, and label columns
	fields := []arrow.Field{
		semconv.FieldFromFQN("utf8.label.test", true),
		semconv.FieldFromFQN("utf8.metadata.test", true),
		semconv.FieldFromFQN("utf8.generated.test", true),
	}

	// CSV data where:
	// Row 0: All columns have values - should pick generated (highest precedence)
	// Row 1: Generated is null, others have values - should pick metadata
	// Row 2: Generated and metadata are null - should pick label
	// Row 3: All are null - should return null
	data := `label_0,metadata_0,generated_0
label_1,metadata_1,null
label_2,null,null
null,null,null`

	record, err := CSVToArrow(fields, data)
	require.NoError(t, err)

	e := newExpressionEvaluator()

	t.Run("ambiguous column should use per-row precedence order", func(t *testing.T) {
		colExpr := &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: "test",
				Type:   types.ColumnTypeAmbiguous,
			},
		}

		colVec, err := e.eval(colExpr, record)
		require.NoError(t, err)
		require.Equal(t, arrow.STRING, colVec.DataType().ID())

		// Test per-row precedence resolution
		col := colVec.(*array.String)
		require.Equal(t, "generated_0", col.Value(0)) // Generated has highest precedence
		require.Equal(t, "metadata_1", col.Value(1))  // Generated is null, metadata has next precedence
		require.Equal(t, "label_2", col.Value(2))     // Generated and metadata are null, label has next precedence
		require.True(t, col.IsNull(3))                // All are null
	})

	t.Run("look-up matching single column should return Array", func(t *testing.T) {
		// Create a record with only one column type
		fields := []arrow.Field{
			semconv.FieldFromFQN("utf8.label.single", false),
		}
		data := `label_0
label_1
label_2
`

		singleRecord, err := CSVToArrow(fields, data)
		require.NoError(t, err)

		colExpr := &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: "single",
				Type:   types.ColumnTypeAmbiguous,
			},
		}

		colVec, err := e.eval(colExpr, singleRecord)
		require.NoError(t, err)
		require.Equal(t, arrow.STRING, colVec.DataType().ID())

		// Test single column behavior
		col := colVec.(*array.String)
		require.Equal(t, "label_0", col.Value(0))
		require.Equal(t, "label_1", col.Value(1))
		require.Equal(t, "label_2", col.Value(2))
	})

	t.Run("ambiguous column with no matching columns should return default scalar", func(t *testing.T) {
		colExpr := &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: "nonexistent",
				Type:   types.ColumnTypeAmbiguous,
			},
		}

		colVec, err := e.eval(colExpr, record)
		require.NoError(t, err)
		require.Equal(t, arrow.STRING, colVec.DataType().ID())
	})
}

func TestEvaluateUnaryCastExpression(t *testing.T) {
	colMsg := semconv.ColumnIdentMessage
	colStatusCode := semconv.NewIdentifier("status_code", types.ColumnTypeMetadata, types.Loki.String)
	colTimeout := semconv.NewIdentifier("timeout", types.ColumnTypeParsed, types.Loki.String)
	colMixedValues := semconv.NewIdentifier("mixed_values", types.ColumnTypeMetadata, types.Loki.String)

	t.Run("unknown column", func(t *testing.T) {
		e := newExpressionEvaluator()
		expr := &physical.UnaryExpr{
			Left: &physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "does_not_exist",
					Type:   types.ColumnTypeAmbiguous,
				},
			},
			Op: types.UnaryOpCastFloat,
		}

		n := len(words)
		rec := batch(n, time.Now())
		colVec, err := e.eval(expr, rec)
		require.NoError(t, err)

		id := colVec.DataType().ID()
		require.Equal(t, arrow.STRUCT, id)

		arr, ok := colVec.(*array.Struct)
		require.True(t, ok)

		require.Equal(t, 3, arr.NumField()) // value, error, error_details
		value, ok := arr.Field(0).(*array.Float64)
		require.True(t, ok)

		errorField, ok := arr.Field(1).(*array.String)
		require.True(t, ok)

		errorDetailsField, ok := arr.Field(2).(*array.String)
		require.True(t, ok)

		require.Equal(t, n, value.Len())
		for i := range n {
			require.Equal(t, 0.0, value.Value(i), "expected value to be 0.0 for row %d", i)
			require.Equal(t, types.SampleExtractionErrorType, errorField.Value(i))
			require.Contains(t, `strconv.ParseFloat: parsing "": invalid syntax`, errorDetailsField.Value(i))
		}
	})

	t.Run("cast column generates a value", func(t *testing.T) {
		expr := &physical.UnaryExpr{
			Left: &physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "status_code",
					Type:   types.ColumnTypeAmbiguous,
				},
			},
			Op: types.UnaryOpCastBytes,
		}

		e := newExpressionEvaluator()

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colMsg, false),
				semconv.FieldFromIdent(colStatusCode, true),
				semconv.FieldFromIdent(colTimeout, true),
			},
			nil,
		)
		rows := arrowtest.Rows{
			{"utf8.builtin.message": "timeout set", "utf8.metadata.status_code": "200", "utf8.parsed.timeout": "2m"},
			{"utf8.builtin.message": "short timeout", "utf8.metadata.status_code": "204", "utf8.parsed.timeout": "10s"},
			{"utf8.builtin.message": "long timeout", "utf8.metadata.status_code": "404", "utf8.parsed.timeout": "1h"},
		}

		record := rows.Record(memory.DefaultAllocator, schema)

		colVec, err := e.eval(expr, record)
		require.NoError(t, err)
		id := colVec.DataType().ID()
		require.Equal(t, arrow.STRUCT, id)

		arr, ok := colVec.(*array.Struct)
		require.True(t, ok)

		require.Equal(t, 1, arr.NumField())
		value, ok := arr.Field(0).(*array.Float64)
		require.True(t, ok)

		require.Equal(t, 3, value.Len())
		require.Equal(t, 200.0, value.Value(0))
		require.Equal(t, 204.0, value.Value(1))
		require.Equal(t, 404.0, value.Value(2))
	})

	t.Run("cast column generates a value from a parsed column", func(t *testing.T) {
		expr := &physical.UnaryExpr{
			Left: &physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "timeout",
					Type:   types.ColumnTypeAmbiguous,
				},
			},
			Op: types.UnaryOpCastDuration,
		}

		e := newExpressionEvaluator()

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colMsg, false),
				semconv.FieldFromIdent(colStatusCode, true),
				semconv.FieldFromIdent(colTimeout, true),
			},
			nil,
		)
		rows := arrowtest.Rows{
			{"utf8.builtin.message": "timeout set", "utf8.metadata.status_code": "200", "utf8.parsed.timeout": "2m"},
			{"utf8.builtin.message": "short timeout", "utf8.metadata.status_code": "204", "utf8.parsed.timeout": "10s"},
			{"utf8.builtin.message": "long timeout", "utf8.metadata.status_code": "404", "utf8.parsed.timeout": "1h"},
		}

		record := rows.Record(memory.DefaultAllocator, schema)

		colVec, err := e.eval(expr, record)
		require.NoError(t, err)
		id := colVec.DataType().ID()
		require.Equal(t, arrow.STRUCT, id)

		arr, ok := colVec.(*array.Struct)
		require.True(t, ok)

		require.Equal(t, 1, arr.NumField())
		value, ok := arr.Field(0).(*array.Float64)
		require.True(t, ok)

		require.Equal(t, 3, value.Len())
		require.Equal(t, 120.0, value.Value(0))
		require.Equal(t, 3600.0, value.Value(2))
	})

	t.Run("cast operation tracks errors", func(t *testing.T) {
		colExpr := &physical.UnaryExpr{
			Left: &physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "mixed_values",
					Type:   types.ColumnTypeAmbiguous,
				},
			},
			Op: types.UnaryOpCastFloat,
		}

		e := newExpressionEvaluator()

		schema := arrow.NewSchema(
			[]arrow.Field{
				semconv.FieldFromIdent(colMsg, false),
				semconv.FieldFromIdent(colMixedValues, true),
			},
			nil,
		)
		rows := arrowtest.Rows{
			{"utf8.builtin.message": "valid numeric", "utf8.metadata.mixed_values": "42.5"},
			{"utf8.builtin.message": "invalid numeric", "utf8.metadata.mixed_values": "not_a_number"},
			{"utf8.builtin.message": "valid bytes", "utf8.metadata.mixed_values": "1KB"},
			{"utf8.builtin.message": "invalid bytes", "utf8.metadata.mixed_values": "invalid_bytes"},
			{"utf8.builtin.message": "empty string", "utf8.metadata.mixed_values": ""},
		}

		record := rows.Record(memory.DefaultAllocator, schema)

		colVec, err := e.eval(colExpr, record)
		require.NoError(t, err)
		id := colVec.DataType().ID()
		require.Equal(t, arrow.STRUCT, id)

		arr, ok := colVec.(*array.Struct)
		require.True(t, ok)

		require.Equal(t, 3, arr.NumField()) // value, error, errorDetails

		// Convert struct to rows for comparison
		actual, err := structToRows(arr)
		require.NoError(t, err)

		// Define expected output
		expected := arrowtest.Rows{
			{
				semconv.ColumnIdentValue.FQN():        42.5,
				semconv.ColumnIdentError.FQN():        nil,
				semconv.ColumnIdentErrorDetails.FQN(): nil,
			},
			{
				semconv.ColumnIdentValue.FQN():        0.0,
				semconv.ColumnIdentError.FQN():        types.SampleExtractionErrorType,
				semconv.ColumnIdentErrorDetails.FQN(): `strconv.ParseFloat: parsing "not_a_number": invalid syntax`,
			},
			{
				semconv.ColumnIdentValue.FQN():        0.0,
				semconv.ColumnIdentError.FQN():        types.SampleExtractionErrorType,
				semconv.ColumnIdentErrorDetails.FQN(): `strconv.ParseFloat: parsing "1KB": invalid syntax`,
			},
			{
				semconv.ColumnIdentValue.FQN():        0.0,
				semconv.ColumnIdentError.FQN():        types.SampleExtractionErrorType,
				semconv.ColumnIdentErrorDetails.FQN(): `strconv.ParseFloat: parsing "invalid_bytes": invalid syntax`,
			},
			{
				semconv.ColumnIdentValue.FQN():        0.0,
				semconv.ColumnIdentError.FQN():        types.SampleExtractionErrorType,
				semconv.ColumnIdentErrorDetails.FQN(): `strconv.ParseFloat: parsing "": invalid syntax`,
			},
		}

		require.Equal(t, expected, actual)
	})
}

func structToRows(structArr *array.Struct) (arrowtest.Rows, error) {
	// Get the struct type to extract schema information
	structType := structArr.DataType().(*arrow.StructType)

	// Create schema from struct fields
	schema := arrow.NewSchema(structType.Fields(), nil)

	// Extract field arrays from the struct
	columns := make([]arrow.Array, structArr.NumField())
	for i := 0; i < structArr.NumField(); i++ {
		columns[i] = structArr.Field(i)
	}

	// Create and return the record
	record := array.NewRecord(schema, columns, int64(structArr.Len()))
	defer record.Release()
	return arrowtest.RecordRows(record)
}

func TestLogfmtParser(t *testing.T) {
	colMsg := "utf8.builtin.message"

	for _, tt := range []struct {
		name           string
		schema         *arrow.Schema
		input          arrowtest.Rows
		requestedKeys  []string
		strict         bool
		keepEmpty      bool
		expectedFields int
		expectedOutput arrowtest.Rows
	}{
		{
			name: "parse stage transforms records, adding columns parsed from message",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=error status=500"},
				{colMsg: "level=info status=200"},
				{colMsg: "level=debug status=201"},
			},
			requestedKeys:  []string{"level", "status"},
			strict:         false,
			keepEmpty:      false,
			expectedFields: 2, // 2 columns: level, status
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":  "error",
					"utf8.parsed.status": "500",
				},
				{
					"utf8.parsed.level":  "info",
					"utf8.parsed.status": "200",
				},
				{
					"utf8.parsed.level":  "debug",
					"utf8.parsed.status": "201",
				},
			},
		},
		{
			name: "handle missing keys with NULL",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=error"},
				{colMsg: "status=200"},
				{colMsg: "level=info"},
			},
			requestedKeys:  []string{"level"},
			strict:         false,
			keepEmpty:      false,
			expectedFields: 1, // 1 column: level
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level": "error",
				},
				{
					"utf8.parsed.level": nil,
				},
				{
					"utf8.parsed.level": "info",
				},
			},
		},
		{
			name: "handle errors with error columns",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=info status=200"},       // No errors
				{colMsg: "status==value level=error"},   // Double equals error on requested key
				{colMsg: "level=\"unclosed status=500"}, // Unclosed quote error
			},
			requestedKeys:  []string{"level", "status"},
			strict:         false,
			keepEmpty:      false,
			expectedFields: 4, // 4 columns: level, status, __error__, __error_details__
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":                   "info",
					"utf8.parsed.status":                  "200",
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
				{
					"utf8.parsed.level":                   nil,
					"utf8.parsed.status":                  nil,
					semconv.ColumnIdentError.FQN():        types.LogfmtParserErrorType,
					semconv.ColumnIdentErrorDetails.FQN(): "logfmt syntax error at pos 8 : unexpected '='",
				},
				{
					"utf8.parsed.level":                   nil,
					"utf8.parsed.status":                  nil,
					semconv.ColumnIdentError.FQN():        types.LogfmtParserErrorType,
					semconv.ColumnIdentErrorDetails.FQN(): "logfmt syntax error at pos 27 : unterminated quoted value",
				},
			},
		},
		{
			name: "extract all keys when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=info status=200 method=GET"},
				{colMsg: "level=warn code=304"},
				{colMsg: "level=error status=500 method=POST duration=123ms"},
			},
			requestedKeys:  nil, // nil means extract all keys
			strict:         false,
			keepEmpty:      false,
			expectedFields: 5, // 5 columns: code, duration, level, method, status
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.code":     nil,
					"utf8.parsed.duration": nil,
					"utf8.parsed.level":    "info",
					"utf8.parsed.method":   "GET",
					"utf8.parsed.status":   "200",
				},
				{
					"utf8.parsed.code":     "304",
					"utf8.parsed.duration": nil,
					"utf8.parsed.level":    "warn",
					"utf8.parsed.method":   nil,
					"utf8.parsed.status":   nil,
				},
				{
					"utf8.parsed.code":     nil,
					"utf8.parsed.duration": "123ms",
					"utf8.parsed.level":    "error",
					"utf8.parsed.method":   "POST",
					"utf8.parsed.status":   "500",
				},
			},
		},
		{
			name: "extract all keys with errors when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=info status=200 method=GET"},       // Valid line
				{colMsg: "level==error code=500"},                  // Double equals error
				{colMsg: "msg=\"unclosed duration=100ms code=400"}, // Unclosed quote error
				{colMsg: "level=debug method=POST"},                // Valid line
			},
			requestedKeys:  nil, // nil means extract all keys
			strict:         false,
			keepEmpty:      false,
			expectedFields: 5, // 5 columns: level, method, status, __error__, __error_details__
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":                   "info",
					"utf8.parsed.method":                  "GET",
					"utf8.parsed.status":                  "200",
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
				{
					"utf8.parsed.level":                   nil,
					"utf8.parsed.method":                  nil,
					"utf8.parsed.status":                  nil,
					semconv.ColumnIdentError.FQN():        types.LogfmtParserErrorType,
					semconv.ColumnIdentErrorDetails.FQN(): "logfmt syntax error at pos 7 : unexpected '='",
				},
				{
					"utf8.parsed.level":                   nil,
					"utf8.parsed.method":                  nil,
					"utf8.parsed.status":                  nil,
					semconv.ColumnIdentError.FQN():        types.LogfmtParserErrorType,
					semconv.ColumnIdentErrorDetails.FQN(): "logfmt syntax error at pos 38 : unterminated quoted value",
				},
				{
					"utf8.parsed.level":                   "debug",
					"utf8.parsed.method":                  "POST",
					"utf8.parsed.status":                  nil,
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
			},
		},
		{
			name: "strict mode stops on first error",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=info status=200 method=GET"}, // Valid line
				{colMsg: "level==error code=500"},            // Double equals error
				{colMsg: "level=debug method=POST"},          // Valid line
			},
			requestedKeys:  nil,
			strict:         true,
			keepEmpty:      false,
			expectedFields: 5, // level, method, status, __error__, __error_details__
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":                "info",
					"utf8.parsed.method":               "GET",
					"utf8.parsed.status":               "200",
					"utf8.generated.__error__":         nil,
					"utf8.generated.__error_details__": nil,
				},
				{
					"utf8.parsed.level":                nil,
					"utf8.parsed.method":               nil,
					"utf8.parsed.status":               nil,
					"utf8.generated.__error__":         "LogfmtParserErr",
					"utf8.generated.__error_details__": "logfmt syntax error at pos 7 : unexpected '='",
				},
				{
					"utf8.parsed.level":                "debug",
					"utf8.parsed.method":               "POST",
					"utf8.parsed.status":               nil,
					"utf8.generated.__error__":         nil,
					"utf8.generated.__error_details__": nil,
				},
			},
		},
		{
			name: "non-strict mode continues parsing on error",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=info status=200 method=GET"}, // Valid line
				{colMsg: "code=500 level= error=true"},       // All fields parsed successfully (no double equals)
				{colMsg: "level=debug method=POST"},          // Valid line
			},
			requestedKeys:  nil,
			strict:         false,
			keepEmpty:      false,
			expectedFields: 5, // code, error, level, method, status  (no errors, so no error columns)
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.code":   nil,
					"utf8.parsed.error":  nil,
					"utf8.parsed.level":  "info",
					"utf8.parsed.method": "GET",
					"utf8.parsed.status": "200",
				},
				{
					"utf8.parsed.code":   "500",
					"utf8.parsed.error":  "true",
					"utf8.parsed.level":  nil,
					"utf8.parsed.method": nil,
					"utf8.parsed.status": nil,
				},
				{
					"utf8.parsed.code":   nil,
					"utf8.parsed.error":  nil,
					"utf8.parsed.level":  "debug",
					"utf8.parsed.method": "POST",
					"utf8.parsed.status": nil,
				},
			},
		},
		{
			name: "keepEmpty mode retains empty values",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=info status= method=GET"},
				{colMsg: "level= status=200"},
			},
			requestedKeys:  nil,
			strict:         false,
			keepEmpty:      true,
			expectedFields: 3, // level, method, status
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":  "info",
					"utf8.parsed.method": "GET",
					"utf8.parsed.status": "",
				},
				{
					"utf8.parsed.level":  "",
					"utf8.parsed.method": nil,
					"utf8.parsed.status": "200",
				},
			},
		},
		{
			name: "without keepEmpty mode skips empty values",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=info status= method=GET"},
				{colMsg: "level= status=200"},
			},
			requestedKeys:  nil,
			strict:         false,
			keepEmpty:      false,
			expectedFields: 3, // level, method, status
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":  "info",
					"utf8.parsed.method": "GET",
					"utf8.parsed.status": nil,
				},
				{
					"utf8.parsed.level":  nil,
					"utf8.parsed.method": nil,
					"utf8.parsed.status": "200",
				},
			},
		},
		{
			name: "strict and keepEmpty both enabled",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level= status=200"},
				{colMsg: "level==error method=POST"},
			},
			requestedKeys:  nil,
			strict:         true,
			keepEmpty:      true,
			expectedFields: 4, // level, status, __error__, __error_details__
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":                "",
					"utf8.parsed.status":               "200",
					"utf8.generated.__error__":         nil,
					"utf8.generated.__error_details__": nil,
				},
				{
					"utf8.parsed.level":                nil,
					"utf8.parsed.status":               nil,
					"utf8.generated.__error__":         "LogfmtParserErr",
					"utf8.generated.__error_details__": "logfmt syntax error at pos 7 : unexpected '='",
				},
			},
		},
		{
			name: "strict mode with requested keys stops on error",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=info status=200 method=GET"},
				{colMsg: "level==error status=500"}, // Error in requested key
				{colMsg: "level=debug status=201"},
			},
			requestedKeys:  []string{"level", "status"},
			strict:         true,
			keepEmpty:      false,
			expectedFields: 4, // level, status, __error__, __error_details__
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":                "info",
					"utf8.parsed.status":               "200",
					"utf8.generated.__error__":         nil,
					"utf8.generated.__error_details__": nil,
				},
				{
					"utf8.parsed.level":                nil,
					"utf8.parsed.status":               nil,
					"utf8.generated.__error__":         "LogfmtParserErr",
					"utf8.generated.__error_details__": "logfmt syntax error at pos 7 : unexpected '='",
				},
				{
					"utf8.parsed.level":                "debug",
					"utf8.parsed.status":               "201",
					"utf8.generated.__error__":         nil,
					"utf8.generated.__error_details__": nil,
				},
			},
		},
		{
			name: "keepEmpty with requested keys retains empty values",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=info status= method=GET"},
				{colMsg: "level= status=200 method=POST"},
				{colMsg: "level=debug status=404"},
			},
			requestedKeys:  []string{"level", "status"},
			strict:         false,
			keepEmpty:      true,
			expectedFields: 2, // level, status (no errors)
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":  "info",
					"utf8.parsed.status": "",
				},
				{
					"utf8.parsed.level":  "",
					"utf8.parsed.status": "200",
				},
				{
					"utf8.parsed.level":  "debug",
					"utf8.parsed.status": "404",
				},
			},
		},
		{
			name: "without keepEmpty and requested keys skips empty values",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=info status= method=GET"},
				{colMsg: "level= status=200 method=POST"},
				{colMsg: "level=debug status=404"},
			},
			requestedKeys:  []string{"level", "status"},
			strict:         false,
			keepEmpty:      false,
			expectedFields: 2, // level, status (no errors)
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":  "info",
					"utf8.parsed.status": nil,
				},
				{
					"utf8.parsed.level":  nil,
					"utf8.parsed.status": "200",
				},
				{
					"utf8.parsed.level":  "debug",
					"utf8.parsed.status": "404",
				},
			},
		},
		{
			name: "strict and keepEmpty with requested keys",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level= status=200 method=GET"},
				{colMsg: "level=info status= method=POST"},
				{colMsg: "level==error status=500"},
			},
			requestedKeys:  []string{"level", "status"},
			strict:         true,
			keepEmpty:      true,
			expectedFields: 4, // level, status, __error__, __error_details__
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":                "",
					"utf8.parsed.status":               "200",
					"utf8.generated.__error__":         nil,
					"utf8.generated.__error_details__": nil,
				},
				{
					"utf8.parsed.level":                "info",
					"utf8.parsed.status":               "",
					"utf8.generated.__error__":         nil,
					"utf8.generated.__error_details__": nil,
				},
				{
					"utf8.parsed.level":                nil,
					"utf8.parsed.status":               nil,
					"utf8.generated.__error__":         "LogfmtParserErr",
					"utf8.generated.__error_details__": "logfmt syntax error at pos 7 : unexpected '='",
				},
			},
		},
		{
			name: "strict mode does not ignore errors in non-requested keys",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=info status=200 bad==value"}, // Error in non-requested key still causes error
				{colMsg: "level=warn status=404"},
			},
			strict:         true,
			keepEmpty:      false,
			requestedKeys:  []string{"level", "status"},
			expectedFields: 4, // level, status, __error__, __error_details__
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":                nil,
					"utf8.parsed.status":               nil,
					"utf8.generated.__error__":         "LogfmtParserErr",
					"utf8.generated.__error_details__": "logfmt syntax error at pos 27 : unexpected '='",
				},
				{
					"utf8.parsed.level":                "warn",
					"utf8.parsed.status":               "404",
					"utf8.generated.__error__":         nil,
					"utf8.generated.__error_details__": nil,
				},
			},
		},
		{
			name: "keepEmpty only affects present keys with empty values",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: "level=info status="},
				{colMsg: "level= method=GET"},
				{colMsg: "status=200"},
			},
			strict:         false,
			keepEmpty:      true,
			requestedKeys:  []string{"level", "status", "method"},
			expectedFields: 3, // level, status, method
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":  "info",
					"utf8.parsed.status": "",
					"utf8.parsed.method": nil,
				},
				{
					"utf8.parsed.level":  "",
					"utf8.parsed.status": nil,
					"utf8.parsed.method": "GET",
				},
				{
					"utf8.parsed.level":  nil,
					"utf8.parsed.status": "200",
					"utf8.parsed.method": nil,
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			expr := &physical.VariadicExpr{
				Op: types.VariadicOpParseLogfmt,
				Expressions: []physical.Expression{
					&physical.ColumnExpr{
						Ref: semconv.ColumnIdentMessage.ColumnRef(),
					},
					&physical.LiteralExpr{
						Literal: types.NewLiteral(tt.requestedKeys),
					},
					&physical.LiteralExpr{
						Literal: types.NewLiteral(tt.strict),
					},
					&physical.LiteralExpr{
						Literal: types.NewLiteral(tt.keepEmpty),
					},
				},
			}
			e := newExpressionEvaluator()

			record := tt.input.Record(memory.DefaultAllocator, tt.schema)
			col, err := e.eval(expr, record)
			require.NoError(t, err)
			id := col.DataType().ID()
			require.Equal(t, arrow.STRUCT, id)

			arr, ok := col.(*array.Struct)
			require.True(t, ok)
			defer arr.Release()

			require.Equal(t, tt.expectedFields, arr.NumField())

			// Convert record to rows for comparison
			actual, err := structToRows(arr)
			require.NoError(t, err)
			require.Equal(t, tt.expectedOutput, actual)
		})
	}
}

func TestEvaluateParseExpression_JSON(t *testing.T) {
	var (
		colTS  = "timestamp_ns.builtin.timestamp"
		colMsg = "utf8.builtin.message"
	)

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
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"level": "error", "status": "500"}`},
				{colMsg: `{"level": "info", "status": "200"}`},
				{colMsg: `{"level": "debug", "status": "201"}`},
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 2, // 2 columns: level, status
			expectedOutput: arrowtest.Rows{
				{"utf8.parsed.level": "error", "utf8.parsed.status": "500"},
				{"utf8.parsed.level": "info", "utf8.parsed.status": "200"},
				{"utf8.parsed.level": "debug", "utf8.parsed.status": "201"},
			},
		},
		{
			name: "parse stage preserves existing columns",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("timestamp_ns.builtin.timestamp", true),
				semconv.FieldFromFQN("utf8.builtin.message", true),
				semconv.FieldFromFQN("utf8.label.app", true),
			}, nil),
			input: arrowtest.Rows{
				{colTS: time.Unix(1, 0).UTC(), colMsg: `{"level": "error", "status": "500"}`, "utf8.label.app": "frontend"},
				{colTS: time.Unix(2, 0).UTC(), colMsg: `{"level": "info", "status": "200"}`, "utf8.label.app": "backend"},
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 2, // level, status
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":  "error",
					"utf8.parsed.status": "500",
				},
				{
					"utf8.parsed.level":  "info",
					"utf8.parsed.status": "200",
				},
			},
		},
		{
			name: "handle missing keys with NULL",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"level": "error"}`},
				{colMsg: `{"status": "200"}`},
				{colMsg: `{"level": "info"}`},
			},
			requestedKeys:  []string{"level"},
			expectedFields: 1, // 1 column: level
			expectedOutput: arrowtest.Rows{
				{"utf8.parsed.level": "error"},
				{"utf8.parsed.level": nil},
				{"utf8.parsed.level": "info"},
			},
		},
		{
			name: "handle errors with error columns",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"level": "info", "status": "200"}`}, // No errors
				{colMsg: `{"level": "error", "status":`},       // Missing closing brace and value
				{colMsg: `{"level": "info", "status": 200}`},   // Number should be converted to string
			},
			requestedKeys:  []string{"level", "status"},
			expectedFields: 4, // 4 columns: level, status, __error__, __error_details__ (due to malformed JSON)
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":                   "info",
					"utf8.parsed.status":                  "200",
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
				{
					"utf8.parsed.level":                   nil,
					"utf8.parsed.status":                  nil,
					semconv.ColumnIdentError.FQN():        "JSONParserErr",
					semconv.ColumnIdentErrorDetails.FQN(): "Malformed JSON error",
				},
				{
					"utf8.parsed.level":                   "info",
					"utf8.parsed.status":                  "200",
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
			},
		},
		{
			name: "extract all keys when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"level": "info", "status": "200", "method": "GET"}`},
				{colMsg: `{"level": "warn", "code": "304"}`},
				{colMsg: `{"level": "error", "status": "500", "method": "POST", "duration": "123ms"}`},
			},
			requestedKeys:  nil, // nil means extract all keys
			expectedFields: 5,   // 5 columns: message, code, duration, level, method, status
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.code":     nil,
					"utf8.parsed.duration": nil,
					"utf8.parsed.level":    "info",
					"utf8.parsed.method":   "GET",
					"utf8.parsed.status":   "200",
				},
				{
					"utf8.parsed.code":     "304",
					"utf8.parsed.duration": nil,
					"utf8.parsed.level":    "warn",
					"utf8.parsed.method":   nil,
					"utf8.parsed.status":   nil,
				},
				{
					"utf8.parsed.code":     nil,
					"utf8.parsed.duration": "123ms",
					"utf8.parsed.level":    "error",
					"utf8.parsed.method":   "POST",
					"utf8.parsed.status":   "500",
				},
			},
		},
		{
			name: "extract all keys with errors when none requested",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"level": "info", "status": "200", "method": "GET"}`}, // Valid line
				{colMsg: `{"level": "error", "code": 500}`},                     // Also valid, adds code column
				{colMsg: `{"msg": "unclosed}`},                                  // Unclosed quote
				{colMsg: `{"level": "debug", "method": "POST"}`},                // Valid line
			},
			requestedKeys:  nil, // nil means extract all keys
			expectedFields: 6,   // 6 columns: level, method, status, code, __error__, __error_details__
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":                   "info",
					"utf8.parsed.method":                  "GET",
					"utf8.parsed.status":                  "200",
					"utf8.parsed.code":                    nil,
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
				{
					"utf8.parsed.level":                   "error",
					"utf8.parsed.method":                  nil,
					"utf8.parsed.status":                  nil,
					"utf8.parsed.code":                    "500",
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
				{
					"utf8.parsed.level":                   nil,
					"utf8.parsed.method":                  nil,
					"utf8.parsed.status":                  nil,
					"utf8.parsed.code":                    nil,
					semconv.ColumnIdentError.FQN():        "JSONParserErr",
					semconv.ColumnIdentErrorDetails.FQN(): "Value is string, but can't find closing '\"' symbol",
				},
				{
					"utf8.parsed.level":                   "debug",
					"utf8.parsed.method":                  "POST",
					"utf8.parsed.status":                  nil,
					"utf8.parsed.code":                    nil,
					semconv.ColumnIdentError.FQN():        nil,
					semconv.ColumnIdentErrorDetails.FQN(): nil,
				},
			},
		},
		{
			name: "handle nested JSON objects with underscore flattening",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"user": {"name": "john", "details": {"age": "30", "city": "NYC"}}, "status": "active"}`},
				{colMsg: `{"app": {"version": "1.0", "config": {"debug": "true"}}, "level": "info"}`},
				{colMsg: `{"nested": {"deep": {"very": {"deep": "value"}}}}`},
			},
			requestedKeys:  nil, // Extract all keys including nested ones
			expectedFields: 8,   // app_config_debug, app_version, level, nested_deep_very_deep, status, user_details_age, user_details_city, user_name
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.app_config_debug":      nil,
					"utf8.parsed.app_version":           nil,
					"utf8.parsed.level":                 nil,
					"utf8.parsed.nested_deep_very_deep": nil,
					"utf8.parsed.status":                "active",
					"utf8.parsed.user_details_age":      "30",
					"utf8.parsed.user_details_city":     "NYC",
					"utf8.parsed.user_name":             "john",
				},
				{
					"utf8.parsed.app_config_debug":      "true",
					"utf8.parsed.app_version":           "1.0",
					"utf8.parsed.level":                 "info",
					"utf8.parsed.nested_deep_very_deep": nil,
					"utf8.parsed.status":                nil,
					"utf8.parsed.user_details_age":      nil,
					"utf8.parsed.user_details_city":     nil,
					"utf8.parsed.user_name":             nil,
				},
				{
					"utf8.parsed.app_config_debug":      nil,
					"utf8.parsed.app_version":           nil,
					"utf8.parsed.level":                 nil,
					"utf8.parsed.nested_deep_very_deep": "value",
					"utf8.parsed.status":                nil,
					"utf8.parsed.user_details_age":      nil,
					"utf8.parsed.user_details_city":     nil,
					"utf8.parsed.user_name":             nil,
				},
			},
		},
		{
			name: "handle nested JSON with specific requested keys",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"user": {"name": "alice", "profile": {"email": "alice@example.com"}}, "level": "debug"}`},
				{colMsg: `{"user": {"name": "bob"}, "level": "info"}`},
				{colMsg: `{"level": "error", "error": {"code": "500", "message": "internal"}}`},
			},
			requestedKeys:  []string{"user_name", "user_profile_email", "level"},
			expectedFields: 3, // level, user_name, user_profile_email
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.level":              "debug",
					"utf8.parsed.user_name":          "alice",
					"utf8.parsed.user_profile_email": "alice@example.com",
				},
				{
					"utf8.parsed.level":              "info",
					"utf8.parsed.user_name":          "bob",
					"utf8.parsed.user_profile_email": nil,
				},
				{
					"utf8.parsed.level":              "error",
					"utf8.parsed.user_name":          nil,
					"utf8.parsed.user_profile_email": nil,
				},
			},
		},
		{
			name: "accept JSON numbers as strings (v1 compatibility)",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"status": 200, "port": 8080, "timeout": 30.5, "retries": 0}`},
				{colMsg: `{"level": "info", "pid": 12345, "memory": 256.8}`},
				{colMsg: `{"score": -1, "version": 2.1, "enabled": true}`},
			},
			requestedKeys:  []string{"status", "port", "timeout", "pid", "memory", "score", "version"},
			expectedFields: 7, // memory, pid, port, score, status, timeout, version
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.memory":  nil,
					"utf8.parsed.pid":     nil,
					"utf8.parsed.port":    "8080",
					"utf8.parsed.score":   nil,
					"utf8.parsed.status":  "200",
					"utf8.parsed.timeout": "30.5",
					"utf8.parsed.version": nil,
				},
				{
					"utf8.parsed.memory":  "256.8",
					"utf8.parsed.pid":     "12345",
					"utf8.parsed.port":    nil,
					"utf8.parsed.score":   nil,
					"utf8.parsed.status":  nil,
					"utf8.parsed.timeout": nil,
					"utf8.parsed.version": nil,
				},
				{
					"utf8.parsed.memory":  nil,
					"utf8.parsed.pid":     nil,
					"utf8.parsed.port":    nil,
					"utf8.parsed.score":   "-1",
					"utf8.parsed.status":  nil,
					"utf8.parsed.timeout": nil,
					"utf8.parsed.version": "2.1",
				},
			},
		},
		{
			name: "mixed nested objects and numbers",
			schema: arrow.NewSchema([]arrow.Field{
				semconv.FieldFromFQN("utf8.builtin.message", true),
			}, nil),
			input: arrowtest.Rows{
				{colMsg: `{"request": {"url": "/api/users", "method": "GET"}, "response": {"status": 200, "time": 45.2}}`},
				{colMsg: `{"user": {"id": 123, "profile": {"age": 25}}, "active": true}`},
			},
			requestedKeys:  nil, // Extract all keys
			expectedFields: 7,   // active, request_method, request_url, response_status, response_time, user_id, user_profile_age
			expectedOutput: arrowtest.Rows{
				{
					"utf8.parsed.active":           nil,
					"utf8.parsed.request_method":   "GET",
					"utf8.parsed.request_url":      "/api/users",
					"utf8.parsed.response_status":  "200",
					"utf8.parsed.response_time":    "45.2",
					"utf8.parsed.user_id":          nil,
					"utf8.parsed.user_profile_age": nil,
				},
				{
					"utf8.parsed.active":           "true",
					"utf8.parsed.request_method":   nil,
					"utf8.parsed.request_url":      nil,
					"utf8.parsed.response_status":  nil,
					"utf8.parsed.response_time":    nil,
					"utf8.parsed.user_id":          "123",
					"utf8.parsed.user_profile_age": "25",
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			alloc := memory.DefaultAllocator

			expr := &physical.VariadicExpr{
				Op: types.VariadicOpParseJSON,
				Expressions: []physical.Expression{
					&physical.ColumnExpr{
						Ref: semconv.ColumnIdentMessage.ColumnRef(),
					},
					&physical.LiteralExpr{
						Literal: types.NewLiteral(tt.requestedKeys),
					},
					&physical.LiteralExpr{
						Literal: types.NewLiteral(false),
					},
					&physical.LiteralExpr{
						Literal: types.NewLiteral(false),
					},
				},
			}
			e := newExpressionEvaluator()

			record := tt.input.Record(alloc, tt.schema)
			col, err := e.eval(expr, record)
			require.NoError(t, err)
			id := col.DataType().ID()
			require.Equal(t, arrow.STRUCT, id)

			arr, ok := col.(*array.Struct)
			require.True(t, ok)

			require.Equal(t, tt.expectedFields, arr.NumField()) // value, error, errorDetails

			// Convert record to rows for comparison
			actual, err := structToRows(arr)
			require.NoError(t, err)
			require.Equal(t, tt.expectedOutput, actual)
		})
	}
}
