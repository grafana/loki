package executor

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

var (
	fields = []arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.String)},
		{Name: "timestamp", Type: arrow.PrimitiveTypes.Int64, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Timestamp)},
		{Name: "value", Type: arrow.PrimitiveTypes.Float64, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Float)},
		{Name: "valid", Type: arrow.FixedWidthTypes.Boolean, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Bool)},
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
			value:     time.Unix(3600, 0).UTC(),
			arrowType: arrow.INT64,
		},
		{
			name:      "duration",
			value:     time.Hour,
			arrowType: arrow.INT64,
		},
		{
			name:      "bytes",
			value:     int64(1024),
			arrowType: arrow.INT64,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			literal := physical.NewLiteral(tt.value)
			e := expressionEvaluator{}

			n := len(words)
			rec := batch(n, time.Now())
			colVec, err := e.eval(literal, rec)
			require.NoError(t, err)
			require.Equalf(t, tt.arrowType, colVec.Type().ArrowType().ID(), "expected: %v got: %v", tt.arrowType.String(), colVec.Type().ArrowType().ID().String())

			for i := range n {
				val := colVec.Value(i)
				require.Equal(t, tt.value, val)
			}
		})
	}
}

func TestEvaluateColumnExpression(t *testing.T) {
	e := expressionEvaluator{}

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

		_, ok := colVec.(*Scalar)
		require.True(t, ok, "expected column vector to be a *Scalar, got %T", colVec)
		require.Equal(t, arrow.STRING, colVec.Type().ArrowType().ID())
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
		require.Equal(t, arrow.STRING, colVec.Type().ArrowType().ID())

		for i := range n {
			val := colVec.Value(i)
			require.Equal(t, words[i%len(words)], val)
		}
	})
}

func TestEvaluateBinaryExpression(t *testing.T) {
	rec, err := CSVToArrow(fields, sampledata)
	require.NoError(t, err)
	defer rec.Release()

	e := expressionEvaluator{}

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
		require.ErrorContains(t, err, "failed to lookup binary function for signature EQ(utf8,int64): types do not match")
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
		result := collectBooleanColumnVector(res)
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
		result := collectBooleanColumnVector(res)
		require.Equal(t, []bool{false, true, false, true, false, true, true, false, true, false}, result)
	})
}

func collectBooleanColumnVector(vec ColumnVector) []bool {
	res := make([]bool, 0, vec.Len())
	arr := vec.ToArray().(*array.Boolean)
	for i := range int(vec.Len()) {
		res = append(res, arr.Value(i))
	}
	return res
}

var words = []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}

func batch(n int, now time.Time) arrow.Record {
	// 1. Create a memory allocator
	mem := memory.NewGoAllocator()

	// 2. Define the schema
	schema := arrow.NewSchema(
		[]arrow.Field{
			{Name: "message", Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadataBuiltinMessage},
			{Name: "timestamp", Type: arrow.PrimitiveTypes.Uint64, Metadata: datatype.ColumnMetadataBuiltinTimestamp},
		},
		nil, // No metadata
	)

	// 3. Create builders for each column
	logBuilder := array.NewStringBuilder(mem)
	defer logBuilder.Release()

	tsBuilder := array.NewUint64Builder(mem)
	defer tsBuilder.Release()

	// 4. Append data to the builders
	logs := make([]string, n)
	ts := make([]uint64, n)

	for i := range n {
		logs[i] = words[i%len(words)]
		ts[i] = uint64(now.Add(time.Duration(i) * time.Second).UnixNano())
	}

	tsBuilder.AppendValues(ts, nil)
	logBuilder.AppendValues(logs, nil)

	// 5. Build the arrays
	logArray := logBuilder.NewArray()
	defer logArray.Release()

	tsArray := tsBuilder.NewArray()
	defer tsArray.Release()

	// 6. Create the record
	columns := []arrow.Array{logArray, tsArray}
	record := array.NewRecord(schema, columns, int64(n))

	return record
}

func TestEvaluateAmbiguousColumnExpression(t *testing.T) {
	e := expressionEvaluator{}

	mem := memory.NewGoAllocator()

	// Create builders for different column types with the same name "test"
	labelBuilder := array.NewStringBuilder(mem)
	defer labelBuilder.Release()
	metadataBuilder := array.NewStringBuilder(mem)
	defer metadataBuilder.Release()
	parsedBuilder := array.NewStringBuilder(mem)
	defer parsedBuilder.Release()
	generatedBuilder := array.NewStringBuilder(mem)
	defer generatedBuilder.Release()

	// Add test data
	labelBuilder.AppendValues([]string{"label_value"}, nil)
	metadataBuilder.AppendValues([]string{"metadata_value"}, nil)
	parsedBuilder.AppendValues([]string{"parsed_value"}, nil)
	generatedBuilder.AppendValues([]string{"generated_value"}, nil)

	// Build arrays
	labelArray := labelBuilder.NewArray()
	defer labelArray.Release()
	metadataArray := metadataBuilder.NewArray()
	defer metadataArray.Release()
	parsedArray := parsedBuilder.NewArray()
	defer parsedArray.Release()
	generatedArray := generatedBuilder.NewArray()
	defer generatedArray.Release()

	// Create schema with multiple columns of the same name but different types
	schema := arrow.NewSchema(
		[]arrow.Field{
			{Name: "test", Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeLabel, datatype.String)},
			{Name: "test", Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeMetadata, datatype.String)},
			{Name: "test", Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeParsed, datatype.String)},
			{Name: "test", Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeGenerated, datatype.String)},
		},
		nil,
	)

	// Create record
	columns := []arrow.Array{labelArray, metadataArray, parsedArray, generatedArray}
	record := array.NewRecord(schema, columns, 1)
	defer record.Release()

	t.Run("ambiguous column should use precedence order", func(t *testing.T) {
		colExpr := &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: "test",
				Type:   types.ColumnTypeAmbiguous,
			},
		}

		colVec, err := e.eval(colExpr, record)
		require.NoError(t, err)
		require.Equal(t, arrow.STRING, colVec.Type().ArrowType().ID())
		require.Equal(t, types.ColumnTypeGenerated, colVec.ColumnType())
		require.Equal(t, "generated_value", colVec.Value(0))
	})

	t.Run("ambiguous column with only some types present", func(t *testing.T) {
		// Create a record with only label and metadata columns
		schemaPartial := arrow.NewSchema(
			[]arrow.Field{
				{Name: "partial", Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeLabel, datatype.String)},
				{Name: "partial", Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeMetadata, datatype.String)},
			},
			nil,
		)

		partialColumns := []arrow.Array{labelArray, metadataArray}
		partialRecord := array.NewRecord(schemaPartial, partialColumns, 1)
		defer partialRecord.Release()

		colExpr := &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: "partial",
				Type:   types.ColumnTypeAmbiguous,
			},
		}

		colVec, err := e.eval(colExpr, partialRecord)
		require.NoError(t, err)
		require.Equal(t, arrow.STRING, colVec.Type().ArrowType().ID())
		require.Equal(t, types.ColumnTypeMetadata, colVec.ColumnType())
		require.Equal(t, "metadata_value", colVec.Value(0))
	})

	t.Run("ambiguous column with single type should work", func(t *testing.T) {
		// Create a record with only one column type
		schemaSingle := arrow.NewSchema(
			[]arrow.Field{
				{Name: "single", Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeLabel, datatype.String)},
			},
			nil,
		)

		singleColumns := []arrow.Array{labelArray}
		singleRecord := array.NewRecord(schemaSingle, singleColumns, 1)
		defer singleRecord.Release()

		colExpr := &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: "single",
				Type:   types.ColumnTypeAmbiguous,
			},
		}

		colVec, err := e.eval(colExpr, singleRecord)
		require.NoError(t, err)
		require.Equal(t, arrow.STRING, colVec.Type().ArrowType().ID())
		require.Equal(t, types.ColumnTypeLabel, colVec.ColumnType())
		require.Equal(t, "label_value", colVec.Value(0))
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
		require.Equal(t, arrow.STRING, colVec.Type().ArrowType().ID())
		require.Equal(t, types.ColumnTypeGenerated, colVec.ColumnType())
		require.Equal(t, "", colVec.Value(0))
	})
}
