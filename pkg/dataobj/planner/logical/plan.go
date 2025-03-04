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
	PlanTypeInvalid    PlanType = iota
	PlanTypeTable               // Represents a table scan operation
	PlanTypeFilter              // Represents a filter operation
	PlanTypeProjection          // Represents a projection operation
	PlanTypeAggregate           // Represents an aggregation operation
)

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

type Plan struct {
	ty  PlanType
	val any
}

func (p Plan) Type() PlanType {
	return p.ty
}

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

// shortcut: must be checked elsewhere
func (p Plan) Table() *MakeTable {
	if p.ty != PlanTypeTable {
		panic(fmt.Sprintf("plan is not a table: %d", p.ty))
	}
	return p.val.(*MakeTable)
}

// shortcut: must be checked elsewhere
func (p Plan) Filter() *Filter {
	if p.ty != PlanTypeFilter {
		panic(fmt.Sprintf("plan is not a filter: %d", p.ty))
	}
	return p.val.(*Filter)
}

// shortcut: must be checked elsewhere
func (p Plan) Projection() *Projection {
	if p.ty != PlanTypeProjection {
		panic(fmt.Sprintf("plan is not a projection: %d", p.ty))
	}
	return p.val.(*Projection)
}

// shortcut: must be checked elsewhere
func (p Plan) Aggregate() *Aggregate {
	if p.ty != PlanTypeAggregate {
		panic(fmt.Sprintf("plan is not an aggregate: %d", p.ty))
	}
	return p.val.(*Aggregate)
}

func newPlan(ty PlanType, val any) Plan {
	return Plan{
		ty:  ty,
		val: val,
	}
}

func NewScan(name string, schema schema.Schema) Plan {
	return newPlan(PlanTypeTable, makeTable(name, schema))
}

func NewFilter(input Plan, expr Expr) Plan {
	return newPlan(PlanTypeFilter, newFilter(input, expr))
}

func NewProjection(input Plan, exprs []Expr) Plan {
	return newPlan(PlanTypeProjection, newProjection(input, exprs))
}

func NewAggregate(input Plan, groupExprs []Expr, aggExprs []AggregateExpr) Plan {
	return newPlan(PlanTypeAggregate, newAggregate(input, groupExprs, aggExprs))
}
