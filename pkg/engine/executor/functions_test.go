package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// Helper function to create a boolean array
func createBoolArray(mem memory.Allocator, values []bool, nulls []bool) *Array {
	builder := array.NewBooleanBuilder(mem)
	defer builder.Release()

	for i, val := range values {
		if nulls != nil && i < len(nulls) && nulls[i] {
			builder.AppendNull()
		} else {
			builder.Append(val)
		}
	}

	return &Array{
		array: builder.NewArray(),
		dt:    datatype.Loki.Bool,
		ct:    types.ColumnTypeBuiltin,
		rows:  int64(len(values)),
	}
}

// Helper function to create a string array
func createStringArray(mem memory.Allocator, values []string, nulls []bool) *Array {
	builder := array.NewStringBuilder(mem)
	defer builder.Release()

	for i, val := range values {
		if nulls != nil && i < len(nulls) && nulls[i] {
			builder.AppendNull()
		} else {
			builder.Append(val)
		}
	}

	return &Array{
		array: builder.NewArray(),
		dt:    datatype.Loki.String,
		ct:    types.ColumnTypeBuiltin,
		rows:  int64(len(values)),
	}
}

// Helper function to create an int64 array
func createInt64Array(mem memory.Allocator, values []int64, nulls []bool) *Array {
	builder := array.NewInt64Builder(mem)
	defer builder.Release()

	for i, val := range values {
		if nulls != nil && i < len(nulls) && nulls[i] {
			builder.AppendNull()
		} else {
			builder.Append(val)
		}
	}

	return &Array{
		array: builder.NewArray(),
		dt:    datatype.Loki.Integer,
		ct:    types.ColumnTypeBuiltin,
		rows:  int64(len(values)),
	}
}

// Helper function to create a arrow.Timestamp array
func createTimestampArray(mem memory.Allocator, values []arrow.Timestamp, nulls []bool) *Array {
	builder := array.NewTimestampBuilder(mem, &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"})
	defer builder.Release()

	for i, val := range values {
		if nulls != nil && i < len(nulls) && nulls[i] {
			builder.AppendNull()
		} else {
			builder.Append(val)
		}
	}

	return &Array{
		array: builder.NewArray(),
		dt:    datatype.Loki.Timestamp,
		ct:    types.ColumnTypeBuiltin,
		rows:  int64(len(values)),
	}
}

// Helper function to create a float64 array
func createFloat64Array(mem memory.Allocator, values []float64, nulls []bool) *Array {
	builder := array.NewFloat64Builder(mem)
	defer builder.Release()

	for i, val := range values {
		if nulls != nil && i < len(nulls) && nulls[i] {
			builder.AppendNull()
		} else {
			builder.Append(val)
		}
	}

	return &Array{
		array: builder.NewArray(),
		dt:    datatype.Loki.Float,
		ct:    types.ColumnTypeBuiltin,
		rows:  int64(len(values)),
	}
}

// Helper function to extract boolean values from result
func extractBoolValues(result ColumnVector) []bool {
	arr := result.ToArray().(*array.Boolean)
	values := make([]bool, arr.Len())
	for i := 0; i < arr.Len(); i++ {
		if arr.IsNull(i) {
			values[i] = false
		} else {
			values[i] = arr.Value(i)
		}
	}
	return values
}

func TestBinaryFunctionRegistry_GetForSignature(t *testing.T) {
	tests := []struct {
		name        string
		op          types.BinaryOp
		dataType    arrow.DataType
		expectError bool
	}{
		{
			name:        "valid equality operation for boolean",
			op:          types.BinaryOpEq,
			dataType:    arrow.FixedWidthTypes.Boolean,
			expectError: false,
		},
		{
			name:        "valid equality operation for string",
			op:          types.BinaryOpEq,
			dataType:    arrow.BinaryTypes.String,
			expectError: false,
		},
		{
			name:        "valid equality operation for int64",
			op:          types.BinaryOpEq,
			dataType:    arrow.PrimitiveTypes.Int64,
			expectError: false,
		},
		{
			name:        "valid equality operation for timestamp",
			op:          types.BinaryOpEq,
			dataType:    arrow.FixedWidthTypes.Timestamp_ns,
			expectError: false,
		},
		{
			name:        "valid equality operation for float64",
			op:          types.BinaryOpEq,
			dataType:    arrow.PrimitiveTypes.Float64,
			expectError: false,
		},
		{
			name:        "valid string contains operation",
			op:          types.BinaryOpMatchSubstr,
			dataType:    arrow.BinaryTypes.String,
			expectError: false,
		},
		{
			name:        "valid regex match operation",
			op:          types.BinaryOpMatchRe,
			dataType:    arrow.BinaryTypes.String,
			expectError: false,
		},
		{
			name:        "invalid operation",
			op:          types.BinaryOpAdd, // Not registered
			dataType:    arrow.PrimitiveTypes.Int64,
			expectError: true,
		},
		{
			name:        "invalid data type for operation",
			op:          types.BinaryOpEq,
			dataType:    arrow.PrimitiveTypes.Int32, // Not registered
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn, err := binaryFunctions.GetForSignature(tt.op, tt.dataType)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, fn)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, fn)
			}
		})
	}
}

func TestBooleanComparisonFunctions(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)

	tests := []struct {
		name     string
		op       types.BinaryOp
		lhs      []bool
		rhs      []bool
		expected []bool
	}{
		{
			name:     "boolean equality",
			op:       types.BinaryOpEq,
			lhs:      []bool{true, false, true, false},
			rhs:      []bool{true, false, false, true},
			expected: []bool{true, true, false, false},
		},
		{
			name:     "boolean inequality",
			op:       types.BinaryOpNeq,
			lhs:      []bool{true, false, true, false},
			rhs:      []bool{true, false, false, true},
			expected: []bool{false, false, true, true},
		},
		{
			name:     "boolean greater than",
			op:       types.BinaryOpGt,
			lhs:      []bool{true, false, true, false},
			rhs:      []bool{false, true, true, false},
			expected: []bool{true, false, false, false},
		},
		{
			name:     "boolean greater than or equal",
			op:       types.BinaryOpGte,
			lhs:      []bool{true, false, true, false},
			rhs:      []bool{false, true, true, false},
			expected: []bool{true, false, true, true},
		},
		{
			name:     "boolean less than",
			op:       types.BinaryOpLt,
			lhs:      []bool{true, false, true, false},
			rhs:      []bool{false, true, true, false},
			expected: []bool{false, true, false, false},
		},
		{
			name:     "boolean less than or equal",
			op:       types.BinaryOpLte,
			lhs:      []bool{true, false, true, false},
			rhs:      []bool{false, true, true, false},
			expected: []bool{false, true, true, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lhsArray := createBoolArray(mem, tt.lhs, nil)
			rhsArray := createBoolArray(mem, tt.rhs, nil)
			defer lhsArray.array.Release()
			defer rhsArray.array.Release()

			fn, err := binaryFunctions.GetForSignature(tt.op, arrow.FixedWidthTypes.Boolean)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhsArray, rhsArray)
			require.NoError(t, err)

			actual := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestStringComparisonFunctions(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)

	tests := []struct {
		name     string
		op       types.BinaryOp
		lhs      []string
		rhs      []string
		expected []bool
	}{
		{
			name:     "string equality",
			op:       types.BinaryOpEq,
			lhs:      []string{"hello", "world", "test", ""},
			rhs:      []string{"hello", "world", "different", ""},
			expected: []bool{true, true, false, true},
		},
		{
			name:     "string inequality",
			op:       types.BinaryOpNeq,
			lhs:      []string{"hello", "world", "test", ""},
			rhs:      []string{"hello", "world", "different", ""},
			expected: []bool{false, false, true, false},
		},
		{
			name:     "string greater than",
			op:       types.BinaryOpGt,
			lhs:      []string{"b", "a", "z", "hello"},
			rhs:      []string{"a", "b", "a", "world"},
			expected: []bool{true, false, true, false},
		},
		{
			name:     "string greater than or equal",
			op:       types.BinaryOpGte,
			lhs:      []string{"b", "a", "z", "hello"},
			rhs:      []string{"a", "a", "a", "hello"},
			expected: []bool{true, true, true, true},
		},
		{
			name:     "string less than",
			op:       types.BinaryOpLt,
			lhs:      []string{"a", "b", "a", "world"},
			rhs:      []string{"b", "a", "z", "hello"},
			expected: []bool{true, false, true, false},
		},
		{
			name:     "string less than or equal",
			op:       types.BinaryOpLte,
			lhs:      []string{"a", "a", "a", "hello"},
			rhs:      []string{"b", "a", "z", "hello"},
			expected: []bool{true, true, true, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lhsArray := createStringArray(mem, tt.lhs, nil)
			rhsArray := createStringArray(mem, tt.rhs, nil)
			defer lhsArray.array.Release()
			defer rhsArray.array.Release()

			fn, err := binaryFunctions.GetForSignature(tt.op, arrow.BinaryTypes.String)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhsArray, rhsArray)
			require.NoError(t, err)

			actual := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestIntegerComparisonFunctions(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)

	tests := []struct {
		name     string
		op       types.BinaryOp
		lhs      []int64
		rhs      []int64
		expected []bool
	}{
		{
			name:     "int64 equality",
			op:       types.BinaryOpEq,
			lhs:      []int64{1, 2, 3, 0, -1},
			rhs:      []int64{1, 3, 3, 0, 1},
			expected: []bool{true, false, true, true, false},
		},
		{
			name:     "int64 inequality",
			op:       types.BinaryOpNeq,
			lhs:      []int64{1, 2, 3, 0, -1},
			rhs:      []int64{1, 3, 3, 0, 1},
			expected: []bool{false, true, false, false, true},
		},
		{
			name:     "int64 greater than",
			op:       types.BinaryOpGt,
			lhs:      []int64{2, 1, 3, 0, -1},
			rhs:      []int64{1, 2, 3, 0, -2},
			expected: []bool{true, false, false, false, true},
		},
		{
			name:     "int64 greater than or equal",
			op:       types.BinaryOpGte,
			lhs:      []int64{2, 1, 3, 0, -1},
			rhs:      []int64{1, 1, 3, 0, -1},
			expected: []bool{true, true, true, true, true},
		},
		{
			name:     "int64 less than",
			op:       types.BinaryOpLt,
			lhs:      []int64{1, 2, 3, 0, -2},
			rhs:      []int64{2, 1, 3, 0, -1},
			expected: []bool{true, false, false, false, true},
		},
		{
			name:     "int64 less than or equal",
			op:       types.BinaryOpLte,
			lhs:      []int64{1, 1, 3, 0, -1},
			rhs:      []int64{2, 1, 3, 0, -1},
			expected: []bool{true, true, true, true, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lhsArray := createInt64Array(mem, tt.lhs, nil)
			rhsArray := createInt64Array(mem, tt.rhs, nil)
			defer lhsArray.array.Release()
			defer rhsArray.array.Release()

			fn, err := binaryFunctions.GetForSignature(tt.op, arrow.PrimitiveTypes.Int64)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhsArray, rhsArray)
			require.NoError(t, err)

			actual := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestTimestampComparisonFunctions(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)

	tests := []struct {
		name     string
		op       types.BinaryOp
		lhs      []arrow.Timestamp
		rhs      []arrow.Timestamp
		expected []bool
	}{
		{
			name:     "timestamp equality",
			op:       types.BinaryOpEq,
			lhs:      []arrow.Timestamp{1, 2, 3, 0, 100},
			rhs:      []arrow.Timestamp{1, 3, 3, 0, 50},
			expected: []bool{true, false, true, true, false},
		},
		{
			name:     "timestamp inequality",
			op:       types.BinaryOpNeq,
			lhs:      []arrow.Timestamp{1, 2, 3, 0, 100},
			rhs:      []arrow.Timestamp{1, 3, 3, 0, 50},
			expected: []bool{false, true, false, false, true},
		},
		{
			name:     "timestamp greater than",
			op:       types.BinaryOpGt,
			lhs:      []arrow.Timestamp{2, 1, 3, 0, 100},
			rhs:      []arrow.Timestamp{1, 2, 3, 0, 50},
			expected: []bool{true, false, false, false, true},
		},
		{
			name:     "timestamp greater than or equal",
			op:       types.BinaryOpGte,
			lhs:      []arrow.Timestamp{2, 1, 3, 0, 100},
			rhs:      []arrow.Timestamp{1, 1, 3, 0, 100},
			expected: []bool{true, true, true, true, true},
		},
		{
			name:     "timestamp less than",
			op:       types.BinaryOpLt,
			lhs:      []arrow.Timestamp{1, 2, 3, 0, 50},
			rhs:      []arrow.Timestamp{2, 1, 3, 0, 100},
			expected: []bool{true, false, false, false, true},
		},
		{
			name:     "timestamp less than or equal",
			op:       types.BinaryOpLte,
			lhs:      []arrow.Timestamp{1, 1, 3, 0, 100},
			rhs:      []arrow.Timestamp{2, 1, 3, 0, 100},
			expected: []bool{true, true, true, true, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lhsArray := createTimestampArray(mem, tt.lhs, nil)
			rhsArray := createTimestampArray(mem, tt.rhs, nil)
			defer lhsArray.array.Release()
			defer rhsArray.array.Release()

			fn, err := binaryFunctions.GetForSignature(tt.op, arrow.FixedWidthTypes.Timestamp_ns)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhsArray, rhsArray)
			require.NoError(t, err)

			actual := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestFloat64ComparisonFunctions(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)

	tests := []struct {
		name     string
		op       types.BinaryOp
		lhs      []float64
		rhs      []float64
		expected []bool
	}{
		{
			name:     "float64 equality",
			op:       types.BinaryOpEq,
			lhs:      []float64{1.0, 2.5, 3.14, 0.0, -1.5},
			rhs:      []float64{1.0, 2.6, 3.14, 0.0, 1.5},
			expected: []bool{true, false, true, true, false},
		},
		{
			name:     "float64 inequality",
			op:       types.BinaryOpNeq,
			lhs:      []float64{1.0, 2.5, 3.14, 0.0, -1.5},
			rhs:      []float64{1.0, 2.6, 3.14, 0.0, 1.5},
			expected: []bool{false, true, false, false, true},
		},
		{
			name:     "float64 greater than",
			op:       types.BinaryOpGt,
			lhs:      []float64{2.0, 1.5, 3.14, 0.0, -1.0},
			rhs:      []float64{1.0, 2.0, 3.14, 0.0, -2.0},
			expected: []bool{true, false, false, false, true},
		},
		{
			name:     "float64 greater than or equal",
			op:       types.BinaryOpGte,
			lhs:      []float64{2.0, 1.5, 3.14, 0.0, -1.0},
			rhs:      []float64{1.0, 1.5, 3.14, 0.0, -1.0},
			expected: []bool{true, true, true, true, true},
		},
		{
			name:     "float64 less than",
			op:       types.BinaryOpLt,
			lhs:      []float64{1.0, 2.0, 3.14, 0.0, -2.0},
			rhs:      []float64{2.0, 1.5, 3.14, 0.0, -1.0},
			expected: []bool{true, false, false, false, true},
		},
		{
			name:     "float64 less than or equal",
			op:       types.BinaryOpLte,
			lhs:      []float64{1.0, 1.5, 3.14, 0.0, -1.0},
			rhs:      []float64{2.0, 1.5, 3.14, 0.0, -1.0},
			expected: []bool{true, true, true, true, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lhsArray := createFloat64Array(mem, tt.lhs, nil)
			rhsArray := createFloat64Array(mem, tt.rhs, nil)
			defer lhsArray.array.Release()
			defer rhsArray.array.Release()

			fn, err := binaryFunctions.GetForSignature(tt.op, arrow.PrimitiveTypes.Float64)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhsArray, rhsArray)
			require.NoError(t, err)

			actual := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestStringMatchingFunctions(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)

	tests := []struct {
		name     string
		op       types.BinaryOp
		lhs      []string
		rhs      []string
		expected []bool
	}{
		{
			name:     "string contains",
			op:       types.BinaryOpMatchSubstr,
			lhs:      []string{"hello world", "test string", "foobar", ""},
			rhs:      []string{"world", "test", "baz", ""},
			expected: []bool{true, true, false, true},
		},
		{
			name:     "string does not contain",
			op:       types.BinaryOpNotMatchSubstr,
			lhs:      []string{"hello world", "test string", "foobar", ""},
			rhs:      []string{"world", "test", "baz", ""},
			expected: []bool{false, false, true, false},
		},
		{
			name:     "regex match",
			op:       types.BinaryOpMatchRe,
			lhs:      []string{"hello123", "test456", "abc", ""},
			rhs:      []string{"^hello\\d+$", "^\\d+", "^[a-z]+$", ".+"},
			expected: []bool{true, false, true, false},
		},
		{
			name:     "regex not match",
			op:       types.BinaryOpNotMatchRe,
			lhs:      []string{"hello123", "test456", "abc", ""},
			rhs:      []string{"^hello\\d+$", "^\\d+", "^[a-z]+$", ".+"},
			expected: []bool{false, true, false, true},
		},
		{
			name:     "case sensitive substring matching",
			op:       types.BinaryOpMatchSubstr,
			lhs:      []string{"Hello World", "TEST", "CaseSensitive"},
			rhs:      []string{"hello", "test", "Case"},
			expected: []bool{false, false, true},
		},
		{
			name:     "special characters in contains",
			op:       types.BinaryOpMatchSubstr,
			lhs:      []string{"hello@world.com", "test[123]", "foo.bar", ""},
			rhs:      []string{"@world", "[123]", ".", ""},
			expected: []bool{true, true, true, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lhsArray := createStringArray(mem, tt.lhs, nil)
			rhsArray := createStringArray(mem, tt.rhs, nil)
			defer lhsArray.array.Release()
			defer rhsArray.array.Release()

			fn, err := binaryFunctions.GetForSignature(tt.op, arrow.BinaryTypes.String)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhsArray, rhsArray)
			require.NoError(t, err)

			actual := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestNullValueHandling(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)

	tests := []struct {
		name     string
		op       types.BinaryOp
		dataType arrow.DataType
		setup    func() (*Array, *Array)
		expected []bool
	}{
		{
			name:     "boolean with nulls",
			op:       types.BinaryOpEq,
			dataType: arrow.FixedWidthTypes.Boolean,
			setup: func() (*Array, *Array) {
				lhs := createBoolArray(mem, []bool{true, false, true}, []bool{false, true, false})
				rhs := createBoolArray(mem, []bool{true, false, false}, []bool{true, false, false})
				return lhs, rhs
			},
			expected: []bool{false, false, false}, // nulls should result in false
		},
		{
			name:     "string with nulls",
			op:       types.BinaryOpEq,
			dataType: arrow.BinaryTypes.String,
			setup: func() (*Array, *Array) {
				lhs := createStringArray(mem, []string{"hello", "world", "test"}, []bool{false, true, false})
				rhs := createStringArray(mem, []string{"hello", "world", "different"}, []bool{true, false, false})
				return lhs, rhs
			},
			expected: []bool{false, false, false}, // nulls should result in false
		},
		{
			name:     "int64 with nulls",
			op:       types.BinaryOpGt,
			dataType: arrow.PrimitiveTypes.Int64,
			setup: func() (*Array, *Array) {
				lhs := createInt64Array(mem, []int64{5, 10, 15}, []bool{false, true, false})
				rhs := createInt64Array(mem, []int64{3, 8, 20}, []bool{true, false, false})
				return lhs, rhs
			},
			expected: []bool{false, false, false}, // nulls should result in false
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lhs, rhs := tt.setup()
			defer lhs.array.Release()
			defer rhs.array.Release()

			fn, err := binaryFunctions.GetForSignature(tt.op, tt.dataType)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhs, rhs)
			require.NoError(t, err)

			actual := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestArrayLengthMismatch(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)

	tests := []struct {
		name     string
		op       types.BinaryOp
		dataType arrow.DataType
		setup    func() (*Array, *Array)
	}{
		{
			name:     "boolean length mismatch",
			op:       types.BinaryOpEq,
			dataType: arrow.FixedWidthTypes.Boolean,
			setup: func() (*Array, *Array) {
				lhs := createBoolArray(mem, []bool{true, false}, nil)
				rhs := createBoolArray(mem, []bool{true, false, true}, nil)
				return lhs, rhs
			},
		},
		{
			name:     "string length mismatch",
			op:       types.BinaryOpEq,
			dataType: arrow.BinaryTypes.String,
			setup: func() (*Array, *Array) {
				lhs := createStringArray(mem, []string{"hello"}, nil)
				rhs := createStringArray(mem, []string{"hello", "world"}, nil)
				return lhs, rhs
			},
		},
		{
			name:     "int64 length mismatch",
			op:       types.BinaryOpGt,
			dataType: arrow.PrimitiveTypes.Int64,
			setup: func() (*Array, *Array) {
				lhs := createInt64Array(mem, []int64{1, 2, 3}, nil)
				rhs := createInt64Array(mem, []int64{1, 2}, nil)
				return lhs, rhs
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lhs, rhs := tt.setup()
			defer lhs.array.Release()
			defer rhs.array.Release()

			fn, err := binaryFunctions.GetForSignature(tt.op, tt.dataType)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhs, rhs)
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

func TestRegexCompileError(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)

	// Test with invalid regex patterns
	lhs := createStringArray(mem, []string{"hello", "world"}, nil)
	rhs := createStringArray(mem, []string{"[", "("}, nil) // Invalid regex patterns
	defer lhs.array.Release()
	defer rhs.array.Release()

	fn, err := binaryFunctions.GetForSignature(types.BinaryOpMatchRe, arrow.BinaryTypes.String)
	require.NoError(t, err)

	_, err = fn.Evaluate(lhs, rhs)
	require.Error(t, err)
}

func TestBoolToIntConversion(t *testing.T) {
	tests := []struct {
		name     string
		input    bool
		expected int
	}{
		{
			name:     "true converts to 1",
			input:    true,
			expected: 1,
		},
		{
			name:     "false converts to 0",
			input:    false,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := boolToInt(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEmptyArrays(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)

	// Test with empty arrays
	lhs := createStringArray(mem, []string{}, nil)
	rhs := createStringArray(mem, []string{}, nil)
	defer lhs.array.Release()
	defer rhs.array.Release()

	fn, err := binaryFunctions.GetForSignature(types.BinaryOpEq, arrow.BinaryTypes.String)
	require.NoError(t, err)

	result, err := fn.Evaluate(lhs, rhs)
	require.NoError(t, err)

	assert.Equal(t, int64(0), result.Len())
}
