package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func Test_buildLogsPredicate(t *testing.T) {
	var (
		// streamID isn't included since [physical.Expression]s can't reference
		// stream IDs.

		timestampColumn = &logs.Column{Type: logs.ColumnTypeTimestamp}
		metadataColumn  = &logs.Column{Name: "metadata", Type: logs.ColumnTypeMetadata}
		messageColumn   = &logs.Column{Type: logs.ColumnTypeMessage}

		columns = []*logs.Column{timestampColumn, metadataColumn, messageColumn}
	)

	tt := []struct {
		name   string
		expr   physicalpb.Expression
		expect logs.Predicate
	}{
		{
			name:   "literal true",
			expr:   *physical.NewLiteral(true).ToExpression(),
			expect: logs.TruePredicate{},
		},
		{
			name:   "literal false",
			expr:   *physical.NewLiteral(false).ToExpression(),
			expect: logs.FalsePredicate{},
		},

		{
			name: "unary NOT",
			expr: *(&physicalpb.UnaryExpression{
				Op:    physicalpb.UNARY_OP_NOT,
				Value: physical.NewLiteral(false).ToExpression(),
			}).ToExpression(),
			expect: logs.NotPredicate{
				Inner: logs.FalsePredicate{},
			},
		},

		{
			name: "binary AND",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_AND,
				Left:  physical.NewLiteral(true).ToExpression(),
				Right: physical.NewLiteral(false).ToExpression(),
			}).ToExpression(),
			expect: logs.AndPredicate{
				Left:  logs.TruePredicate{},
				Right: logs.FalsePredicate{},
			},
		},
		{
			name: "binary OR",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_OR,
				Left:  physical.NewLiteral(true).ToExpression(),
				Right: physical.NewLiteral(false).ToExpression(),
			}).ToExpression(),
			expect: logs.OrPredicate{
				Left:  logs.TruePredicate{},
				Right: logs.FalsePredicate{},
			},
		},
		{
			name: "builtin timestamp reference",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_EQ,
				Left:  columnRef(physicalpb.COLUMN_TYPE_BUILTIN, types.ColumnNameBuiltinTimestamp).ToExpression(),
				Right: physical.NewLiteral(int64(1234567890)).ToExpression(),
			}).ToExpression(),
			expect: logs.EqualPredicate{
				Column: timestampColumn,
				Value:  scalar.NewInt64Scalar(1234567890),
			},
		},
		{
			name: "builtin message reference",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_EQ,
				Left:  columnRef(physicalpb.COLUMN_TYPE_BUILTIN, types.ColumnNameBuiltinMessage).ToExpression(),
				Right: physical.NewLiteral(int64(9876543210)).ToExpression(),
			}).ToExpression(),
			expect: logs.EqualPredicate{
				Column: messageColumn,
				Value:  scalar.NewInt64Scalar(9876543210),
			},
		},
		{
			name: "metadata reference",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_EQ,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "metadata").ToExpression(),
				Right: physical.NewLiteral(int64(5555555555)).ToExpression(),
			}).ToExpression(),
			expect: logs.EqualPredicate{
				Column: metadataColumn,
				Value:  scalar.NewInt64Scalar(5555555555),
			},
		},
		{
			name: "ambiguous metadata reference",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_EQ,
				Left:  columnRef(physicalpb.COLUMN_TYPE_AMBIGUOUS, "metadata").ToExpression(),
				Right: physical.NewLiteral(int64(7777777777)).ToExpression(),
			}).ToExpression(),
			expect: logs.EqualPredicate{
				Column: metadataColumn,
				Value:  scalar.NewInt64Scalar(7777777777),
			},
		},

		{
			name: "check column for null literal",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_EQ,
				Left:  columnRef(physicalpb.COLUMN_TYPE_BUILTIN, types.ColumnNameBuiltinTimestamp).ToExpression(),
				Right: physical.NewLiteral(nil).ToExpression(),
			}).ToExpression(),
			expect: logs.EqualPredicate{
				Column: timestampColumn,
				Value:  scalar.ScalarNull,
			},
		},
		{
			name: "check column for integer literal",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_EQ,
				Left:  columnRef(physicalpb.COLUMN_TYPE_BUILTIN, types.ColumnNameBuiltinTimestamp).ToExpression(),
				Right: physical.NewLiteral(int64(42)).ToExpression(),
			}).ToExpression(),
			expect: logs.EqualPredicate{
				Column: timestampColumn,
				Value:  scalar.NewInt64Scalar(42),
			},
		},
		{
			name: "check column for bytes literal",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_EQ,
				Left:  columnRef(physicalpb.COLUMN_TYPE_BUILTIN, types.ColumnNameBuiltinTimestamp).ToExpression(),
				Right: physical.NewLiteral(types.Bytes(1024)).ToExpression(),
			}).ToExpression(),
			expect: logs.EqualPredicate{
				Column: timestampColumn,
				Value:  scalar.NewInt64Scalar(1024),
			},
		},
		{
			name: "check column for timestamp literal",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_EQ,
				Left:  columnRef(physicalpb.COLUMN_TYPE_BUILTIN, types.ColumnNameBuiltinTimestamp).ToExpression(),
				Right: physical.NewLiteral(types.Timestamp(1609459200000000000)).ToExpression(), // 2021-01-01 00:00:00 UTC in nanoseconds
			}).ToExpression(),
			expect: logs.EqualPredicate{
				Column: timestampColumn,
				Value:  scalar.NewTimestampScalar(arrow.Timestamp(1609459200000000000), arrow.FixedWidthTypes.Timestamp_ns),
			},
		},
		{
			name: "check column for string literal",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_EQ,
				Left:  columnRef(physicalpb.COLUMN_TYPE_BUILTIN, types.ColumnNameBuiltinTimestamp).ToExpression(),
				Right: physical.NewLiteral("hello world").ToExpression(),
			}).ToExpression(),
			expect: logs.EqualPredicate{
				Column: timestampColumn,
				Value:  scalar.NewBinaryScalar(memory.NewBufferBytes([]byte("hello world")), arrow.BinaryTypes.Binary),
			},
		},

		{
			name: "binary EQ",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_EQ,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "metadata").ToExpression(),
				Right: physical.NewLiteral("test_value").ToExpression(),
			}).ToExpression(),
			expect: logs.EqualPredicate{
				Column: metadataColumn,
				Value:  scalar.NewBinaryScalar(memory.NewBufferBytes([]byte("test_value")), arrow.BinaryTypes.Binary),
			},
		},
		{
			name: "binary NEQ",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_NEQ,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "metadata").ToExpression(),
				Right: physical.NewLiteral("test_value").ToExpression(),
			}).ToExpression(),
			expect: logs.NotPredicate{
				Inner: logs.EqualPredicate{
					Column: metadataColumn,
					Value:  scalar.NewBinaryScalar(memory.NewBufferBytes([]byte("test_value")), arrow.BinaryTypes.Binary),
				},
			},
		},
		{
			name: "binary GT",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_GT,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "metadata").ToExpression(),
				Right: physical.NewLiteral(int64(100)).ToExpression(),
			}).ToExpression(),
			expect: logs.GreaterThanPredicate{
				Column: metadataColumn,
				Value:  scalar.NewInt64Scalar(100),
			},
		},
		{
			name: "binary GTE",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_GTE,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "metadata").ToExpression(),
				Right: physical.NewLiteral(int64(100)).ToExpression(),
			}).ToExpression(),
			expect: logs.OrPredicate{
				Left:  logs.GreaterThanPredicate{Column: metadataColumn, Value: scalar.NewInt64Scalar(100)},
				Right: logs.EqualPredicate{Column: metadataColumn, Value: scalar.NewInt64Scalar(100)},
			},
		},
		{
			name: "binary LT",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_LT,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "metadata").ToExpression(),
				Right: physical.NewLiteral(int64(100)).ToExpression(),
			}).ToExpression(),
			expect: logs.LessThanPredicate{
				Column: metadataColumn,
				Value:  scalar.NewInt64Scalar(100),
			},
		},
		{
			name: "binary LTE",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_LTE,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "metadata").ToExpression(),
				Right: physical.NewLiteral(int64(100)).ToExpression(),
			}).ToExpression(),
			expect: logs.OrPredicate{
				Left:  logs.LessThanPredicate{Column: metadataColumn, Value: scalar.NewInt64Scalar(100)},
				Right: logs.EqualPredicate{Column: metadataColumn, Value: scalar.NewInt64Scalar(100)},
			},
		},

		{
			name: "binary EQ (invalid column)",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_EQ,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "nonexistent").ToExpression(),
				Right: physical.NewLiteral("test_value").ToExpression(),
			}).ToExpression(),
			expect: logs.FalsePredicate{}, // non-null value can't equal NULL column
		},
		{
			name: "binary NEQ (invalid column)",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_NEQ,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "nonexistent").ToExpression(),
				Right: physical.NewLiteral("test_value").ToExpression(),
			}).ToExpression(),
			expect: logs.TruePredicate{}, // non-null value != NULL column
		},
		{
			name: "binary EQ NULL (invalid column)",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_EQ,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "nonexistent").ToExpression(),
				Right: physical.NewLiteral(nil).ToExpression(),
			}).ToExpression(),
			expect: logs.TruePredicate{}, // NULL == NULL: always passes
		},
		{
			name: "binary NEQ NULL (invalid column)",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_NEQ,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "nonexistent").ToExpression(),
				Right: physical.NewLiteral(nil).ToExpression(),
			}).ToExpression(),
			expect: logs.FalsePredicate{}, // NULL != NULL: always fails
		},
		{
			name: "binary GT (invalid column)",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_GT,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "nonexistent").ToExpression(),
				Right: physical.NewLiteral(int64(100)).ToExpression(),
			}).ToExpression(),
			expect: logs.FalsePredicate{}, // NULL > value always fails
		},
		{
			name: "binary GTE (invalid column)",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_GTE,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "nonexistent").ToExpression(),
				Right: physical.NewLiteral(int64(100)).ToExpression(),
			}).ToExpression(),
			expect: logs.FalsePredicate{}, // NULL >= value always fails
		},
		{
			name: "binary LT (invalid column)",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_LT,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "nonexistent").ToExpression(),
				Right: physical.NewLiteral(int64(100)).ToExpression(),
			}).ToExpression(),
			expect: logs.FalsePredicate{}, // NULL < value always fails
		},
		{
			name: "binary LTE (invalid column)",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_LTE,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "nonexistent").ToExpression(),
				Right: physical.NewLiteral(int64(100)).ToExpression(),
			}).ToExpression(),
			expect: logs.FalsePredicate{}, // NULL <= value always fails
		},
		{
			name: "binary MATCH_STR (invalid column)",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_MATCH_SUBSTR,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "nonexistent").ToExpression(),
				Right: physical.NewLiteral("substring").ToExpression(),
			}).ToExpression(),
			expect: logs.FalsePredicate{}, // match against non-existent column always fails
		},
		{
			name: "binary NOT_MATCH_STR (invalid column)",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_NOT_MATCH_SUBSTR,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "nonexistent").ToExpression(),
				Right: physical.NewLiteral("substring").ToExpression(),
			}).ToExpression(),
			expect: logs.TruePredicate{}, // not match against non-existent column always passes
		},
		{
			name: "binary MATCH_RE (invalid column)",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_MATCH_RE,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "nonexistent").ToExpression(),
				Right: physical.NewLiteral("^test.*").ToExpression(),
			}).ToExpression(),
			expect: logs.FalsePredicate{}, // match against non-existent column always fails
		},
		{
			name: "binary NOT_MATCH_RE (invalid column)",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_NOT_MATCH_RE,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "nonexistent").ToExpression(),
				Right: physical.NewLiteral("^test.*").ToExpression(),
			}).ToExpression(),
			expect: logs.TruePredicate{}, // not match against non-existent column always passes
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			if tc.expr.Kind == nil {
				t.Skip()
			}

			actual, err := buildLogsPredicate(tc.expr, columns)
			require.NoError(t, err)
			require.Equal(t, tc.expect, actual)
		})
	}
}

// Test_buildLogsPredicate_FuncPredicates tests cases of [buildLogsPredicate]
// which generate [logs.FuncPredicate], testing that the generated functions are
// correct.
func Test_buildLogsPredicate_FuncPredicates(t *testing.T) {
	var (
		metadataColumn = &logs.Column{Name: "metadata", Type: logs.ColumnTypeMetadata}
		columns        = []*logs.Column{metadataColumn}
	)

	type keepTest struct {
		input    scalar.Scalar
		expected bool
	}

	tt := []struct {
		name           string
		expr           physicalpb.Expression
		expectedColumn *logs.Column
		keepTests      []keepTest
	}{
		{
			name: "binary MATCH_STR",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_MATCH_SUBSTR,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "metadata").ToExpression(),
				Right: physical.NewLiteral("substring").ToExpression(),
			}).ToExpression(),
			expectedColumn: metadataColumn,
			keepTests: []keepTest{
				{
					input:    scalar.NewBinaryScalar(memory.NewBufferBytes([]byte("this contains substring here")), arrow.BinaryTypes.Binary),
					expected: true,
				},
				{
					input:    scalar.NewBinaryScalar(memory.NewBufferBytes([]byte("this does not contain it")), arrow.BinaryTypes.Binary),
					expected: false,
				},
				{
					input:    scalar.NewStringScalar("string contains substring"),
					expected: true,
				},
				{
					input:    scalar.NewStringScalar("string does not contain it"),
					expected: false,
				},
				{
					input:    scalar.MakeNullScalar(arrow.BinaryTypes.Binary),
					expected: false, // null binary can't match anything
				},
				{
					input:    scalar.MakeNullScalar(arrow.BinaryTypes.String),
					expected: false, // null string can't match anything
				},
			},
		},
		{
			name: "binary NOT_MATCH_STR",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_NOT_MATCH_SUBSTR,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "metadata").ToExpression(),
				Right: physical.NewLiteral("substring").ToExpression(),
			}).ToExpression(),
			expectedColumn: metadataColumn,
			keepTests: []keepTest{
				{
					input:    scalar.NewBinaryScalar(memory.NewBufferBytes([]byte("this contains substring here")), arrow.BinaryTypes.Binary),
					expected: false,
				},
				{
					input:    scalar.NewBinaryScalar(memory.NewBufferBytes([]byte("this does not contain it")), arrow.BinaryTypes.Binary),
					expected: true,
				},
				{
					input:    scalar.NewStringScalar("string contains substring"),
					expected: false,
				},
				{
					input:    scalar.NewStringScalar("string does not contain it"),
					expected: true,
				},
				{
					input:    scalar.MakeNullScalar(arrow.BinaryTypes.Binary),
					expected: true, // null binary doesn't match, so "not match" is true
				},
				{
					input:    scalar.MakeNullScalar(arrow.BinaryTypes.String),
					expected: true, // null string doesn't match, so "not match" is true
				},
			},
		},
		{
			name: "binary MATCH_RE",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_MATCH_RE,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "metadata").ToExpression(),
				Right: physical.NewLiteral("^test.*").ToExpression(),
			}).ToExpression(),
			expectedColumn: metadataColumn,
			keepTests: []keepTest{
				{
					input:    scalar.NewBinaryScalar(memory.NewBufferBytes([]byte("test string")), arrow.BinaryTypes.Binary),
					expected: true,
				},
				{
					input:    scalar.NewBinaryScalar(memory.NewBufferBytes([]byte("not matching")), arrow.BinaryTypes.Binary),
					expected: false,
				},
				{
					input:    scalar.NewStringScalar("test123"),
					expected: true,
				},
				{
					input:    scalar.NewStringScalar("no match"),
					expected: false,
				},
				{
					input:    scalar.MakeNullScalar(arrow.BinaryTypes.Binary),
					expected: false, // null binary can't match regex
				},
				{
					input:    scalar.MakeNullScalar(arrow.BinaryTypes.String),
					expected: false, // null string can't match regex
				},
			},
		},
		{
			name: "binary NOT_MATCH_RE",
			expr: *(&physicalpb.BinaryExpression{
				Op:    physicalpb.BINARY_OP_NOT_MATCH_RE,
				Left:  columnRef(physicalpb.COLUMN_TYPE_METADATA, "metadata").ToExpression(),
				Right: physical.NewLiteral("^test.*").ToExpression(),
			}).ToExpression(),
			expectedColumn: metadataColumn,
			keepTests: []keepTest{
				{
					input:    scalar.NewBinaryScalar(memory.NewBufferBytes([]byte("test string")), arrow.BinaryTypes.Binary),
					expected: false,
				},
				{
					input:    scalar.NewBinaryScalar(memory.NewBufferBytes([]byte("not matching")), arrow.BinaryTypes.Binary),
					expected: true,
				},
				{
					input:    scalar.NewStringScalar("test123"),
					expected: false,
				},
				{
					input:    scalar.NewStringScalar("no match"),
					expected: true,
				},
				{
					input:    scalar.MakeNullScalar(arrow.BinaryTypes.Binary),
					expected: true, // null binary doesn't match regex, so "not match" is true
				},
				{
					input:    scalar.MakeNullScalar(arrow.BinaryTypes.String),
					expected: true, // null string doesn't match regex, so "not match" is true
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := buildLogsPredicate(tc.expr, columns)
			require.NoError(t, err)

			// Verify it's a FuncPredicate
			funcPred, ok := actual.(logs.FuncPredicate)
			require.True(t, ok, "expected FuncPredicate, got %T", actual)

			// Verify the column is correct
			require.Equal(t, tc.expectedColumn, funcPred.Column)

			// Test the Keep function behavior
			for i, keepTest := range tc.keepTests {
				result := funcPred.Keep(nil, keepTest.input)
				require.Equal(t, keepTest.expected, result, "keepTest[%d] with input %v", i, keepTest.input)
			}
		})
	}
}

func columnRef(ty physicalpb.ColumnType, column string) *physicalpb.ColumnExpression {
	return &physicalpb.ColumnExpression{
		Type: ty,
		Name: column,
	}
}
