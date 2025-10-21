package physical

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// Context carries planning state that needs to be propagated down the plan tree.
// This enables each branch to have independent context, which is essential for complex queries:
// - Binary operations with different [$range] intervals, offsets or @timestamp.
//
// Propagation:
// - Each process() method passes context to children.
// - Context can be cloned and modified by nodes (ex: RangeAggregation sets rangeInterval) that gets inherited by all descendants.
type Context struct {
	from          time.Time
	through       time.Time
	rangeInterval time.Duration
	direction     SortOrder
	v1Compatible  bool
}

func NewContext(from, through time.Time) *Context {
	return &Context{
		from:    from,
		through: through,
	}
}

func (pc *Context) Clone() *Context {
	return &Context{
		from:          pc.from,
		through:       pc.through,
		rangeInterval: pc.rangeInterval,
		direction:     pc.direction,
	}
}

func (pc *Context) WithDirection(direction SortOrder) *Context {
	cloned := pc.Clone()
	cloned.direction = direction
	return cloned
}

func (pc *Context) WithRangeInterval(rangeInterval time.Duration) *Context {
	cloned := pc.Clone()
	cloned.rangeInterval = rangeInterval
	return cloned
}

func (pc *Context) WithTimeRange(from, through time.Time) *Context {
	cloned := pc.Clone()
	cloned.from = from
	cloned.through = through
	return cloned
}

func (pc *Context) GetResolveTimeRange() (from, through time.Time) {
	return pc.from.Add(-pc.rangeInterval), pc.through
}

// Planner creates an executable physical plan from a logical plan.
// Planning is done in two steps:
//  1. Convert
//     Instructions from the logical plan are converted into Nodes in the physical plan.
//     Most instructions can be translated into a single Node. However, MakeTable translates into multiple DataObjScan nodes.
//  2. Pushdown
//     a) Push down the limit of the Limit node to the DataObjScan nodes.
//     b) Push down the predicate from the Filter node to the DataObjScan nodes.
type Planner struct {
	context *Context
	catalog Catalog
	plan    *Plan
}

// NewPlanner creates a new planner instance with the given context.
func NewPlanner(ctx *Context, catalog Catalog) *Planner {
	return &Planner{
		context: ctx,
		catalog: catalog,
	}
}

// Build converts a given logical plan into a physical plan and returns an error if the conversion fails.
// The resulting plan can be accessed using [Planner.Plan].
func (p *Planner) Build(lp *logical.Plan) (*Plan, error) {
	p.reset()
	for _, inst := range lp.Instructions {
		switch inst := inst.(type) {
		case *logical.Return:
			_, err := p.process(inst.Value, p.context)
			if err != nil {
				return nil, err
			}
			return p.plan, nil
		}
	}
	return nil, errors.New("logical plan has no return value")
}

// reset resets the internal state of the planner
func (p *Planner) reset() {
	p.plan = &Plan{}
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

// Convert a [logical.Instruction] into [Node].
func (p *Planner) process(inst logical.Value, ctx *Context) (Node, error) {
	switch inst := inst.(type) {
	case *logical.MakeTable:
		return p.processMakeTable(inst, ctx)
	case *logical.Select:
		return p.processSelect(inst, ctx)
	case *logical.Projection:
		return p.processProjection(inst, ctx)
	case *logical.Sort:
		return p.processSort(inst, ctx)
	case *logical.Limit:
		return p.processLimit(inst, ctx)
	case *logical.RangeAggregation:
		return p.processRangeAggregation(inst, ctx)
	case *logical.VectorAggregation:
		return p.processVectorAggregation(inst, ctx)
	case *logical.Parse:
		return p.processParse(inst, ctx)
	case *logical.BinOp:
		return p.processBinOp(inst, ctx)
	case *logical.UnaryOp:
		return p.processUnaryOp(inst, ctx)
	case *logical.LogQLCompat:
		p.context.v1Compatible = true
		return p.process(inst.Value, ctx)
	}
	return nil, nil
}

// Convert [logical.MakeTable] into one or more [DataObjScan] nodes.
func (p *Planner) processMakeTable(lp *logical.MakeTable, ctx *Context) (Node, error) {
	shard, ok := lp.Shard.(*logical.ShardInfo)
	if !ok {
		return nil, fmt.Errorf("invalid shard, got %T", lp.Shard)
	}

	predicates := make([]Expression, len(lp.Predicates))
	for i, predicate := range lp.Predicates {
		predicates[i] = p.convertPredicate(predicate)
	}

	from, through := ctx.GetResolveTimeRange()

	filteredShardDescriptors, err := p.catalog.ResolveShardDescriptorsWithShard(p.convertPredicate(lp.Selector), predicates, ShardInfo(*shard), from, through)
	if err != nil {
		return nil, err
	}
	sort.Slice(filteredShardDescriptors, func(i, j int) bool {
		return filteredShardDescriptors[i].TimeRange.End.After(filteredShardDescriptors[j].TimeRange.End)
	})
	if ctx.direction == ASC {
		slices.Reverse(filteredShardDescriptors)
	}

	// Scan work can be parallelized across multiple workers, so we wrap
	// everything into a single Parallelize node.
	var parallelize Node = &Parallelize{}
	p.plan.graph.Add(parallelize)

	scanSet := &ScanSet{}
	p.plan.graph.Add(scanSet)

	for _, desc := range filteredShardDescriptors {
		for _, section := range desc.Sections {
			scanSet.Targets = append(scanSet.Targets, &ScanTarget{
				Type: ScanTypeDataObject,

				DataObject: &DataObjScan{
					Location:  desc.Location,
					StreamIDs: desc.Streams,
					Section:   section,
				},
			})
		}
	}

	var base Node = scanSet

	if p.context.v1Compatible {
		compat := &ColumnCompat{
			Source:      types.ColumnTypeMetadata,
			Destination: types.ColumnTypeMetadata,
			Collision:   types.ColumnTypeLabel,
		}
		base, err = p.wrapNodeWith(base, compat)
		if err != nil {
			return nil, err
		}
	}

	// Add an edge between the parallelize and the final base node (which may
	// have been changed after processing compatibility).
	if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: base}); err != nil {
		return nil, err
	}
	return parallelize, nil
}

// Convert [logical.Select] into one [Filter] node.
func (p *Planner) processSelect(lp *logical.Select, ctx *Context) (Node, error) {
	node := &Filter{
		Predicates: []Expression{p.convertPredicate(lp.Predicate)},
	}
	p.plan.graph.Add(node)
	child, err := p.process(lp.Table, ctx)
	if err != nil {
		return nil, err
	}
	if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}
	return node, nil
}

// processSort processes a [logical.Sort] node.
func (p *Planner) processSort(lp *logical.Sort, ctx *Context) (Node, error) {
	order := DESC
	if lp.Ascending {
		order = ASC
	}

	node := &TopK{
		SortBy:     &ColumnExpr{Ref: lp.Column.Ref},
		Ascending:  order == ASC,
		NullsFirst: false,

		// K initially starts at 0, indicating to sort everything. The
		// [limitPushdown] optimization pass can update this value based on how
		// many rows are needed.
		K: 0,
	}

	p.plan.graph.Add(node)

	child, err := p.process(lp.Table, ctx.WithDirection(order))
	if err != nil {
		return nil, err
	}

	if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}
	return node, nil
}

// Converts a [logical.Projection] into a physical [Projection] node.
func (p *Planner) processProjection(lp *logical.Projection, ctx *Context) (Node, error) {
	expressions := make([]Expression, len(lp.Expressions))
	for i := range lp.Expressions {
		expressions[i] = p.convertPredicate(lp.Expressions[i])
	}

	node := &Projection{
		Expressions: expressions,
		All:         lp.All,
		Expand:      lp.Expand,
		Drop:        lp.Drop,
	}
	p.plan.graph.Add(node)

	child, err := p.process(lp.Relation, ctx)
	if err != nil {
		return nil, err
	}
	if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}

	return node, nil
}

// Convert [logical.Limit] into one [Limit] node.
func (p *Planner) processLimit(lp *logical.Limit, ctx *Context) (Node, error) {
	node := &Limit{
		Skip:  lp.Skip,
		Fetch: lp.Fetch,
	}
	p.plan.graph.Add(node)
	child, err := p.process(lp.Table, ctx)
	if err != nil {
		return nil, err
	}
	if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}
	return node, nil
}

func (p *Planner) processRangeAggregation(r *logical.RangeAggregation, ctx *Context) (Node, error) {
	partitionBy := make([]ColumnExpression, len(r.PartitionBy))
	for i, col := range r.PartitionBy {
		partitionBy[i] = &ColumnExpr{Ref: col.Ref}
	}

	node := &RangeAggregation{
		PartitionBy: partitionBy,
		Operation:   r.Operation,
		Start:       r.Start,
		End:         r.End,
		Range:       r.RangeInterval,
		Step:        r.Step,
	}
	p.plan.graph.Add(node)

	child, err := p.process(r.Table, ctx.WithRangeInterval(r.RangeInterval))
	if err != nil {
		return nil, err
	}

	if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}
	return node, nil
}

// Convert [logical.VectorAggregation] into one [VectorAggregation] node.
func (p *Planner) processVectorAggregation(lp *logical.VectorAggregation, ctx *Context) (Node, error) {
	groupBy := make([]ColumnExpression, len(lp.GroupBy))
	for i, col := range lp.GroupBy {
		groupBy[i] = &ColumnExpr{Ref: col.Ref}
	}

	node := &VectorAggregation{
		GroupBy:   groupBy,
		Operation: lp.Operation,
	}
	p.plan.graph.Add(node)
	child, err := p.process(lp.Table, ctx)
	if err != nil {
		return nil, err
	}
	if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}
	return node, nil
}

func (p *Planner) hasNonMathExpressionChild(n Node) bool {
	if n == nil {
		return false
	}

	if _, ok := n.(*Projection); ok {
		for _, c := range p.plan.Children(n) {
			if p.hasNonMathExpressionChild(c) {
				return true
			}
		}
		return false
	} else {
		return true
	}
}

func (p *Planner) processMathExpressionChild(c logical.Value, rootNode bool, ctx *Context) (Expression, Node, *ColumnExpr, error) {
	switch v := c.(type) {
	case *logical.BinOp:
		leftChild, leftInput, leftInputRef, err := p.processMathExpressionChild(v.Left, false, ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		rightChild, rightInput, rightInputRef, err := p.processMathExpressionChild(v.Right, false, ctx)
		if err != nil {
			return nil, nil, nil, err
		}

		// both left and right expressions have obj scans, replace with join
		if leftInput != nil && rightInput != nil {
			leftInputRef.Ref = types.ColumnRef{
				Column: "value_left",
				Type:   types.ColumnTypeGenerated,
			}
			rightInputRef.Ref = types.ColumnRef{
				Column: "value_right",
				Type:   types.ColumnTypeGenerated,
			}
			join := &Join{}
			p.plan.graph.Add(join)
			projection := &Projection{
				Expressions: []Expression{
					&BinaryExpr{
						Left:  leftChild,
						Right: rightChild,
						Op:    v.Op,
					},
				},
				All:    true,
				Expand: true,
			}
			p.plan.graph.Add(projection)
			if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: join, Child: projection}); err != nil {
				return nil, nil, nil, err
			}
			if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: projection, Child: leftInput}); err != nil {
				return nil, nil, nil, err
			}
			if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: projection, Child: rightInput}); err != nil {
				return nil, nil, nil, err
			}

			columnRef := newColumnExpr(types.ColumnNameGeneratedValue, types.ColumnTypeGenerated)

			return columnRef, join, columnRef, nil
		}

		input := leftInput
		if leftInput == nil {
			input = rightInput
		}
		inputRef := leftInputRef
		if leftInputRef == nil {
			inputRef = rightInputRef
		}
		expr := &BinaryExpr{
			Left:  leftChild,
			Right: rightChild,
			Op:    v.Op,
		}

		if rootNode {
			projection := &Projection{
				Expressions: []Expression{expr},
				All:         true,
				Expand:      true,
			}
			p.plan.graph.Add(projection)
			if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: projection, Child: input}); err != nil {
				return nil, nil, nil, err
			}

			columnRef := newColumnExpr(types.ColumnNameGeneratedValue, types.ColumnTypeGenerated)

			return columnRef, projection, columnRef, nil
		} else {
			return expr, input, inputRef, nil
		}
	case *logical.UnaryOp:
		child, input, inputRef, err := p.processMathExpressionChild(v.Value, false, ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		expr := &UnaryExpr{
			Left: child,
			Op:   v.Op,
		}
		if rootNode {
			projection := &Projection{
				Expressions: []Expression{expr},
				All:         true,
				Expand:      true,
			}
			p.plan.graph.Add(projection)
			if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: projection, Child: input}); err != nil {
				return nil, nil, nil, err
			}

			columnRef := newColumnExpr(types.ColumnNameGeneratedValue, types.ColumnTypeGenerated)

			return columnRef, projection, columnRef, nil
		} else {
			return expr, input, inputRef, nil
		}
	case *logical.Literal:
		return &LiteralExpr{Literal: v.Literal}, nil, nil, nil
	default:
		columnRef := newColumnExpr(types.ColumnNameGeneratedValue, types.ColumnTypeGenerated)
		child, err := p.process(c, ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		return columnRef, child, columnRef, nil
	}
}

func (p *Planner) processBinOp(lp *logical.BinOp, ctx *Context) (Node, error) {
	_, node, _, err := p.processMathExpressionChild(lp, true, ctx)
	if err != nil {
		return nil, err
	}

	return node, nil
}

func (p *Planner) processUnaryOp(lp *logical.UnaryOp, ctx *Context) (Node, error) {
	var left Expression

	if l, ok := lp.Value.(*logical.Literal); ok {
		left = &LiteralExpr{Literal: l.Literal}
	} else {
		left = newColumnExpr(types.ColumnNameGeneratedValue, types.ColumnTypeGenerated)
	}

	projectionNode := &Projection{
		Expressions: []Expression{
			&UnaryExpr{
				Left: left,
				Op:   lp.Op,
			},
		},
		All:    true,
		Expand: true,
	}
	p.plan.graph.Add(projectionNode)

	// if lhs is not a literal, then process children nodes
	if _, ok := lp.Value.(*logical.Literal); !ok {
		leftChild, err := p.process(lp.Value, ctx)
		if err != nil {
			return nil, err
		}
		if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: projectionNode, Child: leftChild}); err != nil {
			return nil, err
		}
	}

	return projectionNode, nil
}

// Convert [logical.Parse] into one [ParseNode] node.
// A ParseNode initially has an empty list of RequestedKeys which will be populated during optimization.
func (p *Planner) processParse(lp *logical.Parse, ctx *Context) (Node, error) {
	var node Node = &ParseNode{
		Kind: convertParserKind(lp.Kind),
	}
	p.plan.graph.Add(node)

	child, err := p.process(lp.Table, ctx)
	if err != nil {
		return nil, err
	}

	if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}

	if p.context.v1Compatible {
		compat := &ColumnCompat{
			Source:      types.ColumnTypeParsed,
			Destination: types.ColumnTypeParsed,
			Collision:   types.ColumnTypeLabel,
		}
		node, err = p.wrapNodeWith(node, compat)
		if err != nil {
			return nil, err
		}
	}

	return node, nil
}

func (p *Planner) wrapNodeWith(node Node, wrapper Node) (Node, error) {
	p.plan.graph.Add(wrapper)
	if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: wrapper, Child: node}); err != nil {
		return nil, err
	}
	return wrapper, nil
}

// Optimize runs optimization passes over the plan, modifying it
// if any optimizations can be applied.
func (p *Planner) Optimize(plan *Plan) (*Plan, error) {
	for i, root := range plan.Roots() {
		optimizations := []*optimization{
			newOptimization("PredicatePushdown", plan).withRules(
				&predicatePushdown{plan: plan},
			),
			newOptimization("LimitPushdown", plan).withRules(
				&limitPushdown{plan: plan},
			),
			newOptimization("ProjectionPushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
			newOptimization("ParallelPushdown", plan).withRules(
				&parallelPushdown{plan: plan},
			),

			// Perform cleanups at the very end.
			newOptimization("Cleanup", plan).withRules(
				&removeNoopFilter{plan: plan},
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

func convertParserKind(kind logical.ParserKind) ParserKind {
	switch kind {
	case logical.ParserLogfmt:
		return ParserLogfmt
	case logical.ParserJSON:
		return ParserJSON
	default:
		return ParserInvalid
	}
}
