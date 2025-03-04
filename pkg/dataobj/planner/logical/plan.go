// Package logical provides a logical query plan representation for data processing operations.
// It defines a type system for expressions and plan nodes that can be used to build and
// manipulate query plans in a structured way.
package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
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
)

// String returns a human-readable representation of the plan type.
func (t PlanType) String() string {
	switch t {
	case PlanTypeTable:
		return "Table"
	case PlanTypeFilter:
		return "Filter"
	case PlanTypeProjection:
		return "Projection"
	case PlanTypeAggregate:
		return "Aggregate"
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
	default:
		panic(fmt.Sprintf("unknown plan type: %d", p.ty))
	}
}

// Table returns the underlying MakeTable plan.
// Panics if the plan is not a table scan.
// This is a shortcut method that should only be used when the type is known.
func (p Plan) Table() *MakeTable {
	if p.ty != PlanTypeTable {
		panic(fmt.Sprintf("plan is not a table: %d", p.ty))
	}
	return p.val.(*MakeTable)
}

// Filter returns the underlying Filter plan.
// Panics if the plan is not a filter.
// This is a shortcut method that should only be used when the type is known.
func (p Plan) Filter() *Filter {
	if p.ty != PlanTypeFilter {
		panic(fmt.Sprintf("plan is not a filter: %d", p.ty))
	}
	return p.val.(*Filter)
}

// Projection returns the underlying Projection plan.
// Panics if the plan is not a projection.
// This is a shortcut method that should only be used when the type is known.
func (p Plan) Projection() *Projection {
	if p.ty != PlanTypeProjection {
		panic(fmt.Sprintf("plan is not a projection: %d", p.ty))
	}
	return p.val.(*Projection)
}

// Aggregate returns the underlying Aggregate plan.
// Panics if the plan is not an aggregate.
// This is a shortcut method that should only be used when the type is known.
func (p Plan) Aggregate() *Aggregate {
	if p.ty != PlanTypeAggregate {
		panic(fmt.Sprintf("plan is not an aggregate: %d", p.ty))
	}
	return p.val.(*Aggregate)
}

// newPlan creates a new Plan with the given type and value.
// This is an internal function used by the public constructor functions.
func newPlan(ty PlanType, val any) Plan {
	return Plan{
		ty:  ty,
		val: val,
	}
}

// NewScan creates a new table scan plan node.
// It represents the operation of reading data from a table with the given name and schema.
func NewScan(name string, schema schema.Schema) Plan {
	return newPlan(PlanTypeTable, makeTable(name, schema))
}

// NewFilter creates a new filter plan node.
// It represents the operation of filtering rows from the input plan based on the given expression.
func NewFilter(input Plan, expr Expr) Plan {
	return newPlan(PlanTypeFilter, newFilter(input, expr))
}

// NewProjection creates a new projection plan node.
// It represents the operation of projecting columns from the input plan based on the given expressions.
func NewProjection(input Plan, exprs []Expr) Plan {
	return newPlan(PlanTypeProjection, newProjection(input, exprs))
}

// NewAggregate creates a new aggregate plan node.
// It represents the operation of aggregating rows from the input plan based on the given
// grouping expressions and aggregate expressions.
func NewAggregate(input Plan, groupExprs []Expr, aggExprs []AggregateExpr) Plan {
	return newPlan(PlanTypeAggregate, newAggregate(input, groupExprs, aggExprs))
}
