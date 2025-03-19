package logical

import (
	"fmt"
)

// ConvertToSSA converts a [TreeNode] into a [Plan].
func ConvertToSSA(node TreeNode) (*Plan, error) {
	// Initialize the builder with an empty node at index 0
	var builder ssaBuilder

	value, err := builder.processNode(node)
	if err != nil {
		return nil, fmt.Errorf("error converting plan to SSA: %w", err)
	}

	// Add the final Return instruction based on the last value.
	builder.instructions = append(builder.instructions, &Return{Value: value})

	return &Plan{Instructions: builder.instructions}, nil
}

// ssaBuilder is a helper type for building SSA forms
type ssaBuilder struct {
	instructions []Instruction
	nextID       int
}

func (b *ssaBuilder) getID() int {
	b.nextID++
	return b.nextID
}

// processPlan processes a logical plan and returns the resulting Value.
func (b *ssaBuilder) processNode(plan TreeNode) (Value, error) {
	switch plan.Type() {
	case TreeNodeTypeMakeTable:
		return b.processMakeTablePlan(plan.MakeTable())
	case TreeNodeTypeSelect:
		return b.processSelectPlan(plan.Select())
	case TreeNodeTypeLimit:
		return b.processLimitPlan(plan.Limit())
	case TreeNodeTypeSort:
		return b.processSortPlan(plan.Sort())
	default:
		return nil, fmt.Errorf("unsupported plan type: %v", plan.Type())
	}
}

func (b *ssaBuilder) processMakeTablePlan(plan *MakeTable) (Value, error) {
	plan.id = fmt.Sprintf("%%%d", b.getID())
	b.instructions = append(b.instructions, plan)
	return plan, nil
}

func (b *ssaBuilder) processSelectPlan(plan *Select) (Value, error) {
	// Process the child plan first
	if _, err := b.processNode(plan.Input); err != nil {
		return nil, err
	} else if _, err := b.processExpr(plan.Expr, plan.Input); err != nil {
		return nil, err
	}

	// Create a node for the select
	plan.id = fmt.Sprintf("%%%d", b.getID())
	b.instructions = append(b.instructions, plan)
	return plan, nil
}

// processExpr processes an expression and returns its ID
// It handles different expression types by delegating to specific processing methods
func (b *ssaBuilder) processExpr(expr Expr, parent TreeNode) (Value, error) {
	switch expr.Type() {
	case ExprTypeColumn:
		return b.processColumnExpr(expr.Column(), parent)
	case ExprTypeLiteral:
		return b.processLiteralExpr(expr.Literal())
	case ExprTypeBinaryOp:
		return b.processBinaryOpExpr(expr.BinaryOp(), parent)
	default:
		return nil, fmt.Errorf("unknown expression type: %v", expr.Type())
	}
}

// processColumnExpr processes a column expression
// It creates a ColumnRef node with the column name and type
func (b *ssaBuilder) processColumnExpr(expr *ColumnExpr, parent TreeNode) (Value, error) {
	return &ColumnRef{Column: expr.Name}, nil
}

// processLiteralExpr processes a literal expression
// It creates a Literal node with the value and type
func (b *ssaBuilder) processLiteralExpr(expr *LiteralExpr) (Value, error) {
	// TODO(rfratto): replace with just returning LiteralExpr
	return &ColumnRef{Column: "LITERAL"}, nil
}

// processBinaryOpExpr processes a binary operation expression
// It processes the left and right operands, then creates a BinaryOp node
func (b *ssaBuilder) processBinaryOpExpr(expr *BinOpExpr, parent TreeNode) (Value, error) {
	if _, err := b.processExpr(expr.Left, parent); err != nil {
		return nil, err
	} else if _, err := b.processExpr(expr.Right, parent); err != nil {
		return nil, err
	}

	// TODO(rfratto): replace with returning a BinOp node.
	return &ColumnRef{Column: "BINOP"}, nil
}

// processLimitPlan processes a limit plan and returns the ID of the resulting SSA node.
// This converts a Limit logical plan node to its SSA representation.
//
// The SSA node for a Limit plan has the following format:
//
//	%ID = Limit [Skip=X, Fetch=Y]
//
// Where X is the number of rows to skip and Y is the maximum number of rows to return.
// The Limit node always includes both Skip and Fetch properties, even when they are zero.
// This ensures consistent representation and allows for optimization in a separate step.
//
// The Limit node references its input plan as a dependency.
func (b *ssaBuilder) processLimitPlan(plan *Limit) (Value, error) {
	if _, err := b.processNode(plan.Input); err != nil {
		return nil, err
	}

	plan.id = fmt.Sprintf("%%%d", b.getID())
	b.instructions = append(b.instructions, plan)
	return plan, nil
}

// processSortPlan processes a sort plan and returns the ID of the resulting SSA node.
// This converts a Sort logical plan node to its SSA representation.
//
// The SSA node for a Sort plan has the following format:
//
//	%ID = Sort [expr=X, direction=Y, nulls=Z]
//
// Where X is the name of the sort expression, Y is the sort direction, and Z is the nulls position.
// The Sort node references its input plan and sort expression as dependencies.
func (b *ssaBuilder) processSortPlan(plan *Sort) (Value, error) {
	if _, err := b.processNode(plan.Input); err != nil {
		return nil, err
	}

	plan.id = fmt.Sprintf("%%%d", b.getID())
	b.instructions = append(b.instructions, plan)
	return plan, nil
}
