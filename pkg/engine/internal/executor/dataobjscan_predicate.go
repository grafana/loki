package executor

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// buildLogsPredicate builds a [logs.Predicate] from an expr. The columns slice
// determines available columns that can be referenced by the expression.
//
// The returned predicate performs no filtering on stream ID; callers must use
// [logs.AndPredicate] and manually include filters for stream IDs if desired.
//
// References to columns that do not exist in the columns slice map to a
// [logs.FalsePredicate].
//
// buildLogsPredicate returns an error if:
//
//   - Expressions cannot be represented as a boolean value
//   - An expression is not supported (such as comparing two columns or comparing
//     two literals).
func buildLogsPredicate(expr physical.Expression, columns []*logs.Column) (logs.Predicate, error) {
	switch expr := expr.Kind.(type) {
	case *physical.Expression_UnaryExpression:
		return buildLogsUnaryPredicate(*expr.UnaryExpression, columns)

	case *physical.Expression_BinaryExpression:
		return buildLogsBinaryPredicate(*expr.BinaryExpression, columns)

	case *physical.Expression_LiteralExpression:
		switch (expr.LiteralExpression.Kind).(type) {
		case *physical.LiteralExpression_BoolLiteral:
			val := expr.LiteralExpression.GetBoolLiteral().Value
			if val {
				return logs.TruePredicate{}, nil
			}
			return logs.FalsePredicate{}, nil
		}

	case *physical.Expression_ColumnExpression:
		// TODO(rfratto): This would add support for statements like
		//
		// SELECT * WHERE boolean_column
		//
		// (where boolean_column is a column containing boolean values). No such
		// column type exists now, so I'm leaving it unsupported here for the time
		// being.
		return nil, fmt.Errorf("plain column references (%s) are unsupported for logs predicates", expr)

	default:
		// TODO(rfratto): This doesn't yet cover two potential future cases:
		//
		// * A plain boolean literal
		// * A reference to a column containing boolean values
		return nil, fmt.Errorf("expression %[1]s (type %[1]T) cannot be interpreted as a boolean", expr)
	}

	return nil, fmt.Errorf("expression %[1]s (type %[1]T) cannot be interpreted as a boolean", expr)
}

func buildLogsUnaryPredicate(expr physical.UnaryExpression, columns []*logs.Column) (logs.Predicate, error) {
	inner, err := buildLogsPredicate(*expr.Value, columns)
	if err != nil {
		return nil, fmt.Errorf("building unary predicate: %w", err)
	}

	switch expr.Op {
	case physical.UNARY_OP_NOT:
		return logs.NotPredicate{Inner: inner}, nil
	}

	return nil, fmt.Errorf("unsupported unary operator %s in logs predicate", expr.Op)
}

var comparisonBinaryOps = map[physical.BinaryOp]struct{}{
	physical.BINARY_OP_EQ:                {},
	physical.BINARY_OP_NEQ:               {},
	physical.BINARY_OP_GT:                {},
	physical.BINARY_OP_GTE:               {},
	physical.BINARY_OP_LT:                {},
	physical.BINARY_OP_LTE:               {},
	physical.BINARY_OP_MATCH_SUBSTR:      {},
	physical.BINARY_OP_NOT_MATCH_SUBSTR:  {},
	physical.BINARY_OP_MATCH_RE:          {},
	physical.BINARY_OP_NOT_MATCH_RE:      {},
	physical.BINARY_OP_MATCH_PATTERN:     {},
	physical.BINARY_OP_NOT_MATCH_PATTERN: {},
}

func buildLogsBinaryPredicate(expr physical.BinaryExpression, columns []*logs.Column) (logs.Predicate, error) {
	switch expr.Op {
	case physical.BINARY_OP_AND:
		left, err := buildLogsPredicate(*expr.Left, columns)
		if err != nil {
			return nil, fmt.Errorf("building left binary predicate: %w", err)
		}
		right, err := buildLogsPredicate(*expr.Right, columns)
		if err != nil {
			return nil, fmt.Errorf("building right binary predicate: %w", err)
		}
		return logs.AndPredicate{Left: left, Right: right}, nil

	case physical.BINARY_OP_OR:
		left, err := buildLogsPredicate(*expr.Left, columns)
		if err != nil {
			return nil, fmt.Errorf("building left binary predicate: %w", err)
		}
		right, err := buildLogsPredicate(*expr.Right, columns)
		if err != nil {
			return nil, fmt.Errorf("building right binary predicate: %w", err)
		}
		return logs.OrPredicate{Left: left, Right: right}, nil
	}

	if _, ok := comparisonBinaryOps[expr.Op]; ok {
		return buildLogsComparison(&expr, columns)
	}

	return nil, fmt.Errorf("expression %[1]s (type %[1]T) cannot be interpreted as a boolean", expr)
}

func buildLogsComparison(expr *physical.BinaryExpression, columns []*logs.Column) (logs.Predicate, error) {
	// Currently, we only support comparisons where the left-hand side is a
	// [physical.ColumnExpr] and the right-hand side is a [physical.LiteralExpr].
	//
	// Support for other cases could be added in the future:
	//
	// * LHS LiteralExpr, RHS ColumnExpr could be supported by inverting the
	//   operation.
	// * LHS LiteralExpr, RHS LiteralExpr could be supported by evaluating the
	//   expresion immediately.
	// * LHS ColumnExpr, RHS ColumnExpr could be supported in the future, but would need
	//   support down to the dataset level.

	columnRef, leftValid := expr.Left.Kind.(*physical.Expression_ColumnExpression)
	literalExpr, rightValid := expr.Right.Kind.(*physical.Expression_LiteralExpression)

	if !leftValid || !rightValid {
		return nil, fmt.Errorf("binary comparisons require the left-hand operation to reference a column (got %T) and the right-hand operation to be a literal (got %T)", expr.Left, expr.Right)
	}

	// findColumn may return nil for col if the referenced column doesn't exist;
	// this is handled in the switch statement below and converts to either
	// [logs.FalsePredicate] or [logs.TruePredicate] depending on the operation.
	col, err := findColumn(*columnRef.ColumnExpression, columns)
	if err != nil {
		return nil, fmt.Errorf("finding column %s: %w", columnRef.ColumnExpression, err)
	}

	s, err := buildDataobjScalar(*literalExpr.LiteralExpression)
	if err != nil {
		return nil, err
	}

	switch expr.Op {
	case physical.BINARY_OP_EQ:
		if col == nil && s.IsValid() {
			return logs.FalsePredicate{}, nil // Column(NULL) == non-null: always fails
		} else if col == nil && !s.IsValid() {
			return logs.TruePredicate{}, nil // Column(NULL) == NULL: always passes
		}
		return logs.EqualPredicate{Column: col, Value: s}, nil

	case physical.BINARY_OP_NEQ:
		if col == nil && s.IsValid() {
			return logs.TruePredicate{}, nil // Column(NULL) != non-null: always passes
		} else if col == nil && !s.IsValid() {
			return logs.FalsePredicate{}, nil // Column(NULL) != NULL: always fails
		}
		return logs.NotPredicate{Inner: logs.EqualPredicate{Column: col, Value: s}}, nil

	case physical.BINARY_OP_GT:
		if col == nil {
			return logs.FalsePredicate{}, nil // Column(NULL) > value: always fails
		}
		return logs.GreaterThanPredicate{Column: col, Value: s}, nil

	case physical.BINARY_OP_GTE:
		if col == nil {
			return logs.FalsePredicate{}, nil // Column(NULL) >= value: always fails
		}
		return logs.OrPredicate{
			Left:  logs.GreaterThanPredicate{Column: col, Value: s},
			Right: logs.EqualPredicate{Column: col, Value: s},
		}, nil

	case physical.BINARY_OP_LT:
		if col == nil {
			return logs.FalsePredicate{}, nil // Column(NULL) < value: always fails
		}
		return logs.LessThanPredicate{Column: col, Value: s}, nil

	case physical.BINARY_OP_LTE:
		if col == nil {
			return logs.FalsePredicate{}, nil // Column(NULL) <= value: always fails
		}
		return logs.OrPredicate{
			Left:  logs.LessThanPredicate{Column: col, Value: s},
			Right: logs.EqualPredicate{Column: col, Value: s},
		}, nil

	case physical.BINARY_OP_MATCH_SUBSTR, physical.BINARY_OP_MATCH_RE, physical.BINARY_OP_MATCH_PATTERN:
		if col == nil {
			return logs.FalsePredicate{}, nil // Match operations against a non-existent column will always fail.
		}
		return buildLogsMatch(col, expr.Op, s)

	case physical.BINARY_OP_NOT_MATCH_SUBSTR, physical.BINARY_OP_NOT_MATCH_RE, physical.BINARY_OP_NOT_MATCH_PATTERN:
		if col == nil {
			return logs.TruePredicate{}, nil // Not match operations against a non-existent column will always pass.
		}
		return buildLogsMatch(col, expr.Op, s)
	}

	return nil, fmt.Errorf("unsupported binary operator %s in logs predicate", expr.Op)
}

// findColumn finds a column by ref in the slice of columns. If ref is invalid,
// findColumn returns an error. If the column does not exist, findColumn
// returns nil.
func findColumn(ref physical.ColumnExpression, columns []*logs.Column) (*logs.Column, error) {
	if ref.Type != physical.COLUMN_TYPE_BUILTIN && ref.Type != physical.COLUMN_TYPE_METADATA && ref.Type != physical.COLUMN_TYPE_AMBIGUOUS {
		return nil, fmt.Errorf("invalid column ref %s, expected builtin or metadata", ref)
	}

	columnMatch := func(ref physical.ColumnExpression, column *logs.Column) bool {
		switch {
		case ref.Type == physical.COLUMN_TYPE_BUILTIN && ref.Name == types.ColumnNameBuiltinTimestamp:
			return column.Type == logs.ColumnTypeTimestamp
		case ref.Type == physical.COLUMN_TYPE_BUILTIN && ref.Name == types.ColumnNameBuiltinMessage:
			return column.Type == logs.ColumnTypeMessage
		case ref.Type == physical.COLUMN_TYPE_METADATA || ref.Type == physical.COLUMN_TYPE_AMBIGUOUS:
			return column.Name == ref.Name
		}

		return false
	}

	for _, column := range columns {
		if columnMatch(ref, column) {
			return column, nil
		}
	}

	return nil, nil
}

// buildDataobjScalar builds a dataobj-compatible [scalar.Scalar] from a
// [types.Literal].
func buildDataobjScalar(lit physical.LiteralExpression) (scalar.Scalar, error) {
	// [logs.ReaderOptions.Validate] specifies that all scalars must be one of
	// the given types:
	//
	// * [scalar.Null] (of any types)
	// * [scalar.Int64]
	// * [scalar.Uint64]
	// * [scalar.Timestamp] (nanosecond precision)
	// * [scalar.Binary]
	//
	// All of our mappings below evaluate to one of the above types.

	switch lit := lit.Kind.(type) {
	case *physical.LiteralExpression_NullLiteral:
		return scalar.ScalarNull, nil
	case *physical.LiteralExpression_IntegerLiteral:
		return scalar.NewInt64Scalar(lit.IntegerLiteral.Value), nil
	case *physical.LiteralExpression_BytesLiteral:
		// [types.BytesLiteral] refers to byte sizes, not binary data.
		return scalar.NewInt64Scalar(lit.BytesLiteral.Value), nil
	case *physical.LiteralExpression_TimestampLiteral:
		ts := arrow.Timestamp(lit.TimestampLiteral.Value)
		tsType := arrow.FixedWidthTypes.Timestamp_ns
		return scalar.NewTimestampScalar(ts, tsType), nil
	case *physical.LiteralExpression_StringLiteral:
		buf := memory.NewBufferBytes([]byte(lit.StringLiteral.Value))
		return scalar.NewBinaryScalar(buf, arrow.BinaryTypes.Binary), nil
	}

	return nil, fmt.Errorf("unsupported literal type %T", lit)
}

func buildLogsMatch(col *logs.Column, op physical.BinaryOp, value scalar.Scalar) (logs.Predicate, error) {
	// All the match operations require the value to be a string or a binary.
	var find []byte

	switch value := value.(type) {
	case *scalar.Binary:
		find = value.Data()
	case *scalar.String:
		find = value.Data()
	default:
		return nil, fmt.Errorf("unsupported scalar type %T for op %s, expected binary or string", value, op)
	}

	switch op {
	case physical.BINARY_OP_MATCH_SUBSTR:
		return logs.FuncPredicate{
			Column: col,
			Keep: func(_ *logs.Column, value scalar.Scalar) bool {
				return bytes.Contains(getBytes(value), find)
			},
		}, nil

	case physical.BINARY_OP_NOT_MATCH_SUBSTR:
		return logs.FuncPredicate{
			Column: col,
			Keep: func(_ *logs.Column, value scalar.Scalar) bool {
				return !bytes.Contains(getBytes(value), find)
			},
		}, nil

	case physical.BINARY_OP_MATCH_RE:
		re, err := regexp.Compile(string(find))
		if err != nil {
			return nil, err
		}
		return logs.FuncPredicate{
			Column: col,
			Keep: func(_ *logs.Column, value scalar.Scalar) bool {
				return re.Match(getBytes(value))
			},
		}, nil

	case physical.BINARY_OP_NOT_MATCH_RE:
		re, err := regexp.Compile(string(find))
		if err != nil {
			return nil, err
		}
		return logs.FuncPredicate{
			Column: col,
			Keep: func(_ *logs.Column, value scalar.Scalar) bool {
				return !re.Match(getBytes(value))
			},
		}, nil
	}

	// NOTE(rfratto): [physical.BINARY_OP_MATCH_PATTERN] and [physical.BINARY_OP_NOT_MATCH_PATTERN]
	// are currently unsupported.
	return nil, fmt.Errorf("unrecognized match operation %s", op)
}

func getBytes(value scalar.Scalar) []byte {
	if !value.IsValid() {
		return nil
	}

	switch value := value.(type) {
	case *scalar.Binary:
		return value.Data()
	case *scalar.String:
		return value.Data()
	}

	return nil
}
