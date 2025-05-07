package executor

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

// buildLogsPredicate builds a logs predicate from an expression.
func buildLogsPredicate(_ physical.Expression) (dataobj.LogsPredicate, error) {
	// TODO(rfratto): implement converting expressions into logs predicates.
	//
	// There's a few challenges here:
	//
	// - Expressions do not cleanly map to [dataobj.LogsPredicate]s. For example,
	//   an expression may be simply a column reference, but a logs predicate is
	//   always some expression that can evaluate to true.
	//
	// - Mapping expressions into [dataobj.TimeRangePredicate] is a massive pain;
	//   since TimeRangePredicate specifies both bounds for the time range, we
	//   would need to find and collapse multiple physical.Expressions into a
	//   single TimeRangePredicate.
	//
	// - While [dataobj.MetadataMatcherPredicate] and
	//   [dataobj.LogMessageFilterPredicate] are catch-alls for function-based
	//   predicates, they are row-based and not column-based, so our
	//   expressionEvaluator cannot be used here.
	//
	// Long term, we likely want two things:
	//
	// 1. Use dataset.Reader and dataset.Predicate directly instead of
	//    dataobj.LogsReader.
	//
	// 2. Update dataset.Reader to be vector based instead of row-based.
	//
	// It's not clear if we should resolve the issues with LogsPredicate (or find
	// hacks to make them work in the short term), or skip straight to using
	// dataset.Reader instead.
	//
	// Implementing DataObjScan in the dataobj package would be a clean way to
	// handle all of this, but that would cause cyclic dependencies. I also don't
	// think we should start removing things from internal for this; we can probably
	// find a way to remove the explicit dependency from the dataobj package from
	// the physical planner instead.
	return nil, fmt.Errorf("logs predicate conversion is not supported")
}
