// Package logical provides a logical query plan representation for data processing operations.
// It defines a type system for expressions and plan nodes that can be used to build and
// manipulate query plans in a structured way.
package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// TreeNodeType is an enum representing the type of plan node. It allows
// consumers to determine the concrete type of a Plan and safely cast to the
// appropriate interface.
type TreeNodeType int

const (
	TreeNodeTypeInvalid   TreeNodeType = iota // Invalid or uninitialized plan
	TreeNodeTypeMakeTable                     // Represents an operation to make a table relation.
	TreeNodeTypeSelect                        // Represents a Select operation
	TreeNodeTypeLimit                         // Represents a limit operation
	TreeNodeTypeSort                          // Represents a sort operation
)

// String returns a string representation of the plan type.
// This is useful for debugging and error messages.
func (t TreeNodeType) String() string {
	switch t {
	case TreeNodeTypeInvalid:
		return "Invalid"
	case TreeNodeTypeMakeTable:
		return "MakeTable"
	case TreeNodeTypeSelect:
		return "Select"
	case TreeNodeTypeLimit:
		return "Limit"
	case TreeNodeTypeSort:
		return "Sort"
	default:
		return "Unknown"
	}
}

// TreeNode is a wrapper around plan node types (MakeTable, Select, etc.), and
// provides methods to safely access the underlying value.
//
// This approach replaces the previous interface-based design to reduce
// indirection and improve code clarity.
type TreeNode struct {
	ty  TreeNodeType // The type of plan
	val any          // The concrete node value
}

// Type returns the type of the plan.
// This allows consumers to determine the concrete type of the plan
// and safely cast to the appropriate interface.
func (p TreeNode) Type() TreeNodeType {
	return p.ty
}

// Schema returns the schema of the data produced by this plan node.
// It delegates to the appropriate concrete plan type based on the plan type.
func (p TreeNode) Schema() schema.Schema {
	switch p.ty {
	case TreeNodeTypeMakeTable:
		return *(p.val.(*MakeTable).Schema())
	case TreeNodeTypeSelect:
		return *(p.val.(*Select).Schema())
	case TreeNodeTypeLimit:
		return *(p.val.(*Limit).Schema())
	case TreeNodeTypeSort:
		return *(p.val.(*Sort).Schema())
	default:
		panic(fmt.Sprintf("unknown plan type: %v", p.ty))
	}
}

// MakeTable returns the concrete table plan if this is a table plan.
// Panics if this is not a maketable plan.
func (p TreeNode) MakeTable() *MakeTable {
	if p.ty != TreeNodeTypeMakeTable {
		panic(fmt.Sprintf("not a maketable plan: %v", p.ty))
	}
	return p.val.(*MakeTable)
}

// Select returns the concrete select plan if this is a select plan.
// Panics if this is not a select plan.
func (p TreeNode) Select() *Select {
	if p.ty != TreeNodeTypeSelect {
		panic(fmt.Sprintf("not a select plan: %v", p.ty))
	}
	return p.val.(*Select)
}

// Limit returns the concrete limit plan if this is a limit plan.
// Panics if this is not a limit plan.
func (p TreeNode) Limit() *Limit {
	if p.ty != TreeNodeTypeLimit {
		panic(fmt.Sprintf("not a limit plan: %v", p.ty))
	}
	return p.val.(*Limit)
}

// Sort returns the concrete sort plan if this is a sort plan.
// Panics if this is not a sort plan.
func (p TreeNode) Sort() *Sort {
	if p.ty != TreeNodeTypeSort {
		panic(fmt.Sprintf("not a sort plan: %v", p.ty))
	}
	return p.val.(*Sort)
}

// newPlan creates a new plan with the given type and value.
// This is a helper function for creating plans of different types.
func newPlan(ty TreeNodeType, val any) TreeNode {
	return TreeNode{
		ty:  ty,
		val: val,
	}
}

// NewMakeTableNode creates a new make table plan. This is the entry point for
// building a query plan, as all queries start with a base table relation.
func NewMakeTableNode(name string, schema schema.Schema) TreeNode {
	return newPlan(TreeNodeTypeMakeTable, makeTable(name, schema))
}

// NewSelectNode creates a new select plan. This applies a boolean expression
// to select (filter) rows from the input plan.
func NewSelectNode(input TreeNode, expr Expr) TreeNode {
	return newPlan(TreeNodeTypeSelect, newSelect(input, expr))
}

// NewLimitNode creates a new limit plan. This limits the number of rows
// returned by the input plan. If skip is 0, no rows are skipped. If fetch is
// 0, all rows are returned after applying skip.
func NewLimitNode(input TreeNode, skip uint64, fetch uint64) TreeNode {
	return newPlan(TreeNodeTypeLimit, newLimit(input, skip, fetch))
}

// NewSortNode creates a new sort plan. This sorts the rows from the input plan
// based on the provided sort expression.
func NewSortNode(input TreeNode, expr SortExpr) TreeNode {
	return newPlan(TreeNodeTypeSort, newSort(input, expr))
}
