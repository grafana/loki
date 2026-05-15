package physical

import (
	"slices"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// parallelPushdown is a rule that moves or splits supported operations as a
// child of [Parallelize] to parallelize as much work as possible.
type parallelPushdown struct {
	plan   *Plan
	pushed map[Node]struct{}
}

var _ rule = (*parallelPushdown)(nil)

func (p *parallelPushdown) apply(root Node) bool {
	if p.pushed == nil {
		p.pushed = make(map[Node]struct{})
	}

	// find all nodes that can be parallelized
	nodes := findMatchingNodes(p.plan, root, func(node Node) bool {
		if _, ok := p.pushed[node]; ok {
			return false
		}

		// canPushdown only returns true if all children of node are [Parallelize].
		return p.canPushdown(node)
	})

	// apply parallel pushdown to each node
	changed := false
	for _, node := range nodes {
		if p.applyParallelization(node) {
			changed = true
		}
	}

	return changed
}

func (p *parallelPushdown) applyParallelization(node Node) bool {
	// There are two catchall cases here:
	//
	// 1. Nodes which get *shifted* down into a parallel pushdown, where the
	//    positions of the node and the Parallelize swap.
	//
	//    For example, filtering gets moved down to be parallelized.
	//
	// 2. Nodes which get *sharded* into a parallel pushdown, where a copy of
	//    the node is injected into each child of the Parallelize.
	//
	//    For example, a TopK gets copied for local TopK, which is then merged back
	//    up to the parent TopK.
	//
	// There can be additional special cases, such as parallelizing an `avg` by
	// pushing down a `sum` and `count` into the Parallelize.
	switch node := node.(type) {
	case *Projection, *Filter, *ColumnCompat: // Catchall for shifting nodes
		for _, parallelize := range p.plan.Children(node) {
			p.plan.graph.Inject(parallelize, node.Clone())
		}
		p.plan.graph.Eliminate(node)
		p.pushed[node] = struct{}{}
		return true

	case *TopK: // shard TopK
		for _, parallelize := range p.plan.Children(node) {
			p.plan.graph.Inject(parallelize, node.Clone())
		}
		p.pushed[node] = struct{}{}
		return true

	case *RangeAggregation:
		vecAgg := p.findParentVectorAggregation(node)
		if vecAgg == nil {
			return false // No VectorAgg parent - don't shift
		}

		// RangeAggregation can be parallelized only when the operation
		// is associative and commutative with the parent vector aggregation.
		if !p.canShardAggregation(vecAgg, node) {
			return false
		}

		// Shift: move RangeAgg into Parallelize, eliminate original
		for _, parallelize := range p.plan.Children(node) {
			p.plan.graph.Inject(parallelize, node.Clone())
		}
		p.plan.graph.Eliminate(node)
		p.pushed[node] = struct{}{}
		return true
	}

	return false
}

// canPushdown returns true if the given node has children that are all of type
// [NodeTypeParallelize]. Nodes with no children are not supported.
func (p *parallelPushdown) canPushdown(node Node) bool {
	children := p.plan.Children(node)
	if len(children) == 0 {
		// Must have at least one child.
		return false
	}

	// foundNonParallelize is true if there is at least one child that is not of
	// type [NodeTypeParallelize].
	foundNonParallelize := slices.ContainsFunc(children, func(n Node) bool {
		return n.Type() != NodeTypeParallelize
	})
	return !foundNonParallelize
}

// findParentVectorAggregation finds the VectorAggregation node that is a parent
// of the given node (typically a RangeAggregation). Returns nil if no parent
// VectorAggregation exists.
func (p *parallelPushdown) findParentVectorAggregation(node Node) *VectorAggregation {
	for _, parent := range p.plan.Parent(node) {
		if vecAgg, ok := parent.(*VectorAggregation); ok {
			return vecAgg
		}
	}
	return nil
}

// canShardAggregation returns true if the combination of vector and
// range aggregation operations can be safely parallelized.
//
// This is valid for aggregate functions that are both associative and commutative.
//
// Supported combinations:
//   - sum over sum/count: additive operations can be summed across partitions
//   - max over max: max of local maxes equals global max
//   - min over min: min of local mins equals global min
func (p *parallelPushdown) canShardAggregation(vec *VectorAggregation, rng *RangeAggregation) bool {
	// without grouping is not pushed down.
	if vec.Grouping.Without || rng.Grouping.Without {
		return false
	}

	// range aggregation's grouping must preserve all labels that the
	// vector aggregation needs. Currently we require exact match as
	// a conservative simplification.
	if len(vec.Grouping.Columns) != len(rng.Grouping.Columns) {
		return false
	}

	for i, vecCol := range vec.Grouping.Columns {
		rngCol := rng.Grouping.Columns[i]

		colExprA, okA := vecCol.(*ColumnExpr)
		colExprB, okB := rngCol.(*ColumnExpr)
		if !okA || !okB {
			return false
		}

		if colExprA.Ref.Column != colExprB.Ref.Column {
			return false
		}
	}

	switch vec.Operation {
	case types.VectorAggregationTypeSum:
		return rng.Operation == types.RangeAggregationTypeSum || rng.Operation == types.RangeAggregationTypeCount
	case types.VectorAggregationTypeMax:
		return rng.Operation == types.RangeAggregationTypeMax
	case types.VectorAggregationTypeMin:
		return rng.Operation == types.RangeAggregationTypeMin
	}
	return false
}
