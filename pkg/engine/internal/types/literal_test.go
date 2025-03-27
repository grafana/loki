package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLiteralRepresentation(t *testing.T) {
	testCases := []struct {
		name     string
		literal  Literal
		expected string
	}{
		{
			name:     "null literal",
			literal:  NullLiteral(),
			expected: "NULL",
		},
		{
			name:     "bool literal - true",
			literal:  BoolLiteral(true),
			expected: "true",
		},
		{
			name:     "bool literal - false",
			literal:  BoolLiteral(false),
			expected: "false",
		},
		{
			name:     "string literal",
			literal:  StringLiteral("test"),
			expected: `"test"`,
		},
		{
			name:     "string literal with quotes",
			literal:  StringLiteral(`test "quoted" string`),
			expected: `"test "quoted" string"`,
		},
		{
			name:     "float literal - integer value",
			literal:  FloatLiteral(42.0),
			expected: "42",
		},
		{
			name:     "float literal - decimal value",
			literal:  FloatLiteral(3.14159),
			expected: "3.14159",
		},
		{
			name:     "int literal - positive",
			literal:  IntLiteral(123),
			expected: "123",
		},
		{
			name:     "int literal - negative",
			literal:  IntLiteral(-456),
			expected: "-456",
		},
		{
			name:     "timestamp literal",
			literal:  TimestampLiteral(1625097600000),
			expected: "1625097600000",
		},
		{
			name:     "byte array literal",
			literal:  ByteArrayLiteral([]byte("hello")),
			expected: "[104 101 108 108 111]",
		},
		{
			name:     "invalid literal",
			literal:  Literal{Value: make(chan int)}, // channels are not supported types
			expected: "invalid",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.literal.String()
			require.Equal(t, tc.expected, result)
		})
	}
}
