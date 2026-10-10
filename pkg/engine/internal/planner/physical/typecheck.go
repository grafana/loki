package physical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/functions"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// This file implements plan-time type checking of physical expressions.
//
// The executor evaluates expressions by looking up a function implementation
// for the combination of operation and operand data types (see the signature
// catalog in the [functions] package). When no implementation exists, the
// query fails at execution time, after it has already touched storage.
//
// Most operand types are statically known during physical planning, even
// though the schema of the scanned data is not: labels, structured metadata,
// and parsed values are always strings, builtin and generated columns have
// fixed types, and literals carry their type. This allows the same signature
// lookup to run at plan time and reject invalid expressions early.
//
// The check is conservative: whenever the data type of an operand cannot be
// determined statically, the expression is not checked and validation is
// deferred to the execution-time lookup, which remains in place.

// typeCheckPlan validates the expressions of all nodes in the plan against
// the function signature catalog. It only checks expressions that are
// evaluated by the executor's expression evaluator; scan predicates are
// excluded, because they are translated into storage-level predicates that
// support a different set of operations.
func typeCheckPlan(plan *Plan) error {
	for _, root := range plan.Roots() {
		if err := plan.DFSWalk(root, typeCheckNode, dag.PreOrderWalk); err != nil {
			return err
		}
	}
	return nil
}

// typeCheckNode validates the expressions of a single node.
func typeCheckNode(node Node) error {
	switch n := node.(type) {
	case *Filter:
		for _, predicate := range n.Predicates {
			dt, err := inferExpressionType(predicate)
			if err != nil {
				return fmt.Errorf("invalid predicate %s of node %s: %w", predicate, n.ID(), err)
			}
			if dt != nil && dt.ID() != types.BOOL {
				return fmt.Errorf("invalid predicate %s of node %s: predicate must evaluate to a boolean, got %v", predicate, n.ID(), dt)
			}
		}
	case *Projection:
		for _, expr := range n.Expressions {
			if _, err := inferExpressionType(expr); err != nil {
				return fmt.Errorf("invalid expression %s of node %s: %w", expr, n.ID(), err)
			}
		}
	case *RangeAggregation:
		for _, expr := range n.Grouping.Columns {
			if _, err := inferExpressionType(expr); err != nil {
				return fmt.Errorf("invalid grouping expression %s of node %s: %w", expr, n.ID(), err)
			}
		}
	case *VectorAggregation:
		for _, expr := range n.Grouping.Columns {
			if _, err := inferExpressionType(expr); err != nil {
				return fmt.Errorf("invalid grouping expression %s of node %s: %w", expr, n.ID(), err)
			}
		}
	}
	return nil
}

// inferExpressionType infers the data type of the value produced by the given
// expression and validates that every unary and binary operation in the
// expression tree has a function implementation for its operand types.
//
// It returns nil (without an error) when the type cannot be determined
// statically. In that case validation of the enclosing expression is skipped
// and deferred to the execution-time signature lookup.
func inferExpressionType(expr Expression) (types.DataType, error) {
	switch e := expr.(type) {
	case *LiteralExpr:
		return e.ValueType(), nil

	case *ColumnExpr:
		return columnDataType(e.Ref), nil

	case *UnaryExpr:
		operand, err := inferExpressionType(e.Left)
		if err != nil {
			return nil, err
		}
		if operand == nil {
			return nil, nil
		}
		ret, err := functions.UnaryReturnType(e.Op, operand)
		if err != nil {
			return nil, fmt.Errorf("failed to lookup unary function for signature %v(%v): %w", e.Op, operand, err)
		}
		return ret, nil

	case *BinaryExpr:
		lhs, err := inferExpressionType(e.Left)
		if err != nil {
			return nil, err
		}
		rhs, err := inferExpressionType(e.Right)
		if err != nil {
			return nil, err
		}
		if lhs == nil || rhs == nil {
			return nil, nil
		}
		ret, err := functions.BinaryReturnType(e.Op, lhs, rhs)
		if err != nil {
			return nil, fmt.Errorf("failed to lookup binary function for signature %v(%v,%v): %w", e.Op, lhs, rhs, err)
		}
		return ret, nil

	case *VariadicExpr:
		for _, arg := range e.Expressions {
			if _, err := inferExpressionType(arg); err != nil {
				return nil, err
			}
		}
		if !functions.HasVariadic(e.Op) {
			return nil, fmt.Errorf("failed to lookup variadic function %v", e.Op)
		}
		// Variadic functions produce a set of new columns rather than a
		// single value, so they have no scalar result type.
		return nil, nil
	}

	return nil, nil
}

// columnDataType returns the data type of the column referenced by ref, or
// nil if the type cannot be determined statically.
func columnDataType(ref types.ColumnRef) types.DataType {
	switch ref.Type {
	case types.ColumnTypeLabel, types.ColumnTypeMetadata, types.ColumnTypeParsed:
		// Labels, structured metadata, and values extracted by parser stages
		// are always strings.
		return types.Loki.String

	case types.ColumnTypeAmbiguous:
		// An ambiguous reference resolves to a label, metadata, or parsed
		// column, all of which are strings. A reference to a non-existent
		// column evaluates to the empty string.
		return types.Loki.String

	case types.ColumnTypeBuiltin:
		switch ref.Column {
		case types.ColumnNameBuiltinTimestamp:
			return types.Loki.Timestamp
		case types.ColumnNameBuiltinMessage:
			return types.Loki.String
		}

	case types.ColumnTypeGenerated:
		switch ref.Column {
		case types.ColumnNameGeneratedValue:
			return types.Loki.Float
		case types.ColumnNameError, types.ColumnNameErrorDetails:
			return types.Loki.String
		}
	}

	// The type of other columns (e.g. generated columns produced by
	// expressions elsewhere in the plan) cannot be determined without schema
	// propagation, so they are not checked.
	return nil
}
