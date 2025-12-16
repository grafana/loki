package physical

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/oklog/ulid/v2"

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
	case *logical.FunctionOp:
		exprs := make([]Expression, len(inst.Values))
		for i, v := range inst.Values {
			exprs[i] = p.convertPredicate(v)
		}
		node := &VariadicExpr{
			Op:          inst.Op,
			Expressions: exprs,
		}
		return node
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
	case *logical.TopK:
		return p.processTopK(inst, ctx)
	case *logical.RangeAggregation:
		return p.processRangeAggregation(inst, ctx)
	case *logical.VectorAggregation:
		return p.processVectorAggregation(inst, ctx)
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

	dataObjs, err := p.catalog.ResolveDataObjSections(p.convertPredicate(lp.Selector), predicates, ShardInfo(*shard), from, through)
	if err != nil {
		return nil, err
	}
	sort.Slice(dataObjs, func(i, j int) bool {
		return dataObjs[i].TimeRange.End.After(dataObjs[j].TimeRange.End)
	})
	if ctx.direction == ASC {
		slices.Reverse(dataObjs)
	}

	// Scan work can be parallelized across multiple workers, so we wrap
	// everything into a single Parallelize node.
	var parallelize Node = &Parallelize{
		NodeID: ulid.Make(),
	}
	p.plan.graph.Add(parallelize)

	scanSet := &ScanSet{
		NodeID: ulid.Make(),
	}
	p.plan.graph.Add(scanSet)

	for _, dataObj := range dataObjs {
		for _, section := range dataObj.Sections {
			scanSet.Targets = append(scanSet.Targets, &ScanTarget{
				Type: ScanTypeDataObject,

				DataObject: &DataObjScan{
					NodeID: ulid.Make(),

					Location:     dataObj.Location,
					StreamIDs:    dataObj.Streams,
					Section:      section,
					MaxTimeRange: dataObj.TimeRange,
				},
			})
		}
	}

	var base Node = scanSet

	if p.context.v1Compatible {
		compat := &ColumnCompat{
			NodeID: ulid.Make(),

			Source:      types.ColumnTypeMetadata,
			Destination: types.ColumnTypeMetadata,
			Collisions:  []types.ColumnType{types.ColumnTypeLabel},
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
		NodeID: ulid.Make(),

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
		NodeID: ulid.Make(),

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

// processTopK processes a [logical.TopK] node.
func (p *Planner) processTopK(lp *logical.TopK, ctx *Context) (Node, error) {
	order := DESC
	if lp.Ascending {
		order = ASC
	}

	node := &TopK{
		NodeID: ulid.Make(),

		SortBy:     &ColumnExpr{Ref: lp.SortBy.Ref},
		Ascending:  order == ASC,
		NullsFirst: lp.NullsFirst,
		K:          lp.K,
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
	needsCompat := false
	for i := range lp.Expressions {
		expressions[i] = p.convertPredicate(lp.Expressions[i])
		if funcExpr, ok := lp.Expressions[i].(*logical.FunctionOp); ok {
			if funcExpr.Op == types.VariadicOpParseJSON || funcExpr.Op == types.VariadicOpParseLogfmt {
				needsCompat = true
			}
		}
	}

	var node Node = &Projection{
		NodeID: ulid.Make(),

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

	if needsCompat && p.context.v1Compatible {
		compat := &ColumnCompat{
			NodeID: ulid.Make(),

			Source:      types.ColumnTypeParsed,
			Destination: types.ColumnTypeParsed,
			// Check for collisions against both label and metadata columns.
			Collisions: []types.ColumnType{types.ColumnTypeLabel, types.ColumnTypeMetadata},
		}
		var err error
		node, err = p.wrapNodeWith(node, compat)
		if err != nil {
			return nil, err
		}
	}

	return node, nil
}

// Convert [logical.Limit] into one [Limit] node.
func (p *Planner) processLimit(lp *logical.Limit, ctx *Context) (Node, error) {
	node := &Limit{
		NodeID: ulid.Make(),

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
	grouping := make([]ColumnExpression, len(r.Grouping.Columns))
	for i, col := range r.Grouping.Columns {
		grouping[i] = &ColumnExpr{Ref: col.Ref}
	}

	node := &RangeAggregation{
		NodeID: ulid.Make(),

		Grouping: Grouping{
			Columns: grouping,
			Without: r.Grouping.Without,
		},
		Operation: r.Operation,
		Start:     r.Start,
		End:       r.End,
		Range:     r.RangeInterval,
		Step:      r.Step,
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
	grouping := make([]ColumnExpression, len(lp.Grouping.Columns))
	for i, col := range lp.Grouping.Columns {
		grouping[i] = &ColumnExpr{Ref: col.Ref}
	}

	node := &VectorAggregation{
		NodeID: ulid.Make(),

		Grouping: Grouping{
			Columns: grouping,
			Without: lp.Grouping.Without,
		},
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

// collapseMathExpressions traverses over a subtree of math expressions `c` (BinOps, UnaryOps, or Literals) and collapses them
// into a Projection node with a complex Expression. It may insert a Join node if it finds a BinOp with two obj scan inputs.
// Parameters:
//   - lp: current logical plan node
//   - rootNode: true indicates that this is the first call and the function should produce a Node. false indicates
//     that this is a recursive call and the function should keep accumulating Expression if possible.
//   - ctx: context from the current planner call.
//
// Return:
//   - acc: currently accumulated expression.
//   - input: a physical plan node of the only input of that expression, if any.
//   - inputRef: a pointer to a node in `acc` that refers the input, if any. This is for convenience of
//     renaming the column refenrece without a need to search for it in `acc` expression.
//   - err: error
func (p *Planner) collapseMathExpressions(lp logical.Value, rootNode bool, ctx *Context) (acc Expression, input Node, inputRef *ColumnExpr, err error) {
	switch v := lp.(type) {
	case *logical.BinOp:
		// Traverse left and right children
		leftChild, leftInput, leftInputRef, err := p.collapseMathExpressions(v.Left, false, ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		rightChild, rightInput, rightInputRef, err := p.collapseMathExpressions(v.Right, false, ctx)
		if err != nil {
			return nil, nil, nil, err
		}

		// Both left and right expressions have obj scans, replace with join
		if leftInput != nil && rightInput != nil {
			// Replace column references with `_left` and `_right` indicating that there are two inputs coming from a Join
			leftInputRef.Ref = types.ColumnRef{
				Column: "value_left",
				Type:   types.ColumnTypeGenerated,
			}
			rightInputRef.Ref = types.ColumnRef{
				Column: "value_right",
				Type:   types.ColumnTypeGenerated,
			}

			// Insert an InnerJoin on timestamp before Projection
			join := &Join{NodeID: ulid.Make()}
			p.plan.graph.Add(join)

			projection := &Projection{
				NodeID: ulid.Make(),

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

			// Connect the join to the projection
			if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: projection, Child: join}); err != nil {
				return nil, nil, nil, err
			}
			// Connect left and right children to the join
			if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: join, Child: leftInput}); err != nil {
				return nil, nil, nil, err
			}
			if err := p.plan.graph.AddEdge(dag.Edge[Node]{Parent: join, Child: rightInput}); err != nil {
				return nil, nil, nil, err
			}

			// Result of this math expression returns `value` column
			columnRef := newColumnExpr(types.ColumnNameGeneratedValue, types.ColumnTypeGenerated)

			return columnRef, join, columnRef, nil
		}

		// Eigther left or right expression has an obj scan. Pick the non-nil one.
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

		// we have to stop here and produce a Node, otherwise keep collapsing
		if rootNode {
			projection := &Projection{
				NodeID: ulid.Make(),

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
		}

		return expr, input, inputRef, nil
	case *logical.UnaryOp:
		child, input, inputRef, err := p.collapseMathExpressions(v.Value, false, ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		expr := &UnaryExpr{
			Left: child,
			Op:   v.Op,
		}
		if rootNode {
			projection := &Projection{
				NodeID: ulid.Make(),

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
		}

		return expr, input, inputRef, nil
	case *logical.Literal:
		return NewLiteral(v.Value()), nil, nil, nil
	default:
		// If it is neigher a literal nor an expression, then we continue `p.process` on this node and represent in
		// as a column ref `value` in the final math expression.
		columnRef := newColumnExpr(types.ColumnNameGeneratedValue, types.ColumnTypeGenerated)
		child, err := p.process(lp, ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		return columnRef, child, columnRef, nil
	}
}

// Convert one or several [logical.BinOp]s into one [Projection] node where the result expression might be complex with
// multiple binary or unary operations. It also might insert a [Join] node before a [Projection] if this math expression
// reads data on both left and right sides.
func (p *Planner) processBinOp(lp *logical.BinOp, ctx *Context) (Node, error) {
	_, node, _, err := p.collapseMathExpressions(lp, true, ctx)
	if err != nil {
		return nil, err
	}

	return node, nil
}

// Convert one or several [logical.UnaryOp]s into one [Projection] node where the result expression might be complex with
// multiple binary or unary operations. It also might insert a [Join] node before a [Projection] if this math expression
// reads data on both left and right sides.
func (p *Planner) processUnaryOp(lp *logical.UnaryOp, ctx *Context) (Node, error) {
	_, node, _, err := p.collapseMathExpressions(lp, true, ctx)
	if err != nil {
		return nil, err
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
			newOptimization("groupByPushdown", plan).withRules(
				&groupByPushdown{plan: plan},
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
