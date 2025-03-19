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
	PlanTypeInvalid    PlanType = iota // Invalid or uninitialized plan
	PlanTypeTable                      // Represents a table scan operation
	PlanTypeFilter                     // Represents a filter operation
	PlanTypeProjection                 // Represents a projection operation
	PlanTypeAggregate                  // Represents an aggregation operation
	PlanTypeLimit                      // Represents a limit operation
	PlanTypeSort                       // Represents a sort operation
)

// String returns a string representation of the plan type.
// This is useful for debugging and error messages.
func (t PlanType) String() string {
	switch t {
	case PlanTypeInvalid:
		return "Invalid"
	case PlanTypeTable:
		return "Table"
	case PlanTypeFilter:
		return "Filter"
	case PlanTypeProjection:
		return "Projection"
	case PlanTypeAggregate:
		return "Aggregate"
	case PlanTypeLimit:
		return "Limit"
	case PlanTypeSort:
		return "Sort"
	default:
		return "Unknown"
	}
}

// Plan is the core plan type in the logical package.
// It wraps a concrete plan type (MakeTable, Filter, etc.)
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
	case PlanTypeTable:
		return p.val.(*MakeTable).Schema()
	case PlanTypeFilter:
		return p.val.(*Filter).Schema()
	case PlanTypeProjection:
		return p.val.(*Projection).Schema()
	case PlanTypeAggregate:
		return p.val.(*Aggregate).Schema()
	case PlanTypeLimit:
		return p.val.(*Limit).Schema()
	case PlanTypeSort:
		return p.val.(*Sort).Schema()
	default:
		panic(fmt.Sprintf("unknown plan type: %v", p.ty))
	}
}

// Table returns the concrete table plan if this is a table plan.
// Panics if this is not a table plan.
func (p Plan) Table() *MakeTable {
	if p.ty != PlanTypeTable {
		panic(fmt.Sprintf("not a table plan: %v", p.ty))
	}
	return p.val.(*MakeTable)
}

// Filter returns the concrete filter plan if this is a filter plan.
// Panics if this is not a filter plan.
func (p Plan) Filter() *Filter {
	if p.ty != PlanTypeFilter {
		panic(fmt.Sprintf("not a filter plan: %v", p.ty))
	}
	return p.val.(*Filter)
}

// Projection returns the concrete projection plan if this is a projection plan.
// Panics if this is not a projection plan.
func (p Plan) Projection() *Projection {
	if p.ty != PlanTypeProjection {
		panic(fmt.Sprintf("not a projection plan: %v", p.ty))
	}
	return p.val.(*Projection)
}

// Aggregate returns the concrete aggregate plan if this is an aggregate plan.
// Panics if this is not an aggregate plan.
func (p Plan) Aggregate() *Aggregate {
	if p.ty != PlanTypeAggregate {
		panic(fmt.Sprintf("not an aggregate plan: %v", p.ty))
	}
	return p.val.(*Aggregate)
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

// NewScan creates a new table scan plan.
// This is the entry point for building a query plan, as all queries
// start with scanning a table.
func NewScan(name string, schema schema.Schema) Plan {
	return newPlan(PlanTypeTable, makeTable(name, schema))
}

// NewFilter creates a new filter plan.
// This applies a boolean expression to filter rows from the input plan.
func NewFilter(input Plan, expr Expr) Plan {
	return newPlan(PlanTypeFilter, newFilter(input, expr))
}

// NewProjection creates a new projection plan.
// This applies a list of expressions to project columns from the input plan.
func NewProjection(input Plan, exprs []Expr) Plan {
	return newPlan(PlanTypeProjection, newProjection(input, exprs))
}

// NewAggregate creates a new aggregate plan.
// This applies grouping and aggregation to the input plan.
func NewAggregate(input Plan, groupExprs []Expr, aggExprs []AggregateExpr) Plan {
	return newPlan(PlanTypeAggregate, newAggregate(input, groupExprs, aggExprs))
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
