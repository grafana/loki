package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
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
		expr   physical.Expression
		expect logs.Predicate
	}{
		{
			name:   "literal true",
			expr:   physical.NewLiteral(true),
			expect: logs.TruePredicate{},
		},
		{
			name:   "literal false",
			expr:   physical.NewLiteral(false),
			expect: logs.FalsePredicate{},
		},

		{
			name: "unary NOT",
			expr: &physical.UnaryExpr{
				Op:   types.UnaryOpNot,
				Left: physical.NewLiteral(false),
			},
			expect: logs.NotPredicate{
				Inner: logs.FalsePredicate{},
			},
		},

		{
			name: "binary AND",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpAnd,
				Left:  physical.NewLiteral(true),
				Right: physical.NewLiteral(false),
			},
			expect: logs.AndPredicate{
				Left:  logs.TruePredicate{},
				Right: logs.FalsePredicate{},
			},
		},
		{
			name: "binary OR",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpOr,
				Left:  physical.NewLiteral(true),
				Right: physical.NewLiteral(false),
			},
			expect: logs.OrPredicate{
				Left:  logs.TruePredicate{},
				Right: logs.FalsePredicate{},
			},
		},

		{
			name: "builtin timestamp reference",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpEq,
				Left:  columnRef(types.ColumnTypeBuiltin, types.ColumnNameBuiltinTimestamp),
				Right: physical.NewLiteral(int64(1234567890)),
			},
			expect: logs.EqualPredicate{
				Column: timestampColumn,
				Value:  scalar.NewInt64Scalar(1234567890),
			},
		},
		{
			name: "builtin message reference",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpEq,
				Left:  columnRef(types.ColumnTypeBuiltin, types.ColumnNameBuiltinMessage),
				Right: physical.NewLiteral(int64(9876543210)),
			},
			expect: logs.EqualPredicate{
				Column: messageColumn,
				Value:  scalar.NewInt64Scalar(9876543210),
			},
		},
		{
			name: "metadata reference",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpEq,
				Left:  columnRef(types.ColumnTypeMetadata, "metadata"),
				Right: physical.NewLiteral(int64(5555555555)),
			},
			expect: logs.EqualPredicate{
				Column: metadataColumn,
				Value:  scalar.NewInt64Scalar(5555555555),
			},
		},
		{
			name: "ambiguous metadata reference",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpEq,
				Left:  columnRef(types.ColumnTypeAmbiguous, "metadata"),
				Right: physical.NewLiteral(int64(7777777777)),
			},
			expect: logs.EqualPredicate{
				Column: metadataColumn,
				Value:  scalar.NewInt64Scalar(7777777777),
			},
		},

		{
			name: "check column for null literal",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpEq,
				Left:  columnRef(types.ColumnTypeBuiltin, types.ColumnNameBuiltinTimestamp),
				Right: physical.NewLiteral(nil),
			},
			expect: logs.EqualPredicate{
				Column: timestampColumn,
				Value:  scalar.ScalarNull,
			},
		},
		{
			name: "check column for integer literal",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpEq,
				Left:  columnRef(types.ColumnTypeBuiltin, types.ColumnNameBuiltinTimestamp),
				Right: physical.NewLiteral(int64(42)),
			},
			expect: logs.EqualPredicate{
				Column: timestampColumn,
				Value:  scalar.NewInt64Scalar(42),
			},
		},
		{
			name: "check column for bytes literal",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpEq,
				Left:  columnRef(types.ColumnTypeBuiltin, types.ColumnNameBuiltinTimestamp),
				Right: physical.NewLiteral(types.Bytes(1024)),
			},
			expect: logs.EqualPredicate{
				Column: timestampColumn,
				Value:  scalar.NewInt64Scalar(1024),
			},
		},
		{
			name: "check column for timestamp literal",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpEq,
				Left:  columnRef(types.ColumnTypeBuiltin, types.ColumnNameBuiltinTimestamp),
				Right: physical.NewLiteral(types.Timestamp(1609459200000000000)), // 2021-01-01 00:00:00 UTC in nanoseconds
			},
			expect: logs.EqualPredicate{
				Column: timestampColumn,
				Value:  scalar.NewTimestampScalar(arrow.Timestamp(1609459200000000000), arrow.FixedWidthTypes.Timestamp_ns),
			},
		},
		{
			name: "check column for string literal",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpEq,
				Left:  columnRef(types.ColumnTypeBuiltin, types.ColumnNameBuiltinTimestamp),
				Right: physical.NewLiteral("hello world"),
			},
			expect: logs.EqualPredicate{
				Column: timestampColumn,
				Value:  scalar.NewBinaryScalar(memory.NewBufferBytes([]byte("hello world")), arrow.BinaryTypes.Binary),
			},
		},

		{
			name: "binary EQ",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpEq,
				Left:  columnRef(types.ColumnTypeMetadata, "metadata"),
				Right: physical.NewLiteral("test_value"),
			},
			expect: logs.EqualPredicate{
				Column: metadataColumn,
				Value:  scalar.NewBinaryScalar(memory.NewBufferBytes([]byte("test_value")), arrow.BinaryTypes.Binary),
			},
		},
		{
			name: "binary NEQ",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpNeq,
				Left:  columnRef(types.ColumnTypeMetadata, "metadata"),
				Right: physical.NewLiteral("test_value"),
			},
			expect: logs.NotPredicate{
				Inner: logs.EqualPredicate{
					Column: metadataColumn,
					Value:  scalar.NewBinaryScalar(memory.NewBufferBytes([]byte("test_value")), arrow.BinaryTypes.Binary),
				},
			},
		},
		{
			name: "binary GT",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpGt,
				Left:  columnRef(types.ColumnTypeMetadata, "metadata"),
				Right: physical.NewLiteral(int64(100)),
			},
			expect: logs.GreaterThanPredicate{
				Column: metadataColumn,
				Value:  scalar.NewInt64Scalar(100),
			},
		},
		{
			name: "binary GTE",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpGte,
				Left:  columnRef(types.ColumnTypeMetadata, "metadata"),
				Right: physical.NewLiteral(int64(100)),
			},
			expect: logs.OrPredicate{
				Left:  logs.GreaterThanPredicate{Column: metadataColumn, Value: scalar.NewInt64Scalar(100)},
				Right: logs.EqualPredicate{Column: metadataColumn, Value: scalar.NewInt64Scalar(100)},
			},
		},
		{
			name: "binary LT",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpLt,
				Left:  columnRef(types.ColumnTypeMetadata, "metadata"),
				Right: physical.NewLiteral(int64(100)),
			},
			expect: logs.LessThanPredicate{
				Column: metadataColumn,
				Value:  scalar.NewInt64Scalar(100),
			},
		},
		{
			name: "binary LTE",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpLte,
				Left:  columnRef(types.ColumnTypeMetadata, "metadata"),
				Right: physical.NewLiteral(int64(100)),
			},
			expect: logs.OrPredicate{
				Left:  logs.LessThanPredicate{Column: metadataColumn, Value: scalar.NewInt64Scalar(100)},
				Right: logs.EqualPredicate{Column: metadataColumn, Value: scalar.NewInt64Scalar(100)},
			},
		},

		{
			name: "binary EQ (invalid column)",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpEq,
				Left:  columnRef(types.ColumnTypeMetadata, "nonexistent"),
				Right: physical.NewLiteral("test_value"),
			},
			expect: logs.FalsePredicate{}, // non-null value can't equal NULL column
		},
		{
			name: "binary NEQ (invalid column)",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpNeq,
				Left:  columnRef(types.ColumnTypeMetadata, "nonexistent"),
				Right: physical.NewLiteral("test_value"),
			},
			expect: logs.TruePredicate{}, // non-null value != NULL column
		},
		{
			name: "binary EQ NULL (invalid column)",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpEq,
				Left:  columnRef(types.ColumnTypeMetadata, "nonexistent"),
				Right: physical.NewLiteral(nil),
			},
			expect: logs.TruePredicate{}, // NULL == NULL: always passes
		},
		{
			name: "binary NEQ NULL (invalid column)",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpNeq,
				Left:  columnRef(types.ColumnTypeMetadata, "nonexistent"),
				Right: physical.NewLiteral(nil),
			},
			expect: logs.FalsePredicate{}, // NULL != NULL: always fails
		},
		{
			name: "binary GT (invalid column)",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpGt,
				Left:  columnRef(types.ColumnTypeMetadata, "nonexistent"),
				Right: physical.NewLiteral(int64(100)),
			},
			expect: logs.FalsePredicate{}, // NULL > value always fails
		},
		{
			name: "binary GTE (invalid column)",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpGte,
				Left:  columnRef(types.ColumnTypeMetadata, "nonexistent"),
				Right: physical.NewLiteral(int64(100)),
			},
			expect: logs.FalsePredicate{}, // NULL >= value always fails
		},
		{
			name: "binary LT (invalid column)",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpLt,
				Left:  columnRef(types.ColumnTypeMetadata, "nonexistent"),
				Right: physical.NewLiteral(int64(100)),
			},
			expect: logs.FalsePredicate{}, // NULL < value always fails
		},
		{
			name: "binary LTE (invalid column)",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpLte,
				Left:  columnRef(types.ColumnTypeMetadata, "nonexistent"),
				Right: physical.NewLiteral(int64(100)),
			},
			expect: logs.FalsePredicate{}, // NULL <= value always fails
		},
		{
			name: "binary MATCH_STR (invalid column)",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpMatchSubstr,
				Left:  columnRef(types.ColumnTypeMetadata, "nonexistent"),
				Right: physical.NewLiteral("substring"),
			},
			expect: logs.FalsePredicate{}, // match against non-existent column always fails
		},
		{
			name: "binary NOT_MATCH_STR (invalid column)",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpNotMatchSubstr,
				Left:  columnRef(types.ColumnTypeMetadata, "nonexistent"),
				Right: physical.NewLiteral("substring"),
			},
			expect: logs.TruePredicate{}, // not match against non-existent column always passes
		},
		{
			name: "binary MATCH_RE (invalid column)",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpMatchRe,
				Left:  columnRef(types.ColumnTypeMetadata, "nonexistent"),
				Right: physical.NewLiteral("^test.*"),
			},
			expect: logs.FalsePredicate{}, // match against non-existent column always fails
		},
		{
			name: "binary NOT_MATCH_RE (invalid column)",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpNotMatchRe,
				Left:  columnRef(types.ColumnTypeMetadata, "nonexistent"),
				Right: physical.NewLiteral("^test.*"),
			},
			expect: logs.TruePredicate{}, // not match against non-existent column always passes
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			if tc.expr == nil {
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
		expr           physical.Expression
		expectedColumn *logs.Column
		keepTests      []keepTest
	}{
		{
			name: "binary MATCH_STR",
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpMatchSubstr,
				Left:  columnRef(types.ColumnTypeMetadata, "metadata"),
				Right: physical.NewLiteral("substring"),
			},
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
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpNotMatchSubstr,
				Left:  columnRef(types.ColumnTypeMetadata, "metadata"),
				Right: physical.NewLiteral("substring"),
			},
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
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpMatchRe,
				Left:  columnRef(types.ColumnTypeMetadata, "metadata"),
				Right: physical.NewLiteral("^test.*"),
			},
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
			expr: &physical.BinaryExpr{
				Op:    types.BinaryOpNotMatchRe,
				Left:  columnRef(types.ColumnTypeMetadata, "metadata"),
				Right: physical.NewLiteral("^test.*"),
			},
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

func columnRef(ty types.ColumnType, column string) *physical.ColumnExpr {
	return &physical.ColumnExpr{
		Ref: types.ColumnRef{
			Type:   ty,
			Column: column,
		},
	}
}

func Test_buildLogsPredicate_CaseInsensitive_Substring(t *testing.T) {
	messageColumn := &logs.Column{Type: logs.ColumnTypeMessage}
	columns := []*logs.Column{messageColumn}

	// Build a predicate for case-insensitive substring match
	expr := &physical.BinaryExpr{
		Op: types.BinaryOpMatchSubstrCaseInsensitive,
		Left: &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Type:   types.ColumnTypeBuiltin,
				Column: types.ColumnNameBuiltinMessage,
			},
		},
		Right: physical.NewLiteral("DURATION"), // Uppercased pattern
	}

	predicate, err := buildLogsPredicate(expr, columns)
	require.NoError(t, err, "buildLogsPredicate should support case-insensitive operators")

	funcPred, ok := predicate.(logs.FuncPredicate)
	require.True(t, ok, "expected logs.FuncPredicate")

	// Test that it matches various cases
	testCases := []struct {
		name     string
		input    string
		expected bool
	}{
		{"lowercase", "duration", true},
		{"uppercase", "DURATION", true},
		{"mixed case", "Duration", true},
		{"mixed case 2", "DuRaTiOn", true},
		{"in sentence lowercase", "request duration was 100ms", true},
		{"in sentence uppercase", "REQUEST DURATION WAS 100MS", true},
		{"in sentence mixed", "Request Duration was 100ms", true},
		{"no match", "timeout occurred", false},
		{"partial match", "dura", false}, // "DURATION" is not in "dura"
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			buf := memory.NewBufferBytes([]byte(tc.input))
			inputScalar := scalar.NewBinaryScalar(buf, arrow.BinaryTypes.Binary)

			result := funcPred.Keep(messageColumn, inputScalar)
			require.Equal(t, tc.expected, result,
				"case-insensitive match should correctly match %q", tc.input)
		})
	}
}

func Test_buildLogsPredicate_CaseInsensitive_Equality(t *testing.T) {
	messageColumn := &logs.Column{Type: logs.ColumnTypeMessage}
	columns := []*logs.Column{messageColumn}

	// Build a predicate for case-insensitive equality
	expr := &physical.BinaryExpr{
		Op: types.BinaryOpEqCaseInsensitive,
		Left: &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Type:   types.ColumnTypeBuiltin,
				Column: types.ColumnNameBuiltinMessage,
			},
		},
		Right: physical.NewLiteral("ERROR"), // Uppercased pattern
	}

	predicate, err := buildLogsPredicate(expr, columns)
	require.NoError(t, err, "buildLogsPredicate should support case-insensitive operators")

	funcPred, ok := predicate.(logs.FuncPredicate)
	require.True(t, ok, "expected logs.FuncPredicate")

	// Test exact equality with various cases
	testCases := []struct {
		name     string
		input    string
		expected bool
	}{
		{"exact lowercase", "error", true},
		{"exact uppercase", "ERROR", true},
		{"exact mixed case", "Error", true},
		{"exact mixed case 2", "ErRoR", true},
		{"substring should not match", "error occurred", false},
		{"prefix should not match", "error!", false},
		{"different word", "warning", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			buf := memory.NewBufferBytes([]byte(tc.input))
			inputScalar := scalar.NewBinaryScalar(buf, arrow.BinaryTypes.Binary)

			result := funcPred.Keep(messageColumn, inputScalar)
			require.Equal(t, tc.expected, result,
				"case-insensitive equality should correctly match %q", tc.input)
		})
	}
}
