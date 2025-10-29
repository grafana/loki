package physical

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/ulid"
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
	direction     physicalpb.SortOrder
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

func (pc *Context) WithDirection(direction physicalpb.SortOrder) *Context {
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
	plan    *physicalpb.Plan
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
func (p *Planner) Build(lp *logical.Plan) (*physicalpb.Plan, error) {
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
	p.plan = &physicalpb.Plan{}
}

// Convert a predicate from an [logical.Instruction] into an [Expression].
func (p *Planner) convertPredicate(inst logical.Value) *physicalpb.Expression {
	switch inst := inst.(type) {
	case *logical.UnaryOp:
		return (&physicalpb.UnaryExpression{
			Value: p.convertPredicate(inst.Value),
			Op:    unaryOpLogToPhys(inst.Op),
		}).ToExpression()
	case *logical.BinOp:
		return (&physicalpb.BinaryExpression{
			Left:  p.convertPredicate(inst.Left),
			Right: p.convertPredicate(inst.Right),
			Op:    binaryOpLogToPhys(inst.Op),
		}).ToExpression()
	case *logical.ColumnRef:
		return (&physicalpb.ColumnExpression{Name: inst.Ref.Column, Type: ColumnTypeLogToPhys(inst.Ref.Type)}).ToExpression()
	case *logical.Literal:
		return NewLiteral(inst.Value()).ToExpression()
	default:
		panic(fmt.Sprintf("invalid value for predicate: %T", inst))
	}
}

// Convert a [logical.Instruction] into [physicalpb.Node].
func (p *Planner) process(inst logical.Value, ctx *Context) (physicalpb.Node, error) {
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
func (p *Planner) processMakeTable(lp *logical.MakeTable, ctx *Context) (physicalpb.Node, error) {
	shard, ok := lp.Shard.(*logical.ShardInfo)
	if !ok {
		return nil, fmt.Errorf("invalid shard, got %T", lp.Shard)
	}

	predicates := make([]*physicalpb.Expression, len(lp.Predicates))
	for i, predicate := range lp.Predicates {
		predicates[i] = p.convertPredicate(predicate)
	}

	from, through := ctx.GetResolveTimeRange()

	filteredShardDescriptors, err := p.catalog.ResolveShardDescriptorsWithShard(*p.convertPredicate(lp.Selector), predicates, ShardInfo(*shard), from, through)
	if err != nil {
		return nil, err
	}
	sort.Slice(filteredShardDescriptors, func(i, j int) bool {
		return filteredShardDescriptors[i].TimeRange.End.After(filteredShardDescriptors[j].TimeRange.End)
	})
	if ctx.direction == physicalpb.SORT_ORDER_ASCENDING {
		slices.Reverse(filteredShardDescriptors)
	}

	// Scan work can be parallelized across multiple workers, so we wrap
	// everything into a single Parallelize node.
	var parallelize physicalpb.Node = &physicalpb.Parallelize{Id: physicalpb.PlanNodeID{Value: ulid.New()}}
	p.plan.Add(parallelize)

	scanSet := &physicalpb.ScanSet{Id: physicalpb.PlanNodeID{Value: ulid.New()}}
	p.plan.Add(scanSet)

	for _, desc := range filteredShardDescriptors {
		for _, section := range desc.Sections {
			scanSet.Targets = append(scanSet.Targets, &physicalpb.ScanTarget{
				Type: physicalpb.SCAN_TYPE_DATA_OBJECT,

				DataObject: &physicalpb.DataObjScan{
					Id:        physicalpb.PlanNodeID{Value: ulid.New()},
					Location:  string(desc.Location),
					StreamIds: desc.Streams,
					Section:   int64(section),
				},
			})
		}
	}

	var base physicalpb.Node = scanSet

	if p.context.v1Compatible {
		compat := &physicalpb.ColumnCompat{
			Id:          physicalpb.PlanNodeID{Value: ulid.New()},
			Source:      physicalpb.COLUMN_TYPE_METADATA,
			Destination: physicalpb.COLUMN_TYPE_METADATA,
			Collision:   physicalpb.COLUMN_TYPE_LABEL,
		}
		base, err = p.wrapNodeWith(base, compat)
		if err != nil {
			return nil, err
		}
	}

	// Add an edge between the parallelize and the final base node (which may
	// have been changed after processing compatibility).
	if err := p.plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: parallelize, Child: base}); err != nil {
		return nil, err
	}
	return parallelize, nil
}

// Convert [logical.Select] into one [Filter] node.
func (p *Planner) processSelect(lp *logical.Select, ctx *Context) (physicalpb.Node, error) {
	node := &physicalpb.Filter{
		Id:         physicalpb.PlanNodeID{Value: ulid.New()},
		Predicates: []*physicalpb.Expression{p.convertPredicate(lp.Predicate)},
	}
	p.plan.Add(node)
	child, err := p.process(lp.Table, ctx)
	if err != nil {
		return nil, err
	}
	if err := p.plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}
	return node, nil
}

// Pass sort direction from [logical.Sort] to the children.
func (p *Planner) processSort(lp *logical.Sort, ctx *Context) (physicalpb.Node, error) {
	order := physicalpb.SORT_ORDER_DESCENDING
	if lp.Ascending {
		order = physicalpb.SORT_ORDER_ASCENDING
	}

	node := &physicalpb.TopK{
		Id:         physicalpb.PlanNodeID{Value: ulid.New()},
		SortBy:     &physicalpb.ColumnExpression{Name: lp.Column.Ref.Column, Type: ColumnTypeLogToPhys(lp.Column.Ref.Type)},
		Ascending:  order == physicalpb.SORT_ORDER_ASCENDING,
		NullsFirst: false,
		// K initially starts at 0, indicating to sort everything. The
		// [limitPushdown] optimization pass can update this value based on how
		// many rows are needed.
		K: 0,
	}

	p.plan.Add(node)

	child, err := p.process(lp.Table, ctx.WithDirection(order))
	if err != nil {
		return nil, err
	}

	if err := p.plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}
	return node, nil
}

// Converts a [logical.Projection] into a physical [Projection] node.
func (p *Planner) processProjection(lp *logical.Projection, ctx *Context) (physicalpb.Node, error) {
	expressions := make([]*physicalpb.Expression, len(lp.Expressions))
	for i := range lp.Expressions {
		expressions[i] = p.convertPredicate(lp.Expressions[i])
	}

	node := &physicalpb.Projection{
		Id:          physicalpb.PlanNodeID{Value: ulid.New()},
		Expressions: expressions,
		All:         lp.All,
		Expand:      lp.Expand,
		Drop:        lp.Drop,
	}
	p.plan.Add(node)

	child, err := p.process(lp.Relation, ctx)
	if err != nil {
		return nil, err
	}
	if err := p.plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}

	return node, nil
}

// Convert [logical.Limit] into one [Limit] node.
func (p *Planner) processLimit(lp *logical.Limit, ctx *Context) (physicalpb.Node, error) {
	node := &physicalpb.Limit{
		Id:    physicalpb.PlanNodeID{Value: ulid.New()},
		Skip:  lp.Skip,
		Fetch: lp.Fetch,
	}
	p.plan.Add(node)
	child, err := p.process(lp.Table, ctx)
	if err != nil {
		return nil, err
	}
	if err := p.plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}
	return node, nil
}

func (p *Planner) processRangeAggregation(r *logical.RangeAggregation, ctx *Context) (physicalpb.Node, error) {
	partitionBy := make([]*physicalpb.ColumnExpression, len(r.PartitionBy))
	for i, col := range r.PartitionBy {
		partitionBy[i] = &physicalpb.ColumnExpression{Name: col.Ref.Column, Type: ColumnTypeLogToPhys(col.Ref.Type)}
	}

	node := &physicalpb.AggregateRange{
		Id:             physicalpb.PlanNodeID{Value: ulid.New()},
		PartitionBy:    partitionBy,
		Operation:      rangeAggregationTypeLogToPhys(r.Operation),
		StartUnixNanos: r.Start.UnixNano(),
		EndUnixNanos:   r.End.UnixNano(),
		RangeNs:        r.RangeInterval.Nanoseconds(),
		StepNs:         r.Step.Nanoseconds(),
	}
	p.plan.Add(node)

	child, err := p.process(r.Table, ctx.WithRangeInterval(r.RangeInterval))
	if err != nil {
		return nil, err
	}

	if err := p.plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}
	return node, nil
}

// Convert [logical.VectorAggregation] into one [VectorAggregation] node.
func (p *Planner) processVectorAggregation(lp *logical.VectorAggregation, ctx *Context) (physicalpb.Node, error) {
	groupBy := make([]*physicalpb.ColumnExpression, len(lp.GroupBy))
	for i, col := range lp.GroupBy {
		groupBy[i] = &physicalpb.ColumnExpression{Name: col.Ref.Column, Type: ColumnTypeLogToPhys(col.Ref.Type)}
	}

	node := &physicalpb.AggregateVector{
		Id:        physicalpb.PlanNodeID{Value: ulid.New()},
		GroupBy:   groupBy,
		Operation: vectorAggregationTypeLogToPhys(lp.Operation),
	}
	p.plan.Add(node)
	child, err := p.process(lp.Table, ctx)
	if err != nil {
		return nil, err
	}
	if err := p.plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: node, Child: child}); err != nil {
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
func (p *Planner) collapseMathExpressions(lp logical.Value, rootNode bool, ctx *Context) (acc *physicalpb.Expression, input physicalpb.Node, inputRef *physicalpb.ColumnExpression, err error) {
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
			leftInputRef = &physicalpb.ColumnExpression{
				Name: "value_left",
				Type: physicalpb.COLUMN_TYPE_GENERATED,
			}
			rightInputRef = &physicalpb.ColumnExpression{
				Name: "value_right",
				Type: physicalpb.COLUMN_TYPE_GENERATED,
			}

			// Insert an InnerJoin on timestamp before Projection
			join := &physicalpb.Join{Id: physicalpb.PlanNodeID{Value: ulid.New()}}
			p.plan.Add(join)

			projection := &physicalpb.Projection{
				Expressions: []*physicalpb.Expression{
					(&physicalpb.BinaryExpression{
						Left:  leftChild,
						Right: rightChild,
						Op:    binaryOpLogToPhys(v.Op),
					}).ToExpression(),
				},
				All:    true,
				Expand: true,
			}
			p.plan.Add(projection)

			// Connect the join to the projection
			if err := p.plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: projection, Child: join}); err != nil {
				return nil, nil, nil, err
			}
			// Connect left and right children to the join
			if err := p.plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: join, Child: leftInput}); err != nil {
				return nil, nil, nil, err
			}
			if err := p.plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: join, Child: rightInput}); err != nil {
				return nil, nil, nil, err
			}

			// Result of this math expression returns `value` column
			columnRef := newColumnExpr(types.ColumnNameGeneratedValue, physicalpb.COLUMN_TYPE_GENERATED)

			return columnRef.ToExpression(), join, columnRef, nil
		}

		// Either left or right expression has an obj scan. Pick the non-nil one.
		input := leftInput
		if leftInput == nil {
			input = rightInput
		}
		inputRef := leftInputRef
		if leftInputRef == nil {
			inputRef = rightInputRef
		}
		expr := &physicalpb.BinaryExpression{
			Left:  leftChild,
			Right: rightChild,
			Op:    binaryOpLogToPhys(v.Op),
		}

		// we have to stop here and produce a Node, otherwise keep collapsing
		if rootNode {
			projection := &physicalpb.Projection{
				Id:          physicalpb.PlanNodeID{Value: ulid.New()},
				Expressions: []*physicalpb.Expression{expr.ToExpression()},
				All:         true,
				Expand:      true,
			}
			p.plan.Add(projection)
			if err := p.plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: projection, Child: input}); err != nil {
				return nil, nil, nil, err
			}

			columnRef := newColumnExpr(types.ColumnNameGeneratedValue, physicalpb.COLUMN_TYPE_GENERATED)

			return columnRef.ToExpression(), projection, columnRef, nil
		}

		return expr.ToExpression(), input, inputRef, nil
	case *logical.UnaryOp:
		child, input, inputRef, err := p.collapseMathExpressions(v.Value, false, ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		expr := &physicalpb.UnaryExpression{
			Value: child,
			Op:    unaryOpLogToPhys(v.Op),
		}
		if rootNode {
			projection := &physicalpb.Projection{
				Id:          physicalpb.PlanNodeID{Value: ulid.New()},
				Expressions: []*physicalpb.Expression{expr.ToExpression()},
				All:         true,
				Expand:      true,
			}
			p.plan.Add(projection)
			if err := p.plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: projection, Child: input}); err != nil {
				return nil, nil, nil, err
			}

			columnRef := newColumnExpr(types.ColumnNameGeneratedValue, physicalpb.COLUMN_TYPE_GENERATED)

			return columnRef.ToExpression(), projection, columnRef, nil
		}

		return expr.ToExpression(), input, inputRef, nil
	case *logical.Literal:
		return NewLiteral(v.Value()).ToExpression(), nil, nil, nil
	default:
		// If it is neigher a literal nor an expression, then we continue `p.process` on this node and represent in
		// as a column ref `value` in the final math expression.
		columnRef := newColumnExpr(types.ColumnNameGeneratedValue, physicalpb.COLUMN_TYPE_GENERATED)
		child, err := p.process(lp, ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		return columnRef.ToExpression(), child, columnRef, nil
	}
}

// Convert one or several [logical.BinOp]s into one [Projection] node where the result expression might be complex with
// multiple binary or unary operations. It also might insert a [Join] node before a [Projection] if this math expression
// reads data on both left and right sides.
func (p *Planner) processBinOp(lp *logical.BinOp, ctx *Context) (physicalpb.Node, error) {
	_, node, _, err := p.collapseMathExpressions(lp, true, ctx)
	if err != nil {
		return nil, err
	}

	return node, nil
}

// Convert one or several [logical.UnaryOp]s into one [Projection] node where the result expression might be complex with
// multiple binary or unary operations. It also might insert a [Join] node before a [Projection] if this math expression
// reads data on both left and right sides.
func (p *Planner) processUnaryOp(lp *logical.UnaryOp, ctx *Context) (physicalpb.Node, error) {
	_, node, _, err := p.collapseMathExpressions(lp, true, ctx)
	if err != nil {
		return nil, err
	}

	return node, nil
}

// Convert [logical.Parse] into one [ParseNode] node.
// A ParseNode initially has an empty list of RequestedKeys which will be populated during optimization.
func (p *Planner) processParse(lp *logical.Parse, ctx *Context) (physicalpb.Node, error) {
	var node physicalpb.Node = &physicalpb.Parse{
		Id:        physicalpb.PlanNodeID{Value: ulid.New()},
		Operation: convertParserKind(lp.Kind),
	}
	p.plan.Add(node)

	child, err := p.process(lp.Table, ctx)
	if err != nil {
		return nil, err
	}

	if err := p.plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}

	if p.context.v1Compatible {
		compat := &physicalpb.ColumnCompat{
			Id:          physicalpb.PlanNodeID{Value: ulid.New()},
			Source:      physicalpb.COLUMN_TYPE_PARSED,
			Destination: physicalpb.COLUMN_TYPE_PARSED,
			Collision:   physicalpb.COLUMN_TYPE_LABEL,
		}
		node, err = p.wrapNodeWith(node, compat)
		if err != nil {
			return nil, err
		}
	}

	return node, nil
}

func (p *Planner) wrapNodeWith(node physicalpb.Node, wrapper physicalpb.Node) (physicalpb.Node, error) {
	p.plan.Add(wrapper)
	if err := p.plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: wrapper, Child: node}); err != nil {
		return nil, err
	}
	return wrapper, nil
}

// Optimize runs optimization passes over the plan, modifying it
// if any optimizations can be applied.
func (p *Planner) Optimize(plan *physicalpb.Plan) (*physicalpb.Plan, error) {
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

func convertParserKind(kind logical.ParserKind) physicalpb.ParseOp {
	switch kind {
	case logical.ParserLogfmt:
		return physicalpb.PARSE_OP_LOGFMT
	case logical.ParserJSON:
		return physicalpb.PARSE_OP_JSON
	default:
		return physicalpb.PARSE_OP_INVALID
	}
}

func unaryOpLogToPhys(op types.UnaryOp) physicalpb.UnaryOp {
	switch op {
	case types.UnaryOpAbs:
		return physicalpb.UNARY_OP_ABS
	case types.UnaryOpCastBytes:
		return physicalpb.UNARY_OP_CAST_BYTES
	case types.UnaryOpCastDuration:
		return physicalpb.UNARY_OP_CAST_DURATION
	case types.UnaryOpCastFloat:
		return physicalpb.UNARY_OP_CAST_FLOAT
	case types.UnaryOpNot:
		return physicalpb.UNARY_OP_NOT
	default:
		return physicalpb.UNARY_OP_INVALID
	}
}

func binaryOpLogToPhys(op types.BinaryOp) physicalpb.BinaryOp {
	switch op {
	case types.BinaryOpEq:
		return physicalpb.BINARY_OP_EQ
	case types.BinaryOpNeq:
		return physicalpb.BINARY_OP_NEQ
	case types.BinaryOpGt:
		return physicalpb.BINARY_OP_GT
	case types.BinaryOpGte:
		return physicalpb.BINARY_OP_GTE
	case types.BinaryOpLt:
		return physicalpb.BINARY_OP_LT
	case types.BinaryOpLte:
		return physicalpb.BINARY_OP_LTE

	case types.BinaryOpAnd:
		return physicalpb.BINARY_OP_AND
	case types.BinaryOpOr:
		return physicalpb.BINARY_OP_OR
	case types.BinaryOpXor:
		return physicalpb.BINARY_OP_XOR
	case types.BinaryOpNot:
		return physicalpb.BINARY_OP_NOT

	case types.BinaryOpAdd:
		return physicalpb.BINARY_OP_ADD
	case types.BinaryOpSub:
		return physicalpb.BINARY_OP_SUB
	case types.BinaryOpMul:
		return physicalpb.BINARY_OP_MUL
	case types.BinaryOpDiv:
		return physicalpb.BINARY_OP_DIV
	case types.BinaryOpMod:
		return physicalpb.BINARY_OP_MOD
	case types.BinaryOpPow:
		return physicalpb.BINARY_OP_POW

	case types.BinaryOpMatchSubstr:
		return physicalpb.BINARY_OP_MATCH_SUBSTR
	case types.BinaryOpNotMatchSubstr:
		return physicalpb.BINARY_OP_NOT_MATCH_SUBSTR
	case types.BinaryOpMatchRe:
		return physicalpb.BINARY_OP_MATCH_RE
	case types.BinaryOpNotMatchRe:
		return physicalpb.BINARY_OP_NOT_MATCH_RE
	case types.BinaryOpMatchPattern:
		return physicalpb.BINARY_OP_MATCH_PATTERN
	case types.BinaryOpNotMatchPattern:
		return physicalpb.BINARY_OP_NOT_MATCH_PATTERN
	default:
		return physicalpb.BINARY_OP_INVALID
	}
}

func ColumnTypeLogToPhys(colType types.ColumnType) physicalpb.ColumnType {
	switch colType {
	case types.ColumnTypeBuiltin:
		return physicalpb.COLUMN_TYPE_BUILTIN
	case types.ColumnTypeLabel:
		return physicalpb.COLUMN_TYPE_LABEL
	case types.ColumnTypeMetadata:
		return physicalpb.COLUMN_TYPE_METADATA
	case types.ColumnTypeParsed:
		return physicalpb.COLUMN_TYPE_PARSED
	case types.ColumnTypeAmbiguous:
		return physicalpb.COLUMN_TYPE_AMBIGUOUS
	case types.ColumnTypeGenerated:
		return physicalpb.COLUMN_TYPE_GENERATED
	default:
		return physicalpb.COLUMN_TYPE_INVALID
	}
}

func rangeAggregationTypeLogToPhys(rangeAggType types.RangeAggregationType) physicalpb.AggregateRangeOp {
	switch rangeAggType {
	case types.RangeAggregationTypeCount:
		return physicalpb.AGGREGATE_RANGE_OP_COUNT
	case types.RangeAggregationTypeSum:
		return physicalpb.AGGREGATE_RANGE_OP_SUM
	case types.RangeAggregationTypeMax:
		return physicalpb.AGGREGATE_RANGE_OP_MAX
	case types.RangeAggregationTypeMin:
		return physicalpb.AGGREGATE_RANGE_OP_MIN
	default:
		return physicalpb.AGGREGATE_RANGE_OP_INVALID
	}
}

func vectorAggregationTypeLogToPhys(vectorAggType types.VectorAggregationType) physicalpb.AggregateVectorOp {
	switch vectorAggType {
	case types.VectorAggregationTypeSum:
		return physicalpb.AGGREGATE_VECTOR_OP_SUM
	case types.VectorAggregationTypeMax:
		return physicalpb.AGGREGATE_VECTOR_OP_MAX
	case types.VectorAggregationTypeMin:
		return physicalpb.AGGREGATE_VECTOR_OP_MIN
	case types.VectorAggregationTypeCount:
		return physicalpb.AGGREGATE_VECTOR_OP_COUNT
	case types.VectorAggregationTypeAvg:
		return physicalpb.AGGREGATE_VECTOR_OP_AVG
	case types.VectorAggregationTypeStddev:
		return physicalpb.AGGREGATE_VECTOR_OP_STDDEV
	case types.VectorAggregationTypeStdvar:
		return physicalpb.AGGREGATE_VECTOR_OP_STDVAR
	case types.VectorAggregationTypeBottomK:
		return physicalpb.AGGREGATE_VECTOR_OP_BOTTOMK
	case types.VectorAggregationTypeTopK:
		return physicalpb.AGGREGATE_VECTOR_OP_TOPK
	case types.VectorAggregationTypeSort:
		return physicalpb.AGGREGATE_VECTOR_OP_SORT
	case types.VectorAggregationTypeSortDesc:
		return physicalpb.AGGREGATE_VECTOR_OP_SORT_DESC
	default:
		return physicalpb.AGGREGATE_VECTOR_OP_INVALID
	}
}
