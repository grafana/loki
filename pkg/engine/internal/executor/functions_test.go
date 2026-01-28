package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// Helper function to create a boolean array
func createBoolArray(values []bool, nulls []bool) arrow.Array {
	builder := array.NewBooleanBuilder(memory.DefaultAllocator)

	for i, val := range values {
		if nulls != nil && i < len(nulls) && nulls[i] {
			builder.AppendNull()
		} else {
			builder.Append(val)
		}
	}

	return builder.NewArray()
}

// Helper function to create a string array
func createStringArray(values []string, nulls []bool) arrow.Array {
	builder := array.NewStringBuilder(memory.DefaultAllocator)

	for i, val := range values {
		if nulls != nil && i < len(nulls) && nulls[i] {
			builder.AppendNull()
		} else {
			builder.Append(val)
		}
	}

	return builder.NewArray()
}

// Helper function to create an int64 array
func createInt64Array(values []int64, nulls []bool) arrow.Array {
	builder := array.NewInt64Builder(memory.DefaultAllocator)

	for i, val := range values {
		if nulls != nil && i < len(nulls) && nulls[i] {
			builder.AppendNull()
		} else {
			builder.Append(val)
		}
	}

	return builder.NewArray()
}

// Helper function to create a arrow.Timestamp array
func createTimestampArray(values []arrow.Timestamp, nulls []bool) arrow.Array {
	builder := array.NewTimestampBuilder(memory.DefaultAllocator, &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"})

	for i, val := range values {
		if nulls != nil && i < len(nulls) && nulls[i] {
			builder.AppendNull()
		} else {
			builder.Append(val)
		}
	}

	return builder.NewArray()
}

// Helper function to create a float64 array
func createFloat64Array(values []float64, nulls []bool) arrow.Array {
	builder := array.NewFloat64Builder(memory.DefaultAllocator)

	for i, val := range values {
		if nulls != nil && i < len(nulls) && nulls[i] {
			builder.AppendNull()
		} else {
			builder.Append(val)
		}
	}

	return builder.NewArray()
}

// Helper function to extract boolean values from result
func extractBoolValues(result arrow.Array) ([]bool, []bool) {
	arr := result.(*array.Boolean)

	values := make([]bool, arr.Len())
	nulls := make([]bool, arr.Len())
	for i := 0; i < arr.Len(); i++ {
		if arr.IsNull(i) {
			nulls[i] = true
		} else {
			values[i] = arr.Value(i)
		}
	}
	return values, nulls
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
			name:        "valid div operation",
			op:          types.BinaryOpDiv,
			dataType:    arrow.PrimitiveTypes.Float64,
			expectError: false,
		},
		{
			name:        "valid add operation",
			op:          types.BinaryOpAdd,
			dataType:    arrow.PrimitiveTypes.Float64,
			expectError: false,
		},
		{
			name:        "valid Mul operation",
			op:          types.BinaryOpMul,
			dataType:    arrow.PrimitiveTypes.Float64,
			expectError: false,
		},
		{
			name:        "valid sub operation",
			op:          types.BinaryOpSub,
			dataType:    arrow.PrimitiveTypes.Float64,
			expectError: false,
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
			lhsArray := createBoolArray(tt.lhs, nil)
			rhsArray := createBoolArray(tt.rhs, nil)

			fn, err := binaryFunctions.GetForSignature(tt.op, arrow.FixedWidthTypes.Boolean)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhsArray, rhsArray, false, false)
			require.NoError(t, err)

			actual, _ := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestBooleanLogicalOperations(t *testing.T) {
	tests := []struct {
		name     string
		op       types.BinaryOp
		lhs      []bool
		rhs      []bool
		expected []bool
	}{
		{
			name:     "logical AND",
			op:       types.BinaryOpAnd,
			lhs:      []bool{true, true, false, false},
			rhs:      []bool{true, false, true, false},
			expected: []bool{true, false, false, false},
		},
		{
			name:     "logical OR",
			op:       types.BinaryOpOr,
			lhs:      []bool{true, true, false, false},
			rhs:      []bool{true, false, true, false},
			expected: []bool{true, true, true, false},
		},
		{
			name:     "logical XOR",
			op:       types.BinaryOpXor,
			lhs:      []bool{true, true, false, false},
			rhs:      []bool{true, false, true, false},
			expected: []bool{false, true, true, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lhsArray := createBoolArray(tt.lhs, nil)
			rhsArray := createBoolArray(tt.rhs, nil)

			fn, err := binaryFunctions.GetForSignature(tt.op, arrow.FixedWidthTypes.Boolean)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhsArray, rhsArray, false, false)
			require.NoError(t, err)

			actual, _ := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestStringComparisonFunctions(t *testing.T) {
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
			lhsArray := createStringArray(tt.lhs, nil)
			rhsArray := createStringArray(tt.rhs, nil)

			fn, err := binaryFunctions.GetForSignature(tt.op, arrow.BinaryTypes.String)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhsArray, rhsArray, false, false)
			require.NoError(t, err)

			actual, _ := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestIntegerComparisonFunctions(t *testing.T) {
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
			lhsArray := createInt64Array(tt.lhs, nil)
			rhsArray := createInt64Array(tt.rhs, nil)

			fn, err := binaryFunctions.GetForSignature(tt.op, arrow.PrimitiveTypes.Int64)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhsArray, rhsArray, false, false)
			require.NoError(t, err)

			actual, _ := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestTimestampComparisonFunctions(t *testing.T) {
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
			lhsArray := createTimestampArray(tt.lhs, nil)
			rhsArray := createTimestampArray(tt.rhs, nil)

			fn, err := binaryFunctions.GetForSignature(tt.op, arrow.FixedWidthTypes.Timestamp_ns)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhsArray, rhsArray, false, false)
			require.NoError(t, err)

			actual, _ := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestFloat64ComparisonFunctions(t *testing.T) {
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
			lhsArray := createFloat64Array(tt.lhs, nil)
			rhsArray := createFloat64Array(tt.rhs, nil)

			fn, err := binaryFunctions.GetForSignature(tt.op, arrow.PrimitiveTypes.Float64)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhsArray, rhsArray, false, false)
			require.NoError(t, err)

			actual, _ := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestStringMatchingFunctions(t *testing.T) {
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
			lhsArray := createStringArray(tt.lhs, nil)
			rhsArray := createStringArray(tt.rhs, nil)

			fn, err := binaryFunctions.GetForSignature(tt.op, arrow.BinaryTypes.String)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhsArray, rhsArray, false, false)
			require.NoError(t, err)

			actual, _ := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestCaseInsensitiveStringFunctions(t *testing.T) {
	tests := []struct {
		name     string
		op       types.BinaryOp
		lhs      []string
		rhs      []string
		expected []bool
	}{
		{
			name:     "case insensitive equality - lowercase pattern",
			op:       types.BinaryOpEqCaseInsensitive,
			lhs:      []string{"Hello", "WORLD", "TeSt", ""},
			rhs:      []string{"HELLO", "WORLD", "TEST", ""},
			expected: []bool{true, true, true, true},
		},
		{
			name:     "case insensitive equality - mixed case pattern",
			op:       types.BinaryOpEqCaseInsensitive,
			lhs:      []string{"ERROR", "Warning", "info"},
			rhs:      []string{"ERROR", "WARNING", "INFO"},
			expected: []bool{true, true, true},
		},
		{
			name:     "case insensitive equality - no match",
			op:       types.BinaryOpEqCaseInsensitive,
			lhs:      []string{"HELLO", "WORLD", "TEST"},
			rhs:      []string{"GOODBYE", "EARTH", "EXAM"},
			expected: []bool{false, false, false},
		},
		{
			name:     "case insensitive inequality - match",
			op:       types.BinaryOpNotEqCaseInsensitive,
			lhs:      []string{"Hello", "WORLD"},
			rhs:      []string{"HELLO", "WORLD"},
			expected: []bool{false, false},
		},
		{
			name:     "case insensitive inequality - no match",
			op:       types.BinaryOpNotEqCaseInsensitive,
			lhs:      []string{"HELLO", "WORLD"},
			rhs:      []string{"GOODBYE", "EARTH"},
			expected: []bool{true, true},
		},
		{
			name:     "case insensitive substring - basic",
			op:       types.BinaryOpMatchSubstrCaseInsensitive,
			lhs:      []string{"Hello World", "TEST STRING", "FooBar"},
			rhs:      []string{"WORLD", "STRING", "FOOBAR"},
			expected: []bool{true, true, true},
		},
		{
			name:     "case insensitive substring - mixed case lhs and rhs",
			op:       types.BinaryOpMatchSubstrCaseInsensitive,
			lhs:      []string{"Error Occurred", "WARNING message", "Info: log"},
			rhs:      []string{"ERROR", "WARNING", "INFO"},
			expected: []bool{true, true, true},
		},
		{
			name:     "case insensitive substring - no match",
			op:       types.BinaryOpMatchSubstrCaseInsensitive,
			lhs:      []string{"hello world", "test string"},
			rhs:      []string{"GOODBYE", "EXAM"},
			expected: []bool{false, false},
		},
		{
			name:     "case insensitive substring - empty string",
			op:       types.BinaryOpMatchSubstrCaseInsensitive,
			lhs:      []string{"anything", ""},
			rhs:      []string{"", ""},
			expected: []bool{true, true},
		},
		{
			name:     "case insensitive not substring - match",
			op:       types.BinaryOpNotMatchSubstrCaseInsensitive,
			lhs:      []string{"Hello World", "TEST"},
			rhs:      []string{"WORLD", "TEST"},
			expected: []bool{false, false},
		},
		{
			name:     "case insensitive not substring - no match",
			op:       types.BinaryOpNotMatchSubstrCaseInsensitive,
			lhs:      []string{"hello world", "test string"},
			rhs:      []string{"GOODBYE", "EXAM"},
			expected: []bool{true, true},
		},
		{
			name:     "case insensitive with unicode",
			op:       types.BinaryOpMatchSubstrCaseInsensitive,
			lhs:      []string{"Error in MÜNCHEN", "ΑΒΓΔ test"},
			rhs:      []string{"MÜNCHEN", "ΑΒΓΔ"},
			expected: []bool{true, true},
		},
		{
			name:     "case insensitive with numbers and special chars",
			op:       types.BinaryOpEqCaseInsensitive,
			lhs:      []string{"Error123!", "Test@456"},
			rhs:      []string{"ERROR123!", "TEST@456"},
			expected: []bool{true, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lhsArray := createStringArray(tt.lhs, nil)
			rhsArray := createStringArray(tt.rhs, nil)

			fn, err := binaryFunctions.GetForSignature(tt.op, arrow.BinaryTypes.String)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhsArray, rhsArray, false, false)
			require.NoError(t, err)

			actual, _ := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestCompileRegexMatchFunctions(t *testing.T) {
	tests := []struct {
		name     string
		op       types.BinaryOp
		lhs      []string
		rhs      []string
		expected []bool
	}{
		{
			name:     "regex match", // |~ "^\w+\d+$"
			op:       types.BinaryOpMatchRe,
			lhs:      []string{"foo123", "foo", "bar456", "bar"},
			rhs:      []string{"^\\w+\\d+$", "^\\w+\\d+$", "^\\w+\\d+$", "^\\w+\\d+$"},
			expected: []bool{true, false, true, false},
		},
		{
			name:     "regex not match", // !~ "^\w+\d+$"
			op:       types.BinaryOpNotMatchRe,
			lhs:      []string{"foo123", "foo", "bar456", "bar"},
			rhs:      []string{"^\\w+\\d+$", "^\\w+\\d+$", "^\\w+\\d+$", "^\\w+\\d+$"},
			expected: []bool{false, true, false, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lhsArray := createStringArray(tt.lhs, nil)
			rhsArray := createStringArray(tt.rhs, nil)

			fn, err := binaryFunctions.GetForSignature(tt.op, arrow.BinaryTypes.String)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhsArray, rhsArray, false, true)
			require.NoError(t, err)

			actual, _ := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}

}

func TestNullValueHandling(t *testing.T) {
	tests := []struct {
		name     string
		op       types.BinaryOp
		dataType arrow.DataType
		setup    func() (arrow.Array, arrow.Array)
		expected []bool
	}{
		{
			name:     "boolean with nulls",
			op:       types.BinaryOpEq,
			dataType: arrow.FixedWidthTypes.Boolean,
			setup: func() (arrow.Array, arrow.Array) {
				lhs := createBoolArray([]bool{true, false, true}, []bool{false, true, false})
				rhs := createBoolArray([]bool{true, false, false}, []bool{true, false, false})
				return lhs, rhs
			},
			expected: []bool{false, false, false}, // nulls should result in false
		},
		{
			name:     "string with nulls",
			op:       types.BinaryOpEq,
			dataType: arrow.BinaryTypes.String,
			setup: func() (arrow.Array, arrow.Array) {
				lhs := createStringArray([]string{"hello", "world", "test"}, []bool{false, true, false})
				rhs := createStringArray([]string{"hello", "world", "different"}, []bool{true, false, false})
				return lhs, rhs
			},
			expected: []bool{false, false, false}, // nulls should result in false
		},
		{
			name:     "int64 with nulls",
			op:       types.BinaryOpGt,
			dataType: arrow.PrimitiveTypes.Int64,
			setup: func() (arrow.Array, arrow.Array) {
				lhs := createInt64Array([]int64{5, 10, 15}, []bool{false, true, false})
				rhs := createInt64Array([]int64{3, 8, 20}, []bool{true, false, false})
				return lhs, rhs
			},
			expected: []bool{false, false, false}, // nulls should result in false
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lhs, rhs := tt.setup()

			fn, err := binaryFunctions.GetForSignature(tt.op, tt.dataType)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhs, rhs, false, false)
			require.NoError(t, err)

			actual, _ := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestArrayLengthMismatch(t *testing.T) {
	tests := []struct {
		name     string
		op       types.BinaryOp
		dataType arrow.DataType
		setup    func() (arrow.Array, arrow.Array)
	}{
		{
			name:     "boolean length mismatch",
			op:       types.BinaryOpEq,
			dataType: arrow.FixedWidthTypes.Boolean,
			setup: func() (arrow.Array, arrow.Array) {
				lhs := createBoolArray([]bool{true, false}, nil)
				rhs := createBoolArray([]bool{true, false, true}, nil)
				return lhs, rhs
			},
		},
		{
			name:     "string length mismatch",
			op:       types.BinaryOpEq,
			dataType: arrow.BinaryTypes.String,
			setup: func() (arrow.Array, arrow.Array) {
				lhs := createStringArray([]string{"hello"}, nil)
				rhs := createStringArray([]string{"hello", "world"}, nil)
				return lhs, rhs
			},
		},
		{
			name:     "int64 length mismatch",
			op:       types.BinaryOpGt,
			dataType: arrow.PrimitiveTypes.Int64,
			setup: func() (arrow.Array, arrow.Array) {
				lhs := createInt64Array([]int64{1, 2, 3}, nil)
				rhs := createInt64Array([]int64{1, 2}, nil)
				return lhs, rhs
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lhs, rhs := tt.setup()

			fn, err := binaryFunctions.GetForSignature(tt.op, tt.dataType)
			require.NoError(t, err)

			result, err := fn.Evaluate(lhs, rhs, false, false)
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

func TestRegexCompileError(t *testing.T) {
	// Test with invalid regex patterns
	lhs := createStringArray([]string{"hello", "world"}, nil)
	rhs := createStringArray([]string{"[", "("}, nil) // Invalid regex patterns

	fn, err := binaryFunctions.GetForSignature(types.BinaryOpMatchRe, arrow.BinaryTypes.String)
	require.NoError(t, err)

	_, err = fn.Evaluate(lhs, rhs, false, false)
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
	// Test with empty arrays
	lhs := createStringArray([]string{}, nil)
	rhs := createStringArray([]string{}, nil)

	fn, err := binaryFunctions.GetForSignature(types.BinaryOpEq, arrow.BinaryTypes.String)
	require.NoError(t, err)

	result, err := fn.Evaluate(lhs, rhs, false, false)
	require.NoError(t, err)

	assert.Equal(t, int(0), result.Len())
}

func TestUnaryNot(t *testing.T) {
	tests := []struct {
		name     string
		input    []bool
		nulls    []bool
		expected []bool
	}{
		{
			name:     "NOT operation",
			input:    []bool{true, false, true, false},
			nulls:    nil,
			expected: []bool{false, true, false, true},
		},
		{
			name:     "NOT with nulls",
			input:    []bool{true, false, true},
			nulls:    []bool{false, true, false},
			expected: []bool{false, false, false}, // nulls result in false in extractBoolValues
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputArray := createBoolArray(tt.input, tt.nulls)

			fn, err := unaryFunctions.GetForSignature(types.UnaryOpNot, arrow.FixedWidthTypes.Boolean)
			require.NoError(t, err)

			result, err := fn.Evaluate(inputArray)
			require.NoError(t, err)

			actual, nulls := extractBoolValues(result)
			assert.Equal(t, tt.expected, actual)
			if tt.nulls != nil {
				assert.Equal(t, tt.nulls, nulls)
			}
		})
	}
}
