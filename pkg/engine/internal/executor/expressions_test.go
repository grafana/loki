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

		require.Equal(t, 3, arr.NumField()) //value, error, errorDetails

		// Find the value, error, and errorDetails fields using schema
		var valueField *array.Float64
		var errorField, errorDetailsField *array.String
		valueField, ok = arr.Field(0).(*array.Float64)
		require.True(t, ok)
		errorField, ok = arr.Field(1).(*array.String)
		require.True(t, ok)
		errorDetailsField, ok = arr.Field(2).(*array.String)
		require.True(t, ok)

		require.NotNil(t, valueField, "expected to find a Float64 value field in struct")
		require.NotNil(t, errorField, "expected to find error field in struct")
		require.NotNil(t, errorDetailsField, "expected to find errorDetails field in struct")

		// Verify values: first row is valid (42.5), rest are invalid (0.0 with errors)
		require.Equal(t, 42.5, valueField.Value(0))
		require.False(t, valueField.IsNull(0))
		require.True(t, errorField.IsNull(0))

		// Row 1: invalid - "not_a_number"
		require.Equal(t, 0.0, valueField.Value(1))
		require.False(t, valueField.IsNull(1)) // Verify value is non-null 0.0
		require.False(t, errorField.IsNull(1))
		require.Equal(t, types.SampleExtractionErrorType, errorField.Value(1))
		require.Contains(t, errorDetailsField.Value(1), `strconv.ParseFloat: parsing "not_a_number": invalid syntax`)

		// Row 2: invalid - "1KB"
		require.Equal(t, 0.0, valueField.Value(2))
		require.False(t, valueField.IsNull(2)) // Verify value is non-null 0.0
		require.False(t, errorField.IsNull(2))
		require.Equal(t, types.SampleExtractionErrorType, errorField.Value(2))
		require.Contains(t, errorDetailsField.Value(2), `strconv.ParseFloat: parsing "1KB": invalid syntax`)

		// Row 3: invalid - "invalid_bytes"
		require.Equal(t, 0.0, valueField.Value(3))
		require.False(t, valueField.IsNull(3)) // Verify value is non-null 0.0
		require.False(t, errorField.IsNull(3))
		require.Equal(t, types.SampleExtractionErrorType, errorField.Value(3))
		require.Contains(t, errorDetailsField.Value(3), `strconv.ParseFloat: parsing "invalid_bytes": invalid syntax`)

		// Row 4: invalid - empty string
		require.Equal(t, 0.0, valueField.Value(4))
		require.False(t, valueField.IsNull(4)) // Verify value is non-null 0.0
		require.False(t, errorField.IsNull(4))
		require.Equal(t, types.SampleExtractionErrorType, errorField.Value(4))
		require.Contains(t, errorDetailsField.Value(4), `strconv.ParseFloat: parsing "": invalid syntax`)
	})
}
