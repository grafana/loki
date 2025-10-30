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
func (p *Planner) convertPredicate(inst logical.Value) *Expression {
	switch inst := inst.(type) {
	case *logical.UnaryOp:
		return (&UnaryExpression{
			Value: p.convertPredicate(inst.Value),
			Op:    unaryOpLogToPhys(inst.Op),
		}).ToExpression()
	case *logical.BinOp:
		return (&BinaryExpression{
			Left:  p.convertPredicate(inst.Left),
			Right: p.convertPredicate(inst.Right),
			Op:    binaryOpLogToPhys(inst.Op),
		}).ToExpression()
	case *logical.ColumnRef:
		return (&ColumnExpression{Name: inst.Ref.Column, Type: ColumnTypeLogToPhys(inst.Ref.Type)}).ToExpression()
	case *logical.Literal:
		return NewLiteral(inst.Value()).ToExpression()
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

	predicates := make([]*Expression, len(lp.Predicates))
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
	if ctx.direction == SORT_ORDER_ASCENDING {
		slices.Reverse(filteredShardDescriptors)
	}

	// Scan work can be parallelized across multiple workers, so we wrap
	// everything into a single Parallelize node.
	var parallelize Node = &Parallelize{Id: PlanNodeID{Value: ulid.New()}}
	p.plan.Add(parallelize)

	scanSet := &ScanSet{Id: PlanNodeID{Value: ulid.New()}}
	p.plan.Add(scanSet)

	for _, desc := range filteredShardDescriptors {
		for _, section := range desc.Sections {
			scanSet.Targets = append(scanSet.Targets, &ScanTarget{
				Type: SCAN_TYPE_DATA_OBJECT,

				DataObject: &DataObjScan{
					Id:        PlanNodeID{Value: ulid.New()},
					Location:  string(desc.Location),
					StreamIds: desc.Streams,
					Section:   int64(section),
				},
			})
		}
	}

	var base Node = scanSet

	if p.context.v1Compatible {
		compat := &ColumnCompat{
			Id:          PlanNodeID{Value: ulid.New()},
			Source:      COLUMN_TYPE_METADATA,
			Destination: COLUMN_TYPE_METADATA,
			Collision:   COLUMN_TYPE_LABEL,
		}
		base, err = p.wrapNodeWith(base, compat)
		if err != nil {
			return nil, err
		}
	}

	// Add an edge between the parallelize and the final base node (which may
	// have been changed after processing compatibility).
	if err := p.plan.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: base}); err != nil {
		return nil, err
	}
	return parallelize, nil
}

// Convert [logical.Select] into one [Filter] node.
func (p *Planner) processSelect(lp *logical.Select, ctx *Context) (Node, error) {
	node := &Filter{
		Id:         PlanNodeID{Value: ulid.New()},
		Predicates: []*Expression{p.convertPredicate(lp.Predicate)},
	}
	p.plan.Add(node)
	child, err := p.process(lp.Table, ctx)
	if err != nil {
		return nil, err
	}
	if err := p.plan.AddEdge(dag.Edge[Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}
	return node, nil
}

// Pass sort direction from [logical.Sort] to the children.
func (p *Planner) processSort(lp *logical.Sort, ctx *Context) (Node, error) {
	order := SORT_ORDER_DESCENDING
	if lp.Ascending {
		order = SORT_ORDER_ASCENDING
	}

	node := &TopK{
		Id:         PlanNodeID{Value: ulid.New()},
		SortBy:     &ColumnExpression{Name: lp.Column.Ref.Column, Type: ColumnTypeLogToPhys(lp.Column.Ref.Type)},
		Ascending:  order == SORT_ORDER_ASCENDING,
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

	if err := p.plan.AddEdge(dag.Edge[Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}
	return node, nil
}

// Converts a [logical.Projection] into a physical [Projection] node.
func (p *Planner) processProjection(lp *logical.Projection, ctx *Context) (Node, error) {
	expressions := make([]*Expression, len(lp.Expressions))
	for i := range lp.Expressions {
		expressions[i] = p.convertPredicate(lp.Expressions[i])
	}

	node := &Projection{
		Id:          PlanNodeID{Value: ulid.New()},
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
	if err := p.plan.AddEdge(dag.Edge[Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}

	return node, nil
}

// Convert [logical.Limit] into one [Limit] node.
func (p *Planner) processLimit(lp *logical.Limit, ctx *Context) (Node, error) {
	node := &Limit{
		Id:    PlanNodeID{Value: ulid.New()},
		Skip:  lp.Skip,
		Fetch: lp.Fetch,
	}
	p.plan.Add(node)
	child, err := p.process(lp.Table, ctx)
	if err != nil {
		return nil, err
	}
	if err := p.plan.AddEdge(dag.Edge[Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}
	return node, nil
}

func (p *Planner) processRangeAggregation(r *logical.RangeAggregation, ctx *Context) (Node, error) {
	partitionBy := make([]*ColumnExpression, len(r.PartitionBy))
	for i, col := range r.PartitionBy {
		partitionBy[i] = &ColumnExpression{Name: col.Ref.Column, Type: ColumnTypeLogToPhys(col.Ref.Type)}
	}

	node := &AggregateRange{
		Id:             PlanNodeID{Value: ulid.New()},
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

	if err := p.plan.AddEdge(dag.Edge[Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}
	return node, nil
}

// Convert [logical.VectorAggregation] into one [VectorAggregation] node.
func (p *Planner) processVectorAggregation(lp *logical.VectorAggregation, ctx *Context) (Node, error) {
	groupBy := make([]*ColumnExpression, len(lp.GroupBy))
	for i, col := range lp.GroupBy {
		groupBy[i] = &ColumnExpression{Name: col.Ref.Column, Type: ColumnTypeLogToPhys(col.Ref.Type)}
	}

	node := &AggregateVector{
		Id:        PlanNodeID{Value: ulid.New()},
		GroupBy:   groupBy,
		Operation: vectorAggregationTypeLogToPhys(lp.Operation),
	}
	p.plan.Add(node)
	child, err := p.process(lp.Table, ctx)
	if err != nil {
		return nil, err
	}
	if err := p.plan.AddEdge(dag.Edge[Node]{Parent: node, Child: child}); err != nil {
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
func (p *Planner) collapseMathExpressions(lp logical.Value, rootNode bool, ctx *Context) (acc *Expression, input Node, inputRef *ColumnExpression, err error) {
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
			leftInputRef = &ColumnExpression{
				Name: "value_left",
				Type: COLUMN_TYPE_GENERATED,
			}
			rightInputRef = &ColumnExpression{
				Name: "value_right",
				Type: COLUMN_TYPE_GENERATED,
			}

			// Insert an InnerJoin on timestamp before Projection
			join := &Join{Id: PlanNodeID{Value: ulid.New()}}
			p.plan.Add(join)

			projection := &Projection{
				Expressions: []*Expression{
					(&BinaryExpression{
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
			if err := p.plan.AddEdge(dag.Edge[Node]{Parent: projection, Child: join}); err != nil {
				return nil, nil, nil, err
			}
			// Connect left and right children to the join
			if err := p.plan.AddEdge(dag.Edge[Node]{Parent: join, Child: leftInput}); err != nil {
				return nil, nil, nil, err
			}
			if err := p.plan.AddEdge(dag.Edge[Node]{Parent: join, Child: rightInput}); err != nil {
				return nil, nil, nil, err
			}

			// Result of this math expression returns `value` column
			columnRef := newColumnExpr(types.ColumnNameGeneratedValue, COLUMN_TYPE_GENERATED)

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
		expr := &BinaryExpression{
			Left:  leftChild,
			Right: rightChild,
			Op:    binaryOpLogToPhys(v.Op),
		}

		// we have to stop here and produce a Node, otherwise keep collapsing
		if rootNode {
			projection := &Projection{
				Id:          PlanNodeID{Value: ulid.New()},
				Expressions: []*Expression{expr.ToExpression()},
				All:         true,
				Expand:      true,
			}
			p.plan.Add(projection)
			if err := p.plan.AddEdge(dag.Edge[Node]{Parent: projection, Child: input}); err != nil {
				return nil, nil, nil, err
			}

			columnRef := newColumnExpr(types.ColumnNameGeneratedValue, COLUMN_TYPE_GENERATED)

			return columnRef.ToExpression(), projection, columnRef, nil
		}

		return expr.ToExpression(), input, inputRef, nil
	case *logical.UnaryOp:
		child, input, inputRef, err := p.collapseMathExpressions(v.Value, false, ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		expr := &UnaryExpression{
			Value: child,
			Op:    unaryOpLogToPhys(v.Op),
		}
		if rootNode {
			projection := &Projection{
				Id:          PlanNodeID{Value: ulid.New()},
				Expressions: []*Expression{expr.ToExpression()},
				All:         true,
				Expand:      true,
			}
			p.plan.Add(projection)
			if err := p.plan.AddEdge(dag.Edge[Node]{Parent: projection, Child: input}); err != nil {
				return nil, nil, nil, err
			}

			columnRef := newColumnExpr(types.ColumnNameGeneratedValue, COLUMN_TYPE_GENERATED)

			return columnRef.ToExpression(), projection, columnRef, nil
		}

		return expr.ToExpression(), input, inputRef, nil
	case *logical.Literal:
		return NewLiteral(v.Value()).ToExpression(), nil, nil, nil
	default:
		// If it is neigher a literal nor an expression, then we continue `p.process` on this node and represent in
		// as a column ref `value` in the final math expression.
		columnRef := newColumnExpr(types.ColumnNameGeneratedValue, COLUMN_TYPE_GENERATED)
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

// Convert [logical.Parse] into one [ParseNode] node.
// A ParseNode initially has an empty list of RequestedKeys which will be populated during optimization.
func (p *Planner) processParse(lp *logical.Parse, ctx *Context) (Node, error) {
	var node Node = &Parse{
		Id:        PlanNodeID{Value: ulid.New()},
		Operation: convertParserKind(lp.Kind),
	}
	p.plan.Add(node)

	child, err := p.process(lp.Table, ctx)
	if err != nil {
		return nil, err
	}

	if err := p.plan.AddEdge(dag.Edge[Node]{Parent: node, Child: child}); err != nil {
		return nil, err
	}

	if p.context.v1Compatible {
		compat := &ColumnCompat{
			Id:          PlanNodeID{Value: ulid.New()},
			Source:      COLUMN_TYPE_PARSED,
			Destination: COLUMN_TYPE_PARSED,
			Collision:   COLUMN_TYPE_LABEL,
		}
		node, err = p.wrapNodeWith(node, compat)
		if err != nil {
			return nil, err
		}
	}

	return node, nil
}

func (p *Planner) wrapNodeWith(node Node, wrapper Node) (Node, error) {
	p.plan.Add(wrapper)
	if err := p.plan.AddEdge(dag.Edge[Node]{Parent: wrapper, Child: node}); err != nil {
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

func convertParserKind(kind logical.ParserKind) ParseOp {
	switch kind {
	case logical.ParserLogfmt:
		return PARSE_OP_LOGFMT
	case logical.ParserJSON:
		return PARSE_OP_JSON
	default:
		return PARSE_OP_INVALID
	}
}

func unaryOpLogToPhys(op types.UnaryOp) UnaryOp {
	switch op {
	case types.UnaryOpAbs:
		return UNARY_OP_ABS
	case types.UnaryOpCastBytes:
		return UNARY_OP_CAST_BYTES
	case types.UnaryOpCastDuration:
		return UNARY_OP_CAST_DURATION
	case types.UnaryOpCastFloat:
		return UNARY_OP_CAST_FLOAT
	case types.UnaryOpNot:
		return UNARY_OP_NOT
	default:
		return UNARY_OP_INVALID
	}
}

func binaryOpLogToPhys(op types.BinaryOp) BinaryOp {
	switch op {
	case types.BinaryOpEq:
		return BINARY_OP_EQ
	case types.BinaryOpNeq:
		return BINARY_OP_NEQ
	case types.BinaryOpGt:
		return BINARY_OP_GT
	case types.BinaryOpGte:
		return BINARY_OP_GTE
	case types.BinaryOpLt:
		return BINARY_OP_LT
	case types.BinaryOpLte:
		return BINARY_OP_LTE

	case types.BinaryOpAnd:
		return BINARY_OP_AND
	case types.BinaryOpOr:
		return BINARY_OP_OR
	case types.BinaryOpXor:
		return BINARY_OP_XOR
	case types.BinaryOpNot:
		return BINARY_OP_NOT

	case types.BinaryOpAdd:
		return BINARY_OP_ADD
	case types.BinaryOpSub:
		return BINARY_OP_SUB
	case types.BinaryOpMul:
		return BINARY_OP_MUL
	case types.BinaryOpDiv:
		return BINARY_OP_DIV
	case types.BinaryOpMod:
		return BINARY_OP_MOD
	case types.BinaryOpPow:
		return BINARY_OP_POW

	case types.BinaryOpMatchSubstr:
		return BINARY_OP_MATCH_SUBSTR
	case types.BinaryOpNotMatchSubstr:
		return BINARY_OP_NOT_MATCH_SUBSTR
	case types.BinaryOpMatchRe:
		return BINARY_OP_MATCH_RE
	case types.BinaryOpNotMatchRe:
		return BINARY_OP_NOT_MATCH_RE
	case types.BinaryOpMatchPattern:
		return BINARY_OP_MATCH_PATTERN
	case types.BinaryOpNotMatchPattern:
		return BINARY_OP_NOT_MATCH_PATTERN
	default:
		return BINARY_OP_INVALID
	}
}

func ColumnTypeLogToPhys(colType types.ColumnType) ColumnType {
	switch colType {
	case types.ColumnTypeBuiltin:
		return COLUMN_TYPE_BUILTIN
	case types.ColumnTypeLabel:
		return COLUMN_TYPE_LABEL
	case types.ColumnTypeMetadata:
		return COLUMN_TYPE_METADATA
	case types.ColumnTypeParsed:
		return COLUMN_TYPE_PARSED
	case types.ColumnTypeAmbiguous:
		return COLUMN_TYPE_AMBIGUOUS
	case types.ColumnTypeGenerated:
		return COLUMN_TYPE_GENERATED
	default:
		return COLUMN_TYPE_INVALID
	}
}

func rangeAggregationTypeLogToPhys(rangeAggType types.RangeAggregationType) AggregateRangeOp {
	switch rangeAggType {
	case types.RangeAggregationTypeCount:
		return AGGREGATE_RANGE_OP_COUNT
	case types.RangeAggregationTypeSum:
		return AGGREGATE_RANGE_OP_SUM
	case types.RangeAggregationTypeMax:
		return AGGREGATE_RANGE_OP_MAX
	case types.RangeAggregationTypeMin:
		return AGGREGATE_RANGE_OP_MIN
	default:
		return AGGREGATE_RANGE_OP_INVALID
	}
}

func vectorAggregationTypeLogToPhys(vectorAggType types.VectorAggregationType) AggregateVectorOp {
	switch vectorAggType {
	case types.VectorAggregationTypeSum:
		return AGGREGATE_VECTOR_OP_SUM
	case types.VectorAggregationTypeMax:
		return AGGREGATE_VECTOR_OP_MAX
	case types.VectorAggregationTypeMin:
		return AGGREGATE_VECTOR_OP_MIN
	case types.VectorAggregationTypeCount:
		return AGGREGATE_VECTOR_OP_COUNT
	case types.VectorAggregationTypeAvg:
		return AGGREGATE_VECTOR_OP_AVG
	case types.VectorAggregationTypeStddev:
		return AGGREGATE_VECTOR_OP_STDDEV
	case types.VectorAggregationTypeStdvar:
		return AGGREGATE_VECTOR_OP_STDVAR
	case types.VectorAggregationTypeBottomK:
		return AGGREGATE_VECTOR_OP_BOTTOMK
	case types.VectorAggregationTypeTopK:
		return AGGREGATE_VECTOR_OP_TOPK
	case types.VectorAggregationTypeSort:
		return AGGREGATE_VECTOR_OP_SORT
	case types.VectorAggregationTypeSortDesc:
		return AGGREGATE_VECTOR_OP_SORT_DESC
	default:
		return AGGREGATE_VECTOR_OP_INVALID
	}
}
