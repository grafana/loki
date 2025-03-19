// Package logical provides a logical query plan representation for data processing operations.
// It defines a type system for expressions and plan nodes that can be used to build and
// manipulate query plans in a structured way.
package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// PlanType is an enum representing the type of plan node.
// It allows consumers to determine the concrete type of a Plan
// and safely cast to the appropriate interface.
type PlanType int

const (
	PlanTypeInvalid   PlanType = iota // Invalid or uninitialized plan
	PlanTypeMakeTable                 // Represents an operation to make a table relation.
	PlanTypeSelect                    // Represents a Select operation
	PlanTypeLimit                     // Represents a limit operation
	PlanTypeSort                      // Represents a sort operation
)

// String returns a string representation of the plan type.
// This is useful for debugging and error messages.
func (t PlanType) String() string {
	switch t {
	case PlanTypeInvalid:
		return "Invalid"
	case PlanTypeMakeTable:
		return "MakeTable"
	case PlanTypeSelect:
		return "Select"
	case PlanTypeLimit:
		return "Limit"
	case PlanTypeSort:
		return "Sort"
	default:
		return "Unknown"
	}
}

// Plan is the core plan type in the logical package.
// It wraps a concrete plan type (MakeTable, Select, etc.)
// and provides methods to safely access the underlying value.
// This approach replaces the previous interface-based design to reduce
// indirection and improve code clarity.
type Plan struct {
	ty  PlanType // The type of plan
	val any      // The concrete plan value
}

// Type returns the type of the plan.
// This allows consumers to determine the concrete type of the plan
// and safely cast to the appropriate interface.
func (p Plan) Type() PlanType {
	return p.ty
}

// Schema returns the schema of the data produced by this plan node.
// It delegates to the appropriate concrete plan type based on the plan type.
func (p Plan) Schema() schema.Schema {
	switch p.ty {
	case PlanTypeMakeTable:
		return *(p.val.(*MakeTable).Schema())
	case PlanTypeSelect:
		return p.val.(*Select).Schema()
	case PlanTypeLimit:
		return p.val.(*Limit).Schema()
	case PlanTypeSort:
		return p.val.(*Sort).Schema()
	default:
		panic(fmt.Sprintf("unknown plan type: %v", p.ty))
	}
}

// MakeTable returns the concrete table plan if this is a table plan.
// Panics if this is not a maketable plan.
func (p Plan) MakeTable() *MakeTable {
	if p.ty != PlanTypeMakeTable {
		panic(fmt.Sprintf("not a maketable plan: %v", p.ty))
	}
	return p.val.(*MakeTable)
}

// Select returns the concrete select plan if this is a select plan.
// Panics if this is not a select plan.
func (p Plan) Select() *Select {
	if p.ty != PlanTypeSelect {
		panic(fmt.Sprintf("not a select plan: %v", p.ty))
	}
	return p.val.(*Select)
}

// Limit returns the concrete limit plan if this is a limit plan.
// Panics if this is not a limit plan.
func (p Plan) Limit() *Limit {
	if p.ty != PlanTypeLimit {
		panic(fmt.Sprintf("not a limit plan: %v", p.ty))
	}
	return p.val.(*Limit)
}

// Sort returns the concrete sort plan if this is a sort plan.
// Panics if this is not a sort plan.
func (p Plan) Sort() *Sort {
	if p.ty != PlanTypeSort {
		panic(fmt.Sprintf("not a sort plan: %v", p.ty))
	}
	return p.val.(*Sort)
}

// newPlan creates a new plan with the given type and value.
// This is a helper function for creating plans of different types.
func newPlan(ty PlanType, val any) Plan {
	return Plan{
		ty:  ty,
		val: val,
	}
}

// NewMakeTable creates a new make table plan. This is the entry point for
// building a query plan, as all queries start with a base table relation.
func NewMakeTable(name string, schema schema.Schema) Plan {
	return newPlan(PlanTypeMakeTable, makeTable(name, schema))
}

// NewSelect creates a new select plan. This applies a boolean expression to
// select (filter) rows from the input plan.
func NewSelect(input Plan, expr Expr) Plan {
	return newPlan(PlanTypeSelect, newSelect(input, expr))
}

// NewLimit creates a new limit plan.
// This limits the number of rows returned by the input plan.
// If skip is 0, no rows are skipped. If fetch is 0, all rows are returned after applying skip.
func NewLimit(input Plan, skip uint64, fetch uint64) Plan {
	return newPlan(PlanTypeLimit, newLimit(input, skip, fetch))
}

// NewSort creates a new sort plan.
// This sorts the rows from the input plan based on the provided sort expression.
func NewSort(input Plan, expr SortExpr) Plan {
	return newPlan(PlanTypeSort, newSort(input, expr))
}
