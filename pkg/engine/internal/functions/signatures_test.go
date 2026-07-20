package functions

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestBinaryReturnType(t *testing.T) {
	tests := []struct {
		name        string
		op          types.BinaryOp
		lhs, rhs    types.DataType
		expected    types.DataType
		expectError bool
	}{
		{
			name:     "string equality returns bool",
			op:       types.BinaryOpEq,
			lhs:      types.Loki.String,
			rhs:      types.Loki.String,
			expected: types.Loki.Bool,
		},
		{
			name:     "timestamp comparison returns bool",
			op:       types.BinaryOpGte,
			lhs:      types.Loki.Timestamp,
			rhs:      types.Loki.Timestamp,
			expected: types.Loki.Bool,
		},
		{
			name:     "float division returns float",
			op:       types.BinaryOpDiv,
			lhs:      types.Loki.Float,
			rhs:      types.Loki.Float,
			expected: types.Loki.Float,
		},
		{
			name:     "integer addition returns float",
			op:       types.BinaryOpAdd,
			lhs:      types.Loki.Integer,
			rhs:      types.Loki.Integer,
			expected: types.Loki.Float,
		},
		{
			name:     "regex match returns bool",
			op:       types.BinaryOpMatchRe,
			lhs:      types.Loki.String,
			rhs:      types.Loki.String,
			expected: types.Loki.Bool,
		},
		{
			name:     "logical and returns bool",
			op:       types.BinaryOpAnd,
			lhs:      types.Loki.Bool,
			rhs:      types.Loki.Bool,
			expected: types.Loki.Bool,
		},
		{
			name:     "duration compares as integer",
			op:       types.BinaryOpGt,
			lhs:      types.Loki.Duration,
			rhs:      types.Loki.Integer,
			expected: types.Loki.Bool,
		},
		{
			name:        "operand types must match",
			op:          types.BinaryOpEq,
			lhs:         types.Loki.String,
			rhs:         types.Loki.Integer,
			expectError: true,
		},
		{
			name:        "arithmetic on strings is not supported",
			op:          types.BinaryOpAdd,
			lhs:         types.Loki.String,
			rhs:         types.Loki.String,
			expectError: true,
		},
		{
			name:        "regex match on integers is not supported",
			op:          types.BinaryOpMatchRe,
			lhs:         types.Loki.Integer,
			rhs:         types.Loki.Integer,
			expectError: true,
		},
		{
			name:        "logical and on strings is not supported",
			op:          types.BinaryOpAnd,
			lhs:         types.Loki.String,
			rhs:         types.Loki.String,
			expectError: true,
		},
		{
			name:        "pattern match has no implementation",
			op:          types.BinaryOpMatchPattern,
			lhs:         types.Loki.String,
			rhs:         types.Loki.String,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret, err := BinaryReturnType(tt.op, tt.lhs, tt.rhs)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, ret)
			}
		})
	}
}

func TestUnaryReturnType(t *testing.T) {
	tests := []struct {
		name        string
		op          types.UnaryOp
		operand     types.DataType
		expected    types.DataType
		expectError bool
	}{
		{
			name:     "not on bool returns bool",
			op:       types.UnaryOpNot,
			operand:  types.Loki.Bool,
			expected: types.Loki.Bool,
		},
		{
			name:     "cast float on string returns float",
			op:       types.UnaryOpCastFloat,
			operand:  types.Loki.String,
			expected: types.Loki.Float,
		},
		{
			name:     "cast duration on string returns float",
			op:       types.UnaryOpCastDuration,
			operand:  types.Loki.String,
			expected: types.Loki.Float,
		},
		{
			name:        "not on string is not supported",
			op:          types.UnaryOpNot,
			operand:     types.Loki.String,
			expectError: true,
		},
		{
			name:        "cast on float is not supported",
			op:          types.UnaryOpCastFloat,
			operand:     types.Loki.Float,
			expectError: true,
		},
		{
			name:        "abs has no implementation",
			op:          types.UnaryOpAbs,
			operand:     types.Loki.Float,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret, err := UnaryReturnType(tt.op, tt.operand)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, ret)
			}
		})
	}
}

func TestHasVariadic(t *testing.T) {
	require.True(t, HasVariadic(types.VariadicOpParseLogfmt))
	require.True(t, HasVariadic(types.VariadicOpParseJSON))
	require.True(t, HasVariadic(types.VariadicOpParseRegexp))
	require.True(t, HasVariadic(types.VariadicOpParseLabelfmt))
	require.True(t, HasVariadic(types.VariadicOpParseLinefmt))
	require.False(t, HasVariadic(types.VariadicOpInvalid))
}

// TestRegisterPanicsOnUndeclaredSignature verifies that implementations cannot
// be registered for signatures that are not declared in the catalog, so that
// the catalog and the registries cannot drift apart.
func TestRegisterPanicsOnUndeclaredSignature(t *testing.T) {
	require.Panics(t, func() {
		Binary.Register(types.BinaryOpAdd, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{})
	})
	require.Panics(t, func() {
		Unary.Register(types.UnaryOpNot, arrow.BinaryTypes.String, UnaryFunc(nil))
	})
	require.Panics(t, func() {
		Variadic.Register(types.VariadicOpInvalid, VariadicFunctionFunc(nil))
	})
}

// TestCatalogCoversRegistrations verifies that every signature declared in the
// catalog for operations implemented in this package has a registered
// implementation. Cast and parse implementations are registered by the
// executor package and are verified there.
func TestCatalogCoversRegistrations(t *testing.T) {
	for op, sig := range binarySignatures {
		for ty := range sig {
			fn, err := Binary.GetForSignature(op, arrowTypeForTest(t, ty))
			require.NoError(t, err, "missing binary function implementation for declared signature %v(%v)", op, ty)
			require.NotNil(t, fn)
		}
	}
}

func arrowTypeForTest(t *testing.T, ty types.Type) arrow.DataType {
	t.Helper()
	switch ty {
	case types.BOOL:
		return arrow.FixedWidthTypes.Boolean
	case types.STRING:
		return arrow.BinaryTypes.String
	case types.INT64:
		return arrow.PrimitiveTypes.Int64
	case types.FLOAT64:
		return arrow.PrimitiveTypes.Float64
	case types.TIMESTAMP:
		return arrow.FixedWidthTypes.Timestamp_ns
	default:
		t.Fatalf("no arrow type mapping for %v", ty)
		return nil
	}
}
