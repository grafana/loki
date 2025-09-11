package executor

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
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
	switch expr := expr.(type) {
	case physical.UnaryExpression:
		return buildLogsUnaryPredicate(expr, columns)

	case physical.BinaryExpression:
		return buildLogsBinaryPredicate(expr, columns)

	case *physical.LiteralExpr:
		if expr.Literal.Type() == datatype.Loki.Bool {
			val := expr.Literal.(datatype.TypedLiteral[bool]).Value()
			if val {
				return logs.TruePredicate{}, nil
			}
			return logs.FalsePredicate{}, nil
		}

	case *physical.ColumnExpr:
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
	unaryExpr, ok := expr.(*physical.UnaryExpr)
	if !ok {
		return nil, fmt.Errorf("expected physical.UnaryExpr, got %[1]T", expr)
	}

	inner, err := buildLogsPredicate(unaryExpr.Left, columns)
	if err != nil {
		return nil, fmt.Errorf("building unary predicate: %w", err)
	}

	switch unaryExpr.Op {
	case types.UnaryOpNot:
		return logs.NotPredicate{Inner: inner}, nil
	}

	return nil, fmt.Errorf("unsupported unary operator %s in logs predicate", unaryExpr.Op)
}

var comparisonBinaryOps = map[types.BinaryOp]struct{}{
	types.BinaryOpEq:              {},
	types.BinaryOpNeq:             {},
	types.BinaryOpGt:              {},
	types.BinaryOpGte:             {},
	types.BinaryOpLt:              {},
	types.BinaryOpLte:             {},
	types.BinaryOpMatchSubstr:     {},
	types.BinaryOpNotMatchSubstr:  {},
	types.BinaryOpMatchRe:         {},
	types.BinaryOpNotMatchRe:      {},
	types.BinaryOpMatchPattern:    {},
	types.BinaryOpNotMatchPattern: {},
}

func buildLogsBinaryPredicate(expr physical.BinaryExpression, columns []*logs.Column) (logs.Predicate, error) {
	binaryExpr, ok := expr.(*physical.BinaryExpr)
	if !ok {
		return nil, fmt.Errorf("expected physical.BinaryExpr, got %[1]T", expr)
	}

	switch binaryExpr.Op {
	case types.BinaryOpAnd:
		left, err := buildLogsPredicate(binaryExpr.Left, columns)
		if err != nil {
			return nil, fmt.Errorf("building left binary predicate: %w", err)
		}
		right, err := buildLogsPredicate(binaryExpr.Right, columns)
		if err != nil {
			return nil, fmt.Errorf("building right binary predicate: %w", err)
		}
		return logs.AndPredicate{Left: left, Right: right}, nil

	case types.BinaryOpOr:
		left, err := buildLogsPredicate(binaryExpr.Left, columns)
		if err != nil {
			return nil, fmt.Errorf("building left binary predicate: %w", err)
		}
		right, err := buildLogsPredicate(binaryExpr.Right, columns)
		if err != nil {
			return nil, fmt.Errorf("building right binary predicate: %w", err)
		}
		return logs.OrPredicate{Left: left, Right: right}, nil
	}

	if _, ok := comparisonBinaryOps[binaryExpr.Op]; ok {
		return buildLogsComparison(binaryExpr, columns)
	}

	return nil, fmt.Errorf("expression %[1]s (type %[1]T) cannot be interpreted as a boolean", expr)
}

func buildLogsComparison(expr *physical.BinaryExpr, columns []*logs.Column) (logs.Predicate, error) {
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

	columnRef, leftValid := expr.Left.(*physical.ColumnExpr)
	literalExpr, rightValid := expr.Right.(*physical.LiteralExpr)

	if !leftValid || !rightValid {
		return nil, fmt.Errorf("binary comparisons require the left-hand operation to reference a column (got %T) and the right-hand operation to be a literal (got %T)", expr.Left, expr.Right)
	}

	// findColumn may return nil for col if the referenced column doesn't exist;
	// this is handled in the switch statement below and converts to either
	// [logs.FalsePredicate] or [logs.TruePredicate] depending on the operation.
	col, err := findColumn(columnRef.Ref, columns)
	if err != nil {
		return nil, fmt.Errorf("finding column %s: %w", columnRef.Ref, err)
	}

	s, err := buildDataobjScalar(literalExpr.Literal)
	if err != nil {
		return nil, err
	}

	switch expr.Op {
	case types.BinaryOpEq:
		if col == nil && s.IsValid() {
			return logs.FalsePredicate{}, nil // Column(NULL) == non-null: always fails
		} else if col == nil && !s.IsValid() {
			return logs.TruePredicate{}, nil // Column(NULL) == NULL: always passes
		}
		return logs.EqualPredicate{Column: col, Value: s}, nil

	case types.BinaryOpNeq:
		if col == nil && s.IsValid() {
			return logs.TruePredicate{}, nil // Column(NULL) != non-null: always passes
		} else if col == nil && !s.IsValid() {
			return logs.FalsePredicate{}, nil // Column(NULL) != NULL: always fails
		}
		return logs.NotPredicate{Inner: logs.EqualPredicate{Column: col, Value: s}}, nil

	case types.BinaryOpGt:
		if col == nil {
			return logs.FalsePredicate{}, nil // Column(NULL) > value: always fails
		}
		return logs.GreaterThanPredicate{Column: col, Value: s}, nil

	case types.BinaryOpGte:
		if col == nil {
			return logs.FalsePredicate{}, nil // Column(NULL) >= value: always fails
		}
		return logs.OrPredicate{
			Left:  logs.GreaterThanPredicate{Column: col, Value: s},
			Right: logs.EqualPredicate{Column: col, Value: s},
		}, nil

	case types.BinaryOpLt:
		if col == nil {
			return logs.FalsePredicate{}, nil // Column(NULL) < value: always fails
		}
		return logs.LessThanPredicate{Column: col, Value: s}, nil

	case types.BinaryOpLte:
		if col == nil {
			return logs.FalsePredicate{}, nil // Column(NULL) <= value: always fails
		}
		return logs.OrPredicate{
			Left:  logs.LessThanPredicate{Column: col, Value: s},
			Right: logs.EqualPredicate{Column: col, Value: s},
		}, nil

	case types.BinaryOpMatchSubstr, types.BinaryOpMatchRe, types.BinaryOpMatchPattern:
		if col == nil {
			return logs.FalsePredicate{}, nil // Match operations against a non-existent column will always fail.
		}
		return buildLogsMatch(col, expr.Op, s)

	case types.BinaryOpNotMatchSubstr, types.BinaryOpNotMatchRe, types.BinaryOpNotMatchPattern:
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
func findColumn(ref types.ColumnRef, columns []*logs.Column) (*logs.Column, error) {
	if ref.Type != types.ColumnTypeBuiltin && ref.Type != types.ColumnTypeMetadata && ref.Type != types.ColumnTypeAmbiguous {
		return nil, fmt.Errorf("invalid column ref %s, expected builtin or metadata", ref)
	}

	columnMatch := func(ref types.ColumnRef, column *logs.Column) bool {
		switch {
		case ref.Type == types.ColumnTypeBuiltin && ref.Column == types.ColumnNameBuiltinTimestamp:
			return column.Type == logs.ColumnTypeTimestamp
		case ref.Type == types.ColumnTypeBuiltin && ref.Column == types.ColumnNameBuiltinMessage:
			return column.Type == logs.ColumnTypeMessage
		case ref.Type == types.ColumnTypeMetadata || ref.Type == types.ColumnTypeAmbiguous:
			return column.Name == ref.Column
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
// [datatype.Literal].
func buildDataobjScalar(lit datatype.Literal) (scalar.Scalar, error) {
	// [logs.ReaderOptions.Validate] specifies that all scalars must be one of
	// the given types:
	//
	// * [scalar.Null] (of any datatype)
	// * [scalar.Int64]
	// * [scalar.Uint64]
	// * [scalar.Timestamp] (nanosecond precision)
	// * [scalar.Binary]
	//
	// All of our mappings below evaluate to one of the above types.

	switch lit := lit.(type) {
	case datatype.NullLiteral:
		return scalar.ScalarNull, nil
	case datatype.IntegerLiteral:
		return scalar.NewInt64Scalar(lit.Value()), nil
	case datatype.BytesLiteral:
		// [datatype.BytesLiteral] refers to byte sizes, not binary data.
		return scalar.NewInt64Scalar(int64(lit.Value())), nil
	case datatype.TimestampLiteral:
		ts := arrow.Timestamp(lit.Value())
		tsType := arrow.FixedWidthTypes.Timestamp_ns
		return scalar.NewTimestampScalar(ts, tsType), nil
	case datatype.StringLiteral:
		buf := memory.NewBufferBytes([]byte(lit.Value()))
		return scalar.NewBinaryScalar(buf, arrow.BinaryTypes.Binary), nil
	}

	return nil, fmt.Errorf("unsupported literal type %T", lit)
}

func buildLogsMatch(col *logs.Column, op types.BinaryOp, value scalar.Scalar) (logs.Predicate, error) {
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
	case types.BinaryOpMatchSubstr:
		return logs.FuncPredicate{
			Column: col,
			Keep: func(_ *logs.Column, value scalar.Scalar) bool {
				return bytes.Contains(getBytes(value), find)
			},
		}, nil

	case types.BinaryOpNotMatchSubstr:
		return logs.FuncPredicate{
			Column: col,
			Keep: func(_ *logs.Column, value scalar.Scalar) bool {
				return !bytes.Contains(getBytes(value), find)
			},
		}, nil

	case types.BinaryOpMatchRe:
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

	case types.BinaryOpNotMatchRe:
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

	// NOTE(rfratto): [types.BinaryOpMatchPattern] and [types.BinaryOpNotMatchPattern]
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
