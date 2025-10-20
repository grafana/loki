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
			nodes, err := p.process(inst.Value, p.context)
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

// reset resets the internal state of the planner
func (p *Planner) reset() {
	p.plan = &Plan{}
}

// Convert a predicate from an [logical.Instruction] into an [Expression].
func (p *Planner) convertPredicate(inst logical.Value) *physicalpb.Expression {
	switch inst := inst.(type) {
	case *logical.UnaryOp:
		return UnaryExpressionToExpression(&physicalpb.UnaryExpression{
			Value: p.convertPredicate(inst.Value),
			Op:    inst.Op,
		})
	case *logical.BinOp:
		return BinaryExpressionToExpression(&physicalpb.BinaryExpression{
			Left:  p.convertPredicate(inst.Left),
			Right: p.convertPredicate(inst.Right),
			Op:    inst.Op,
		})
	case *logical.ColumnRef:
		return ColumnExpressionToExpression(&physicalpb.ColumnExpression{Name: inst.Ref.Column, Type: inst.Ref.Type})
	case *logical.Literal:
		return LiteralExpressionToExpression(NewLiteral(inst.Value()))
	default:
		panic(fmt.Sprintf("invalid value for predicate: %T", inst))
	}
}

// Convert a [logical.Instruction] into one or multiple [Node]s.
func (p *Planner) process(inst logical.Value, ctx *Context) ([]physicalpb.Node, error) {
	switch inst := inst.(type) {
	case *logical.MakeTable:
		return p.processMakeTable(inst, ctx)
	case *logical.Select:
		return p.processSelect(inst, ctx)
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
	case *logical.LogQLCompat:
		p.context.v1Compatible = true
		return p.process(inst.Value, ctx)
	}
	return nil, nil
}

func (p *Planner) buildNodeGroup(currentGroup []FilteredShardDescriptor, baseNode physicalpb.Node, ctx *Context) error {
	scans := []physicalpb.Node{}
	for _, descriptor := range currentGroup {
		// output current group to nodes
		for _, section := range descriptor.Sections {
			scan := &physicalpb.DataObjScan{
				Location:  string(descriptor.Location),
				StreamIds: descriptor.Streams,
				Section:   int64(section),
				SortOrder: ctx.direction,
			}
			p.plan.graph.Add(scan)
			scans = append(scans, scan)
		}
	}
	if len(scans) > 1 && ctx.direction != physicalpb.SORT_ORDER_INVALID {
		sortMerge := &physicalpb.SortMerge{
			Column: newColumnExpr(types.ColumnNameBuiltinTimestamp, physicalpb.COLUMN_TYPE_BUILTIN),
			Order:  ctx.direction, // apply direction from previously visited Sort node
		}
		p.plan.graph.Add(sortMerge)
		for _, scan := range scans {
			if err := p.plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: sortMerge, Child: scan}); err != nil {
				return err
			}
		}
		if err := p.plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: baseNode, Child: sortMerge}); err != nil {
			return err
		}
	} else {
		for _, scan := range scans {
			if err := p.plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: baseNode, Child: scan}); err != nil {
				return err
			}
		}
	}
	return nil
}

func overlappingShardDescriptors(filteredShardDescriptors []FilteredShardDescriptor) [][]FilteredShardDescriptor {
	// Ensure that shard descriptors are sorted by end time
	sort.Slice(filteredShardDescriptors, func(i, j int) bool {
		return filteredShardDescriptors[i].TimeRange.End.After(filteredShardDescriptors[j].TimeRange.End)
	})

	groups := make([][]FilteredShardDescriptor, 0, len(filteredShardDescriptors))
	var tr TimeRange
	for i, shardDesc := range filteredShardDescriptors {
		if i == 0 || !tr.Overlaps(shardDesc.TimeRange) {
			// Create new group for first item or if item does not overlap with previous group
			groups = append(groups, []FilteredShardDescriptor{shardDesc})
			tr = shardDesc.TimeRange
		} else {
			// Append to existing group
			groups[len(groups)-1] = append(groups[len(groups)-1], shardDesc)
			tr = tr.Merge(shardDesc.TimeRange)
		}
	}
	return groups
}

// Convert [logical.MakeTable] into one or more [DataObjScan] nodes.
func (p *Planner) processMakeTable(lp *logical.MakeTable, ctx *Context) ([]physicalpb.Node, error) {
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

	groups := overlappingShardDescriptors(filteredShardDescriptors)
	if ctx.direction == physicalpb.SORT_ORDER_ASCENDING {
		slices.Reverse(groups)
	}

	var node physicalpb.Node = &physicalpb.Merge{}
	p.plan.graph.Add(node)
	for _, gr := range groups {
		if err := p.buildNodeGroup(gr, node, ctx); err != nil {
			return nil, err
		}
	}

	if p.context.v1Compatible {
		compat := &physicalpb.ColumnCompat{
			Source:      physicalpb.COLUMN_TYPE_METADATA,
			Destination: physicalpb.COLUMN_TYPE_METADATA,
			Collision:   physicalpb.COLUMN_TYPE_LABEL,
		}
		node, err = p.wrapNodeWith(node, compat)
		if err != nil {
			return nil, err
		}
	}

	return []physicalpb.Node{node}, nil
}

// Convert [logical.Select] into one [Filter] node.
func (p *Planner) processSelect(lp *logical.Select, ctx *Context) ([]physicalpb.Node, error) {
	node := &physicalpb.Filter{
		Predicates: []*physicalpb.Expression{p.convertPredicate(lp.Predicate)},
	}
	p.plan.graph.Add(node)
	children, err := p.process(lp.Table, ctx)
	if err != nil {
		return nil, err
	}
	for i := range children {
		if err := p.plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: node, Child: children[i]}); err != nil {
			return nil, err
		}
	}
	return []physicalpb.Node{node}, nil
}

// Pass sort direction from [logical.Sort] to the children.
func (p *Planner) processSort(lp *logical.Sort, ctx *Context) ([]physicalpb.Node, error) {
	order := physicalpb.SORT_ORDER_DESCENDING
	if lp.Ascending {
		order = physicalpb.SORT_ORDER_ASCENDING
	}

	return p.process(lp.Table, ctx.WithDirection(order))
}

// Convert [logical.Limit] into one [Limit] node.
func (p *Planner) processLimit(lp *logical.Limit, ctx *Context) ([]physicalpb.Node, error) {
	node := &physicalpb.Limit{
		Skip:  lp.Skip,
		Fetch: lp.Fetch,
	}
	p.plan.graph.Add(node)
	children, err := p.process(lp.Table, ctx)
	if err != nil {
		return nil, err
	}
	for i := range children {
		if err := p.plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: node, Child: children[i]}); err != nil {
			return nil, err
		}
	}
	return []physicalpb.Node{node}, nil
}

func (p *Planner) processRangeAggregation(r *logical.RangeAggregation, ctx *Context) ([]physicalpb.Node, error) {
	partitionBy := make([]*physicalpb.ColumnExpression, len(r.PartitionBy))
	for i, col := range r.PartitionBy {
		partitionBy[i] = &physicalpb.ColumnExpression{Name: col.Name()}
	}

	node := &physicalpb.AggregateRange{
		PartitionBy:    partitionBy,
		Operation:      r.Operation,
		StartUnixNanos: r.Start.UnixNano(),
		EndUnixNanos:   r.End.UnixNano(),
		RangeNs:        r.RangeInterval.Nanoseconds(),
		StepNs:         r.Step.Nanoseconds(),
	}
	p.plan.graph.Add(node)

	children, err := p.process(r.Table, ctx.WithRangeInterval(r.RangeInterval))
	if err != nil {
		return nil, err
	}

	for i := range children {
		if err := p.plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: node, Child: children[i]}); err != nil {
			return nil, err
		}
	}
	return []physicalpb.Node{node}, nil
}

// Convert [logical.VectorAggregation] into one [VectorAggregation] node.
func (p *Planner) processVectorAggregation(lp *logical.VectorAggregation, ctx *Context) ([]physicalpb.Node, error) {
	groupBy := make([]*physicalpb.ColumnExpression, len(lp.GroupBy))
	for i, col := range lp.GroupBy {
		groupBy[i] = &physicalpb.ColumnExpression{Name: col.Ref.Column, Type: col.Ref.Type}
	}

	node := &physicalpb.AggregateVector{
		GroupBy:   groupBy,
		Operation: lp.Operation,
	}
	p.plan.graph.Add(node)
	children, err := p.process(lp.Table, ctx)
	if err != nil {
		return nil, err
	}
	for i := range children {
		if err := p.plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: node, Child: children[i]}); err != nil {
			return nil, err
		}
	}
	return []physicalpb.Node{node}, nil
}

// Convert [logical.Parse] into one [ParseNode] node.
// A ParseNode initially has an empty list of RequestedKeys which will be populated during optimization.
func (p *Planner) processParse(lp *logical.Parse, ctx *Context) ([]physicalpb.Node, error) {
	var node physicalpb.Node = &physicalpb.Parse{
		Operation: convertParserKind(lp.Kind),
	}
	p.plan.graph.Add(node)

	children, err := p.process(lp.Table, ctx)
	if err != nil {
		return nil, err
	}

	for i := range children {
		if err := p.plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: node, Child: children[i]}); err != nil {
			return nil, err
		}
	}

	if p.context.v1Compatible {
		compat := &physicalpb.ColumnCompat{
			Source:      physicalpb.COLUMN_TYPE_PARSED,
			Destination: physicalpb.COLUMN_TYPE_PARSED,
			Collision:   physicalpb.COLUMN_TYPE_LABEL,
		}
		node, err = p.wrapNodeWith(node, compat)
		if err != nil {
			return nil, err
		}
	}

	return []physicalpb.Node{node}, nil
}

func (p *Planner) wrapNodeWith(node physicalpb.Node, wrapper physicalpb.Node) (physicalpb.Node, error) {
	p.plan.graph.Add(wrapper)
	if err := p.plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: wrapper, Child: node}); err != nil {
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
			newOptimization("CleanupFilters", plan).withRules(
				&removeNoopFilter{plan: plan},
			),
			newOptimization("LimitPushdown", plan).withRules(
				&limitPushdown{plan: plan},
			),
			newOptimization("ProjectionPushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
			newOptimization("CleanupMerge", plan).withRules(
				&removeNoopMerge{plan: plan},
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
