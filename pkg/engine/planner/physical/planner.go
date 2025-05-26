package physical

import (
	"errors"
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
)

// Planner creates an executable physical plan from a logical plan.
// Planning is done in two steps:
//  1. Convert
//     Instructions from the logical plan are converted into Nodes in the physical plan.
//     Most instructions can be translated into a single Node. However, MakeTable translates into multiple DataObjScan nodes.
//  2. Pushdown
//     a) Push down the limit of the Limit node to the DataObjScan nodes.
//     b) Push down the predicate from the Filter node to the DataObjScan nodes.
type Planner struct {
	catalog Catalog
	plan    *Plan
}

// NewPlanner creates a new planner instance with the given context.
func NewPlanner(catalog Catalog) *Planner {
	return &Planner{catalog: catalog}
}

// Build converts a given logical plan into a physical plan and returns an error if the conversion fails.
// The resulting plan can be accessed using [Planner.Plan].
func (p *Planner) Build(lp *logical.Plan) (*Plan, error) {
	p.plan = &Plan{}
	for _, inst := range lp.Instructions {
		switch inst := inst.(type) {
		case *logical.Return:
			nodes, err := p.process(inst.Value)
			if err != nil {
				return nil, err
			}
			if len(nodes) > 1 {
				return nil, errors.New("logical plan has more than 1 return value")
			}
			return p.plan, nil
		}
	}
	return nil, errors.New("logical plan has no return value")
}

// Convert a predicate from an [logical.Instruction] into an [Expression].
func (p *Planner) convertPredicate(inst logical.Value) Expression {
	switch inst := inst.(type) {
	case *logical.UnaryOp:
		return &UnaryExpr{
			Left: p.convertPredicate(inst.Value),
			Op:   inst.Op,
		}
	case *logical.BinOp:
		return &BinaryExpr{
			Left:  p.convertPredicate(inst.Left),
			Right: p.convertPredicate(inst.Right),
			Op:    inst.Op,
		}
	case *logical.ColumnRef:
		return &ColumnExpr{Ref: inst.Ref}
	case *logical.Literal:
		return NewLiteral(inst.Value())
	default:
		panic(fmt.Sprintf("invalid value for predicate: %T", inst))
	}
}

// Convert a [logical.Instruction] into one or multiple [Node]s.
func (p *Planner) process(inst logical.Value) ([]Node, error) {
	switch inst := inst.(type) {
	case *logical.MakeTable:
		return p.processMakeTable(inst)
	case *logical.Select:
		return p.processSelect(inst)
	case *logical.Sort:
		return p.processSort(inst)
	case *logical.Limit:
		return p.processLimit(inst)
	}
	return nil, nil
}

// Convert [logical.MakeTable] into one or more [DataObjScan] nodes.
func (p *Planner) processMakeTable(lp *logical.MakeTable) ([]Node, error) {
	objects, streams, err := p.catalog.ResolveDataObj(p.convertPredicate(lp.Selector))
	if err != nil {
		return nil, err
	}
	nodes := make([]Node, 0, len(objects))
	for i := range objects {
		node := &DataObjScan{
			Location:  objects[i],
			StreamIDs: streams[i],
		}
		p.plan.addNode(node)
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// Convert [logical.Select] into one [Filter] node.
func (p *Planner) processSelect(lp *logical.Select) ([]Node, error) {
	node := &Filter{
		Predicates: []Expression{p.convertPredicate(lp.Predicate)},
	}
	p.plan.addNode(node)
	children, err := p.process(lp.Table)
	if err != nil {
		return nil, err
	}
	for i := range children {
		if err := p.plan.addEdge(Edge{Parent: node, Child: children[i]}); err != nil {
			return nil, err
		}
	}
	return []Node{node}, nil
}

// Convert [logical.Sort] into one [SortMerge] node.
func (p *Planner) processSort(lp *logical.Sort) ([]Node, error) {
	order := ASC
	if !lp.Ascending {
		order = DESC
	}
	node := &SortMerge{
		Column: &ColumnExpr{Ref: lp.Column.Ref},
		Order:  order,
	}
	p.plan.addNode(node)
	children, err := p.process(lp.Table)
	if err != nil {
		return nil, err
	}
	for i := range children {
		if err := p.plan.addEdge(Edge{Parent: node, Child: children[i]}); err != nil {
			return nil, err
		}
	}
	return []Node{node}, nil
}

// Convert [logical.Limit] into one [Limit] node.
func (p *Planner) processLimit(lp *logical.Limit) ([]Node, error) {
	node := &Limit{
		Skip:  lp.Skip,
		Fetch: lp.Fetch,
	}
	p.plan.addNode(node)
	children, err := p.process(lp.Table)
	if err != nil {
		return nil, err
	}
	for i := range children {
		if err := p.plan.addEdge(Edge{Parent: node, Child: children[i]}); err != nil {
			return nil, err
		}
	}
	return []Node{node}, nil
}

// Optimize tries to optimize the plan by pushing down filter predicates and limits
// to the scan nodes.
func (p *Planner) Optimize(plan *Plan) (*Plan, error) {
	for i, root := range plan.Roots() {

		optimizations := []*optimization{
			newOptimization("PredicatePushdown", plan).withRules(
				&predicatePushdown{plan: plan},
				&removeNoopFilter{plan: plan},
			),
			newOptimization("LimitPushdown", plan).withRules(
				&limitPushdown{plan: plan},
			),
		}
		optimizer := newOptimizer(plan, optimizations)
		optimizer.optimize(root)
		if i == 1 {
			return nil, errors.New("physical plan must only have exactly one root node")
		}
	}
	return plan, nil
}
