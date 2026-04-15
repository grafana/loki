package physical

import (
	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// aggregationSplit is a rule that splits range aggregation nodes into parallel
// pieces by injecting a [Parallelize] node between two [RangeAggregation] nodes.
//
// Supported transformations:
//   - max: max -> parallelize -> max
//   - min: min -> parallelize -> min
//   - sum: sum -> parallelize -> sum
//   - count: sum -> parallelize -> count
//
// This rule runs after [parallelPushdown] and is skipped for nodes that have
// already been pushed into a [Parallelize] by that rule. When [parallelPushdown]
// leaves a [Parallelize] as a direct child (because it could not shift the node
// itself), this rule reuses that existing [Parallelize].
type aggregationSplit struct {
	plan  *Plan
	split map[Node]struct{}
}

var _ rule = (*aggregationSplit)(nil)

func (a *aggregationSplit) apply(root Node) bool {
	if a.split == nil {
		a.split = make(map[Node]struct{})
	}

	nodes := findMatchingNodes(a.plan, root, func(node Node) bool {
		rangeAgg, ok := node.(*RangeAggregation)
		if !ok {
			return false
		}
		// Skip nodes already processed by this rule.
		if _, done := a.split[node]; done {
			return false
		}
		// Skip if already pushed down by ParallelPushdown.
		if a.isAlreadyParallelized(rangeAgg) {
			return false
		}
		return canSplitRangeAggregation(rangeAgg)
	})

	changed := false
	for _, node := range nodes {
		a.applySplit(node.(*RangeAggregation))
		changed = true
	}
	return changed
}

// isAlreadyParallelized returns true if the given node has a [Parallelize]
// parent, indicating [parallelPushdown] has already pushed it into a parallel
// region. Because this rule runs after [parallelPushdown], any [Parallelize]
// that was previously deeper in the subtree will already have had intermediate
// nodes (e.g. [ColumnCompat]) shifted below it, leaving it as either the
// parent (node was shifted) or the direct child (node could not be shifted) of
// a [RangeAggregation].
func (a *aggregationSplit) isAlreadyParallelized(node Node) bool {
	for _, parent := range a.plan.Parent(node) {
		if parent.Type() == NodeTypeParallelize {
			return true
		}
	}
	return false
}

// applySplit transforms a single [RangeAggregation] into:
//
//	outerRangeAgg -> Parallelize -> innerRangeAgg
//
// The original node becomes the outer aggregation (with potentially modified
// operation for count), and a clone becomes the inner (partial) aggregation.
//
// If a [Parallelize] is already a direct child of the node (left there by
// [parallelPushdown] when it could not shift the node), it is reused rather
// than a new one being created.
func (a *aggregationSplit) applySplit(rangeAgg *RangeAggregation) {
	// Clone the original to use as the inner (partial) aggregation.
	inner := rangeAgg.Clone().(*RangeAggregation)

	// Reuse an existing direct-child Parallelize if present; otherwise inject a
	// new one between rangeAgg and its children.
	var parallelize Node
	for _, child := range a.plan.Children(rangeAgg) {
		if child.Type() == NodeTypeParallelize {
			parallelize = child
			break
		}
	}
	if parallelize == nil {
		parallelize = &Parallelize{NodeID: ulid.Make()}
		a.plan.graph.Inject(rangeAgg, parallelize)
	}

	// Inject the inner clone between parallelize and its children.
	a.plan.graph.Inject(parallelize, inner)

	// For count, the outer aggregation sums the partial counts.
	if rangeAgg.Operation == types.RangeAggregationTypeCount {
		rangeAgg.Operation = types.RangeAggregationTypeSum
	}
	// max, min, and sum outer operations are unchanged.

	// Set the outer range to the step size so that each outer window captures
	// exactly one inner result point.
	//
	// The inner produces one point per step at timestamp t, representing the
	// aggregation of raw data in (t-Range, t]. Consecutive inner points are
	// Step apart. With outer.Range=Step, the outer window (t-Step, t] always
	// contains exactly the inner point at t and excludes the point at t-Step
	// (exclusive window start). This holds for all three window regimes:
	//   - overlapping (Step < Range): avoids collecting multiple inner points
	//   - aligned (Step == Range):    outer.Range unchanged, already correct
	//   - gapped (Step > Range):      outer.Range widens to Step, still one point
	//
	// Instant queries (Step == 0) are unaffected: there is only one window and
	// one inner point at End, so any positive Range is correct.
	if rangeAgg.Step > 0 {
		rangeAgg.Range = rangeAgg.Step
	}

	a.split[rangeAgg] = struct{}{}
}

// canSplitRangeAggregation returns true if the range aggregation operation
// can be split into parallel pieces.
func canSplitRangeAggregation(rangeAgg *RangeAggregation) bool {
	// Splitting aggregations with overlapping windows (Step < Range) can
	// lead to traffic amplification (each raw datapoint can produce several
	// aggregated datapoints from inner aggregation). However, if `by (...)`
	// grouping is narrow (few labels) it should theoretically aggregate a lot
	// of streams into a few datapoints. For now just skip all `without` groupings
	// and `by` groupings with 5+ labels.
	// TODO(spiridonov): Think if there is a better way to estimate amplification.
	if rangeAgg.Step > 0 && rangeAgg.Step < rangeAgg.Range &&
		(rangeAgg.Grouping.Without || len(rangeAgg.Grouping.Columns) > 4) {
		return false
	}

	// Supported aggregation operations
	switch rangeAgg.Operation {
	case types.RangeAggregationTypeMax,
		types.RangeAggregationTypeMin,
		types.RangeAggregationTypeSum,
		types.RangeAggregationTypeCount:
		return true
	}
	return false
}
