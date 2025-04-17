package executor

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/errors"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
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
			value:     float64(123.456789),
			arrowType: arrow.FLOAT64,
		},
		{
			name:      "timestamp",
			value:     uint64(1744612881740032450),
			arrowType: arrow.UINT64,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			literal := physical.NewLiteral(tt.value)
			e := expressionEvaluator{}

			n := len(words)
			rec := batch(n, time.Now())
			colVec, err := e.eval(literal, rec)
			require.NoError(t, err)
			require.Equalf(t, tt.arrowType, colVec.Type().ID(), "expected: %v got: %v", tt.arrowType.String(), colVec.Type().ID().String())

			for i := range n {
				val := colVec.Value(int64(i))
				require.Equal(t, tt.value, val)
			}
		})
	}
}

func TestEvaluateColumnExpression(t *testing.T) {
	e := expressionEvaluator{}

	t.Run("invalid", func(t *testing.T) {
		colExpr := &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: "does_not_exist",
				Type:   types.ColumnTypeBuiltin,
			},
		}

		n := len(words)
		rec := batch(n, time.Now())
		_, err := e.eval(colExpr, rec)
		require.ErrorContains(t, err, errors.ErrKey.Error())
	})

	t.Run("string(log)", func(t *testing.T) {
		colExpr := &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: "log",
				Type:   types.ColumnTypeBuiltin,
			},
		}

		n := len(words)
		rec := batch(n, time.Now())
		colVec, err := e.eval(colExpr, rec)
		require.NoError(t, err)
		require.Equal(t, arrow.STRING, colVec.Type().ID())

		for i := range n {
			val := colVec.Value(int64(i))
			require.Equal(t, words[i%len(words)], val)
		}
	})
}

var words = []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}

func batch(n int, now time.Time) arrow.Record {
	// 1. Create a memory allocator
	mem := memory.NewGoAllocator()

	// 2. Define the schema
	schema := arrow.NewSchema(
		[]arrow.Field{
			{Name: "log", Type: arrow.BinaryTypes.String},
			{Name: "timestamp", Type: arrow.PrimitiveTypes.Uint64},
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
