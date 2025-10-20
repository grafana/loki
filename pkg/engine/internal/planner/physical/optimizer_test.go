package physical

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/ulid"
)

func TestCanApplyPredicate(t *testing.T) {
	tests := []struct {
		predicate physicalpb.Expression
		want      bool
	}{
		{
			predicate: *LiteralExpressionToExpression(NewLiteral(int64(123))),
			want:      true,
		},
		{
			predicate: *ColumnExpressionToExpression(newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)),
			want:      true,
		},
		{
			predicate: *ColumnExpressionToExpression(newColumnExpr("foo", physicalpb.COLUMN_TYPE_LABEL)),
			want:      false,
		},
		{
			predicate: *BinaryExpressionToExpression(
				newBinaryExpr(ColumnExpressionToExpression(newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)),
					LiteralExpressionToExpression(NewLiteral(types.Timestamp(3600000))),
					physicalpb.BINARY_OP_GT,
				)),
			want: true,
		},
		{
			predicate: *BinaryExpressionToExpression(
				newBinaryExpr(ColumnExpressionToExpression(newColumnExpr("level", physicalpb.COLUMN_TYPE_AMBIGUOUS)),
					LiteralExpressionToExpression(NewLiteral("debug|info")),
					physicalpb.BINARY_OP_MATCH_RE,
				)),
			want: false,
		},
		{
			predicate: *BinaryExpressionToExpression(
				newBinaryExpr(ColumnExpressionToExpression(newColumnExpr("level", physicalpb.COLUMN_TYPE_METADATA)),
					LiteralExpressionToExpression(NewLiteral("debug|info")),
					physicalpb.BINARY_OP_MATCH_RE,
				)),
			want: true,
		},
		{
			predicate: *BinaryExpressionToExpression(
				newBinaryExpr(ColumnExpressionToExpression(newColumnExpr("foo", physicalpb.COLUMN_TYPE_LABEL)),
					LiteralExpressionToExpression(NewLiteral("bar")),
					physicalpb.BINARY_OP_EQ,
				)),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.predicate.String(), func(t *testing.T) {
			got := canApplyPredicate(tt.predicate)
			require.Equal(t, tt.want, got)
		})
	}
}

var time1000 = types.Timestamp(1000000000)

var scan1Id = physicalpb.PlanNodeID{Value: ulid.New()}
var scan2Id = physicalpb.PlanNodeID{Value: ulid.New()}
var mergeId = physicalpb.PlanNodeID{Value: ulid.New()}
var filter1Id = physicalpb.PlanNodeID{Value: ulid.New()}
var filter2Id = physicalpb.PlanNodeID{Value: ulid.New()}
var filter3Id = physicalpb.PlanNodeID{Value: ulid.New()}

func dummyPlan() *Plan {
	plan := &Plan{}
	scan1 := plan.graph.Add(&physicalpb.DataObjScan{Id: scan1Id})
	scan2 := plan.graph.Add(&physicalpb.DataObjScan{Id: scan2Id})
	merge := plan.graph.Add(&physicalpb.SortMerge{Id: mergeId})
	filter1 := plan.graph.Add(&physicalpb.Filter{Id: filter1Id, Predicates: []*physicalpb.Expression{
		BinaryExpressionToExpression(
			newBinaryExpr(ColumnExpressionToExpression(newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)),
				LiteralExpressionToExpression(NewLiteral(time1000)),
				physicalpb.BINARY_OP_GT,
			)),
	}})
	filter2 := plan.graph.Add(&physicalpb.Filter{Id: filter2Id, Predicates: []*physicalpb.Expression{
		BinaryExpressionToExpression(
			newBinaryExpr(
				ColumnExpressionToExpression(newColumnExpr("level", physicalpb.COLUMN_TYPE_AMBIGUOUS)),
				LiteralExpressionToExpression(NewLiteral("debug|info")),
				physicalpb.BINARY_OP_MATCH_RE,
			)),
	}})
	filter3 := plan.graph.Add(&physicalpb.Filter{Id: filter3Id, Predicates: []*physicalpb.Expression{}})

	_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter3, Child: filter2})
	_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter2, Child: filter1})
	_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter1, Child: merge})
	_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: merge, Child: scan1})
	_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: merge, Child: scan2})

	return plan
}

func TestOptimizer(t *testing.T) {

	t.Run("noop", func(t *testing.T) {
		plan := dummyPlan()
		optimizations := []*optimization{
			newOptimization("noop", plan),
		}

		original := PrintAsTree(plan)
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		optimized := PrintAsTree(plan)
		require.Equal(t, original, optimized)
	})

	t.Run("filter predicate pushdown", func(t *testing.T) {
		plan := dummyPlan()
		optimizations := []*optimization{
			newOptimization("predicate pushdown", plan).withRules(
				&predicatePushdown{plan},
			),
		}

		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])
		actual := PrintAsTree(plan)

		optimized := &Plan{}
		scan1 := optimized.graph.Add(&physicalpb.DataObjScan{Id: scan1Id, Predicates: []*physicalpb.Expression{
			BinaryExpressionToExpression(
				newBinaryExpr(
					ColumnExpressionToExpression(newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)),
					LiteralExpressionToExpression(NewLiteral(time1000)),
					physicalpb.BINARY_OP_GT,
				)),
		}})
		scan2 := optimized.graph.Add(&physicalpb.DataObjScan{Id: scan2Id, Predicates: []*physicalpb.Expression{
			BinaryExpressionToExpression(
				newBinaryExpr(
					ColumnExpressionToExpression(newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)),
					LiteralExpressionToExpression(NewLiteral(time1000)),
					physicalpb.BINARY_OP_GT,
				)),
		}})
		merge := optimized.graph.Add(&physicalpb.SortMerge{Id: mergeId})
		filter1 := optimized.graph.Add(&physicalpb.Filter{Id: filter1Id, Predicates: []*physicalpb.Expression{}})
		filter2 := optimized.graph.Add(&physicalpb.Filter{Id: filter2Id, Predicates: []*physicalpb.Expression{
			BinaryExpressionToExpression(
				newBinaryExpr(
					ColumnExpressionToExpression(newColumnExpr("level", physicalpb.COLUMN_TYPE_AMBIGUOUS)),
					LiteralExpressionToExpression(NewLiteral("debug|info")),
					physicalpb.BINARY_OP_MATCH_RE,
				)),
		}})
		filter3 := optimized.graph.Add(&physicalpb.Filter{Id: filter3Id, Predicates: []*physicalpb.Expression{}})

		_ = optimized.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter3, Child: filter2})
		_ = optimized.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter2, Child: filter1})
		_ = optimized.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter1, Child: merge})
		_ = optimized.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: merge, Child: scan1})
		_ = optimized.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: merge, Child: scan2})

		expected := PrintAsTree(optimized)
		require.Equal(t, expected, actual)
	})

	t.Run("filter remove", func(t *testing.T) {
		plan := dummyPlan()
		optimizations := []*optimization{
			newOptimization("noop filter", plan).withRules(
				&removeNoopFilter{plan},
			),
		}

		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])
		actual := PrintAsTree(plan)

		optimized := &Plan{}
		scan1 := optimized.graph.Add(&physicalpb.DataObjScan{Id: scan1Id, Predicates: []*physicalpb.Expression{}})
		scan2 := optimized.graph.Add(&physicalpb.DataObjScan{Id: scan2Id, Predicates: []*physicalpb.Expression{}})
		merge := optimized.graph.Add(&physicalpb.SortMerge{Id: mergeId})
		filter1 := optimized.graph.Add(&physicalpb.Filter{Id: filter1Id, Predicates: []*physicalpb.Expression{
			BinaryExpressionToExpression(
				newBinaryExpr(
					ColumnExpressionToExpression(newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)),
					LiteralExpressionToExpression(NewLiteral(time1000)),
					physicalpb.BINARY_OP_GT,
				)),
		}})
		filter2 := optimized.graph.Add(&physicalpb.Filter{Id: filter2Id, Predicates: []*physicalpb.Expression{
			BinaryExpressionToExpression(
				newBinaryExpr(
					ColumnExpressionToExpression(newColumnExpr("level", physicalpb.COLUMN_TYPE_AMBIGUOUS)),
					LiteralExpressionToExpression(NewLiteral("debug|info")),
					physicalpb.BINARY_OP_MATCH_RE,
				)),
		}})

		_ = optimized.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter2, Child: filter1})
		_ = optimized.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter1, Child: merge})
		_ = optimized.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: merge, Child: scan1})
		_ = optimized.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: merge, Child: scan2})

		expected := PrintAsTree(optimized)
		require.Equal(t, expected, actual)
	})

	t.Run("projection pushdown handles groupby for SUM->COUNT", func(t *testing.T) {
		countOverTimeId := physicalpb.PlanNodeID{Value: ulid.New()}
		sumOfId := physicalpb.PlanNodeID{Value: ulid.New()}

		groupBy := []*physicalpb.ColumnExpression{
			newColumnExpr("service", physicalpb.COLUMN_TYPE_LABEL),
			newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL),
		}

		// generate plan for sum by(service, instance) (count_over_time{...}[])
		plan := &Plan{}
		{
			scan1 := plan.graph.Add(&physicalpb.DataObjScan{Id: scan1Id})
			rangeAgg := plan.graph.Add(&physicalpb.AggregateRange{
				Id:        countOverTimeId,
				Operation: physicalpb.AGGREGATE_RANGE_OP_COUNT,
			})
			vectorAgg := plan.graph.Add(&physicalpb.AggregateVector{
				Id:        sumOfId,
				Operation: physicalpb.AGGREGATE_VECTOR_OP_SUM,
				GroupBy:   groupBy,
			})

			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: vectorAgg, Child: rangeAgg})
			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg, Child: scan1})
		}

		// apply optimisation
		optimizations := []*optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			// pushed down from group and partition by, with range aggregations adding timestamp
			expectedProjections := []*physicalpb.ColumnExpression{
				newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL),
				newColumnExpr("service", physicalpb.COLUMN_TYPE_LABEL),
				newColumnExpr(types.ColumnNameBuiltinTimestamp, physicalpb.COLUMN_TYPE_BUILTIN),
			}

			scan1 := expectedPlan.graph.Add(&physicalpb.DataObjScan{Id: scan1Id, Projections: expectedProjections})
			rangeAgg := expectedPlan.graph.Add(&physicalpb.AggregateRange{
				Id:          countOverTimeId,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: groupBy,
			})
			vectorAgg := expectedPlan.graph.Add(&physicalpb.AggregateVector{
				Id:        sumOfId,
				Operation: physicalpb.AGGREGATE_VECTOR_OP_SUM,
				GroupBy:   groupBy,
			})

			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: vectorAgg, Child: rangeAgg})
			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg, Child: scan1})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("projection pushdown does not handle groupby for MAX->SUM", func(t *testing.T) {
		sumOverTimeId := physicalpb.PlanNodeID{Value: ulid.New()}
		maxOfId := physicalpb.PlanNodeID{Value: ulid.New()}
		groupBy := []*physicalpb.ColumnExpression{
			newColumnExpr("service", physicalpb.COLUMN_TYPE_LABEL),
		}

		partitionBy := []*physicalpb.ColumnExpression{
			newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL),
		}

		// generate plan for max by(service) (sum_over_time{...}[])
		plan := &Plan{}
		{
			scan1 := plan.graph.Add(&physicalpb.DataObjScan{Id: scan1Id})
			rangeAgg := plan.graph.Add(&physicalpb.AggregateRange{
				Id:          sumOverTimeId,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_SUM,
				PartitionBy: partitionBy,
			})
			vectorAgg := plan.graph.Add(&physicalpb.AggregateVector{
				Id:        maxOfId,
				Operation: physicalpb.AGGREGATE_VECTOR_OP_MAX,
				GroupBy:   groupBy,
			})

			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: vectorAgg, Child: rangeAgg})
			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg, Child: scan1})
		}

		// apply optimisation
		optimizations := []*optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			// groupby was not pushed down
			expectedProjections := []*physicalpb.ColumnExpression{
				newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL),
				newColumnExpr(types.ColumnNameBuiltinTimestamp, physicalpb.COLUMN_TYPE_BUILTIN),
			}

			scan1 := expectedPlan.graph.Add(&physicalpb.DataObjScan{Id: scan1Id, Projections: expectedProjections})
			rangeAgg := expectedPlan.graph.Add(&physicalpb.AggregateRange{
				Id:          sumOverTimeId,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_SUM,
				PartitionBy: partitionBy,
			})
			vectorAgg := expectedPlan.graph.Add(&physicalpb.AggregateVector{
				Id:        maxOfId,
				Operation: physicalpb.AGGREGATE_VECTOR_OP_MAX,
				GroupBy:   groupBy,
			})

			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: vectorAgg, Child: rangeAgg})
			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg, Child: scan1})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("projection pushdown handles partition by", func(t *testing.T) {
		range1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		partitionBy := []*physicalpb.ColumnExpression{
			newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL),
			newColumnExpr("service", physicalpb.COLUMN_TYPE_LABEL),
		}

		plan := &Plan{}
		{
			scan1 := plan.graph.Add(&physicalpb.DataObjScan{
				Id: scan1Id,
			})
			scan2 := plan.graph.Add(&physicalpb.DataObjScan{
				Id: scan2Id,
			})
			rangeAgg := plan.graph.Add(&physicalpb.AggregateRange{
				Id:          range1Id,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: partitionBy,
			})

			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg, Child: scan1})
			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg, Child: scan2})
		}

		// apply optimisations
		optimizations := []*optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			projected := append(partitionBy, newColumnExpr(types.ColumnNameBuiltinTimestamp, physicalpb.COLUMN_TYPE_BUILTIN))
			scan1 := expectedPlan.graph.Add(&physicalpb.DataObjScan{
				Id:          scan1Id,
				Projections: projected,
			})
			scan2 := expectedPlan.graph.Add(&physicalpb.DataObjScan{
				Id:          scan2Id,
				Projections: projected,
			})

			rangeAgg := expectedPlan.graph.Add(&physicalpb.AggregateRange{
				Id:          range1Id,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: partitionBy,
			})

			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg, Child: scan1})
			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg, Child: scan2})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("predicate column projection pushdown with existing projections", func(t *testing.T) {
		// Predicate columns should be projected when there are existing projections (metric query)
		range1Id := physicalpb.PlanNodeID{Value: ulid.New()}

		partitionBy := []*physicalpb.ColumnExpression{
			newColumnExpr("service", physicalpb.COLUMN_TYPE_LABEL),
		}

		filterPredicates := []*physicalpb.Expression{
			BinaryExpressionToExpression(
				newBinaryExpr(
					ColumnExpressionToExpression(newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL)),
					LiteralExpressionToExpression(NewLiteral("error")),
					physicalpb.BINARY_OP_EQ,
				)),
			BinaryExpressionToExpression(
				newBinaryExpr(
					ColumnExpressionToExpression(newColumnExpr("message", physicalpb.COLUMN_TYPE_BUILTIN)),
					LiteralExpressionToExpression(NewLiteral(".*exception.*")),
					physicalpb.BINARY_OP_MATCH_RE,
				)),
		}

		plan := &Plan{}
		{
			scan1 := plan.graph.Add(&physicalpb.DataObjScan{Id: scan1Id})
			scan2 := plan.graph.Add(&physicalpb.DataObjScan{Id: scan2Id})
			filter := plan.graph.Add(&physicalpb.Filter{
				Id:         filter1Id,
				Predicates: filterPredicates,
			})
			rangeAgg := plan.graph.Add(&physicalpb.AggregateRange{
				Id:          range1Id,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: partitionBy,
			})

			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg, Child: filter})
			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter, Child: scan1})
			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter, Child: scan2})
		}

		// apply optimisations
		optimizations := []*optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			expectedProjections := []*physicalpb.ColumnExpression{
				newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL),
				newColumnExpr("message", physicalpb.COLUMN_TYPE_BUILTIN),
				newColumnExpr("service", physicalpb.COLUMN_TYPE_LABEL),
				newColumnExpr(types.ColumnNameBuiltinTimestamp, physicalpb.COLUMN_TYPE_BUILTIN),
			}

			scan1 := expectedPlan.graph.Add(&physicalpb.DataObjScan{
				Id:          scan1Id,
				Projections: expectedProjections,
			})
			scan2 := expectedPlan.graph.Add(&physicalpb.DataObjScan{
				Id:          scan2Id,
				Projections: expectedProjections,
			})
			filter := expectedPlan.graph.Add(&physicalpb.Filter{
				Id:         filter1Id,
				Predicates: filterPredicates,
			})
			rangeAgg := expectedPlan.graph.Add(&physicalpb.AggregateRange{
				Id:          range1Id,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: partitionBy,
			})

			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg, Child: filter})
			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter, Child: scan1})
			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter, Child: scan2})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("predicate column projection pushdown without existing projections", func(t *testing.T) {
		limit1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		// Predicate columns should NOT be projected when there are no existing projections (log query)
		filterPredicates := []*physicalpb.Expression{
			BinaryExpressionToExpression(
				newBinaryExpr(
					ColumnExpressionToExpression(newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL)),
					LiteralExpressionToExpression(NewLiteral("error")),
					physicalpb.BINARY_OP_EQ,
				)),
			BinaryExpressionToExpression(
				newBinaryExpr(
					ColumnExpressionToExpression(newColumnExpr("message", physicalpb.COLUMN_TYPE_BUILTIN)),
					LiteralExpressionToExpression(NewLiteral(".*exception.*")),
					physicalpb.BINARY_OP_MATCH_RE,
				)),
		}

		plan := &Plan{}
		{
			scan1 := plan.graph.Add(&physicalpb.DataObjScan{Id: scan1Id})
			scan2 := plan.graph.Add(&physicalpb.DataObjScan{Id: scan2Id})
			filter := plan.graph.Add(&physicalpb.Filter{
				Id:         filter1Id,
				Predicates: filterPredicates,
			})
			limit := plan.graph.Add(&physicalpb.Limit{Id: limit1Id, Fetch: 100})

			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit, Child: filter})
			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter, Child: scan1})
			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter, Child: scan2})
		}

		// apply optimisations
		optimizations := []*optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			scan1 := expectedPlan.graph.Add(&physicalpb.DataObjScan{Id: scan1Id})
			scan2 := expectedPlan.graph.Add(&physicalpb.DataObjScan{Id: scan2Id})
			filter := expectedPlan.graph.Add(&physicalpb.Filter{
				Id:         filter1Id,
				Predicates: filterPredicates,
			})
			limit := expectedPlan.graph.Add(&physicalpb.Limit{Id: limit1Id, Fetch: 100})

			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit, Child: filter})
			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter, Child: scan1})
			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter, Child: scan2})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("limit pushdown with filter should not propagate limit to child nodes", func(t *testing.T) {
		limit1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		// Limit should not be propagated to child nodes when there are filters
		filterPredicates := []*physicalpb.Expression{
			BinaryExpressionToExpression(
				newBinaryExpr(
					ColumnExpressionToExpression(newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL)),
					LiteralExpressionToExpression(NewLiteral("error")),
					physicalpb.BINARY_OP_EQ,
				)),
		}

		plan := &Plan{}
		{
			scan1 := plan.graph.Add(&physicalpb.DataObjScan{Id: scan1Id})
			scan2 := plan.graph.Add(&physicalpb.DataObjScan{Id: scan2Id})
			filter := plan.graph.Add(&physicalpb.Filter{
				Id:         filter1Id,
				Predicates: filterPredicates,
			})
			limit := plan.graph.Add(&physicalpb.Limit{Id: limit1Id, Fetch: 100})

			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit, Child: filter})
			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter, Child: scan1})
			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter, Child: scan2})
		}

		// apply optimisations
		optimizations := []*optimization{
			newOptimization("limit pushdown", plan).withRules(
				&limitPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			scan1 := expectedPlan.graph.Add(&physicalpb.DataObjScan{Id: scan1Id})
			scan2 := expectedPlan.graph.Add(&physicalpb.DataObjScan{Id: scan2Id})
			filter := expectedPlan.graph.Add(&physicalpb.Filter{
				Id:         filter1Id,
				Predicates: filterPredicates,
			})
			limit := expectedPlan.graph.Add(&physicalpb.Limit{Id: limit1Id, Fetch: 100})

			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit, Child: filter})
			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter, Child: scan1})
			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter, Child: scan2})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("limit pushdown without filter should propagate limit to child nodes", func(t *testing.T) {
		limit1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		plan := &Plan{}
		{
			scan1 := plan.graph.Add(&physicalpb.DataObjScan{Id: scan1Id})
			scan2 := plan.graph.Add(&physicalpb.DataObjScan{Id: scan2Id})
			limit := plan.graph.Add(&physicalpb.Limit{Id: limit1Id, Fetch: 100})

			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit, Child: scan1})
			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit, Child: scan2})
		}

		// apply optimisations
		optimizations := []*optimization{
			newOptimization("limit pushdown", plan).withRules(
				&limitPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			scan1 := expectedPlan.graph.Add(&physicalpb.DataObjScan{Id: scan1Id, Limit: 100})
			scan2 := expectedPlan.graph.Add(&physicalpb.DataObjScan{Id: scan2Id, Limit: 100})
			limit := expectedPlan.graph.Add(&physicalpb.Limit{Id: limit1Id, Fetch: 100})

			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit, Child: scan1})
			_ = expectedPlan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit, Child: scan2})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("cleanup no-op merge nodes", func(t *testing.T) {
		limitId := physicalpb.PlanNodeID{Value: ulid.New()}
		sortMergeId := physicalpb.PlanNodeID{Value: ulid.New()}
		scanId := physicalpb.PlanNodeID{Value: ulid.New()}

		plan := func() *Plan {
			plan := &Plan{}
			limit := plan.graph.Add(&physicalpb.Limit{Id: limitId})
			merge := plan.graph.Add(&physicalpb.Merge{Id: mergeId})
			sortmerge := plan.graph.Add(&physicalpb.Merge{Id: sortMergeId})
			scan := plan.graph.Add(&physicalpb.DataObjScan{Id: scanId})

			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit, Child: merge})
			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: merge, Child: sortmerge})
			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: sortmerge, Child: scan})
			return plan
		}()

		optimizations := []*optimization{
			newOptimization("cleanup", plan).withRules(
				&removeNoopMerge{plan},
			),
		}

		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])
		actual := PrintAsTree(plan)

		optimized := func() *Plan {
			plan := &Plan{}
			limit := plan.graph.Add(&physicalpb.Limit{Id: limitId})
			scan := plan.graph.Add(&physicalpb.DataObjScan{Id: scanId})

			_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit, Child: scan})
			return plan
		}()

		expected := PrintAsTree(optimized)
		require.Equal(t, expected, actual, fmt.Sprintf("Expected:\n%s\nActual:\n%s\n", expected, actual))
	})

	// both predicate pushdown and limits pushdown should work together
	t.Run("predicate and limits pushdown", func(t *testing.T) {
		limitId := physicalpb.PlanNodeID{Value: ulid.New()}
		limit1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		sortMergeId := physicalpb.PlanNodeID{Value: ulid.New()}
		filterId := physicalpb.PlanNodeID{Value: ulid.New()}

		plan := &Plan{}
		scan1 := plan.graph.Add(&physicalpb.DataObjScan{Id: scan1Id})
		scan2 := plan.graph.Add(&physicalpb.DataObjScan{Id: scan2Id})
		sortMerge := plan.graph.Add(&physicalpb.SortMerge{Id: sortMergeId})
		filter := plan.graph.Add(&physicalpb.Filter{Id: filterId, Predicates: []*physicalpb.Expression{
			BinaryExpressionToExpression(
				newBinaryExpr(
					ColumnExpressionToExpression(newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)),
					LiteralExpressionToExpression(NewLiteral(time1000)),
					physicalpb.BINARY_OP_GT,
				)),
		}})
		limit := plan.graph.Add(&physicalpb.Limit{Id: limitId, Fetch: 100})

		_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit, Child: filter})
		_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter, Child: sortMerge})
		_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: sortMerge, Child: scan1})
		_ = plan.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: sortMerge, Child: scan2})

		planner := NewPlanner(NewContext(time.Unix(0, 0), time.Unix(3600, 0)), &catalog{})
		actual, err := planner.Optimize(plan)
		require.NoError(t, err)

		optimized := &Plan{}
		{
			scan1 := optimized.graph.Add(&physicalpb.DataObjScan{Id: scan1Id,
				Limit: 100,
				Predicates: []*physicalpb.Expression{
					BinaryExpressionToExpression(
						newBinaryExpr(
							ColumnExpressionToExpression(newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)),
							LiteralExpressionToExpression(NewLiteral(time1000)),
							physicalpb.BINARY_OP_GT,
						)),
				}})
			scan2 := optimized.graph.Add(&physicalpb.DataObjScan{Id: scan2Id,
				Limit: 100,
				Predicates: []*physicalpb.Expression{
					BinaryExpressionToExpression(
						newBinaryExpr(
							ColumnExpressionToExpression(newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)),
							LiteralExpressionToExpression(NewLiteral(time1000)),
							physicalpb.BINARY_OP_GT,
						)),
				}})
			merge := optimized.graph.Add(&physicalpb.SortMerge{Id: mergeId})
			limit := optimized.graph.Add(&physicalpb.Limit{Id: limit1Id, Fetch: 100})

			_ = optimized.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit, Child: merge})
			_ = optimized.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: merge, Child: scan1})
			_ = optimized.graph.AddEdge(dag.Edge[physicalpb.Node]{Parent: merge, Child: scan2})
		}

		expected := PrintAsTree(optimized)
		require.Equal(t, expected, PrintAsTree(actual))
	})
}

func TestProjectionPushdown_PushesRequestedKeysToParseNodes(t *testing.T) {
	tests := []struct {
		name                           string
		buildLogical                   func() logical.Value
		expectedParseKeysRequested     []string
		expectedDataObjScanProjections []string
	}{
		{
			name: "ParseNode remains empty when no operations need parsed fields",
			buildLogical: func() logical.Value {
				// Create a simple log query with no filters that need parsed fields
				// {app="test"} | logfmt
				selectorPredicate := &logical.BinOp{
					Left:  logical.NewColumnRef("app", physicalpb.COLUMN_TYPE_LABEL),
					Right: logical.NewLiteral("test"),
					Op:    physicalpb.BINARY_OP_EQ,
				}
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector:   selectorPredicate,
					Predicates: []logical.Value{selectorPredicate},
					Shard:      logical.NewShard(0, 1),
				})

				// Add parse but no filters requiring parsed fields
				builder = builder.Parse(logical.ParserLogfmt)
				return builder.Value()
			},
		},
		{
			name: "ParseNode skips label and builtin columns, only collects ambiguous",
			buildLogical: func() logical.Value {
				// {app="test"} | logfmt | app="frontend" | level="error"
				// This is a log query (no RangeAggregation) so should parse all keys
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", physicalpb.COLUMN_TYPE_LABEL),
						Right: logical.NewLiteral("test"),
						Op:    physicalpb.BINARY_OP_EQ,
					},
					Shard: logical.NewShard(0, 1),
				})

				builder = builder.Parse(logical.ParserLogfmt)

				// Add filter on label column (should be skipped)
				labelFilter := &logical.BinOp{
					Left:  logical.NewColumnRef("app", physicalpb.COLUMN_TYPE_LABEL),
					Right: logical.NewLiteral("frontend"),
					Op:    physicalpb.BINARY_OP_EQ,
				}
				builder = builder.Select(labelFilter)

				// Add filter on ambiguous column (should be collected)
				ambiguousFilter := &logical.BinOp{
					Left:  logical.NewColumnRef("level", physicalpb.COLUMN_TYPE_AMBIGUOUS),
					Right: logical.NewLiteral("error"),
					Op:    physicalpb.BINARY_OP_EQ,
				}
				builder = builder.Select(ambiguousFilter)

				return builder.Value()
			},
		},
		{
			name: "RangeAggregation with PartitionBy on ambiguous columns",
			buildLogical: func() logical.Value {
				// count_over_time({app="test"} | logfmt [5m]) by (duration, service)
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", physicalpb.COLUMN_TYPE_LABEL),
						Right: logical.NewLiteral("test"),
						Op:    physicalpb.BINARY_OP_EQ,
					},
					Shard: logical.NewShard(0, 1),
				})

				builder = builder.Parse(logical.ParserLogfmt)

				// Range aggregation with PartitionBy
				builder = builder.RangeAggregation(
					[]logical.ColumnRef{
						{Ref: types.ColumnRef{Column: "duration", Type: physicalpb.COLUMN_TYPE_AMBIGUOUS}},
						{Ref: types.ColumnRef{Column: "service", Type: physicalpb.COLUMN_TYPE_LABEL}}, // Label should be skipped
					},
					physicalpb.AGGREGATE_RANGE_OP_COUNT,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute,
					5*time.Minute,
				)

				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"duration"}, // Only ambiguous column from PartitionBy
			expectedDataObjScanProjections: []string{"message", "service", "timestamp"},
		},
		{
			name: "log query with logfmt and filter on ambiguous column",
			buildLogical: func() logical.Value {
				// Create a logical plan that represents:
				// {app="test"} | logfmt | level="error"
				// This is a log query (no RangeAggregation) so should parse all keys
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", physicalpb.COLUMN_TYPE_LABEL),
						Right: logical.NewLiteral("test"),
						Op:    physicalpb.BINARY_OP_EQ,
					},
					Shard: logical.NewShard(0, 1), // noShard
				})

				// Don't set RequestedKeys here - optimization should determine them
				builder = builder.Parse(logical.ParserLogfmt)

				// Add filter with ambiguous column
				filterExpr := &logical.BinOp{
					Left:  logical.NewColumnRef("level", physicalpb.COLUMN_TYPE_AMBIGUOUS),
					Right: logical.NewLiteral("error"),
					Op:    physicalpb.BINARY_OP_EQ,
				}
				builder = builder.Select(filterExpr)
				return builder.Value()
			},
			expectedParseKeysRequested: nil, // Log queries should parse all keys
		},
		{
			name: "metric query with logfmt and groupby on ambiguous column",
			buildLogical: func() logical.Value {
				// Create a logical plan that represents:
				// sum by(status) (count_over_time({app="test"} | logfmt [5m]))
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", physicalpb.COLUMN_TYPE_LABEL),
						Right: logical.NewLiteral("test"),
						Op:    physicalpb.BINARY_OP_EQ,
					},
					Shard: logical.NewShard(0, 1), // noShard
				})

				// Don't set RequestedKeys here - optimization should determine them
				builder = builder.Parse(logical.ParserLogfmt)

				// Range aggregation
				builder = builder.RangeAggregation(
					[]logical.ColumnRef{}, // no partition by
					physicalpb.AGGREGATE_RANGE_OP_COUNT,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute, // step
					5*time.Minute, // range interval
				)

				// Vector aggregation with groupby on ambiguous column
				builder = builder.VectorAggregation(
					[]logical.ColumnRef{
						{Ref: types.ColumnRef{Column: "status", Type: physicalpb.COLUMN_TYPE_AMBIGUOUS}},
					},
					physicalpb.AGGREGATE_VECTOR_OP_SUM,
				)
				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"status"},
			expectedDataObjScanProjections: []string{"message", "timestamp"},
		},
		{
			name: "metric query with multiple ambiguous columns",
			buildLogical: func() logical.Value {
				// Create a logical plan that represents:
				// sum by(status,code) (count_over_time({app="test"} | logfmt | duration > 100 [5m]))
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", physicalpb.COLUMN_TYPE_LABEL),
						Right: logical.NewLiteral("test"),
						Op:    physicalpb.BINARY_OP_EQ,
					},
					Shard: logical.NewShard(0, 1), // noShard
				})

				// Don't set RequestedKeys here - optimization should determine them
				builder = builder.Parse(logical.ParserLogfmt)

				// Add filter with ambiguous column
				filterExpr := &logical.BinOp{
					Left:  logical.NewColumnRef("duration", physicalpb.COLUMN_TYPE_AMBIGUOUS),
					Right: logical.NewLiteral(int64(100)),
					Op:    physicalpb.BINARY_OP_GT,
				}
				builder = builder.Select(filterExpr)

				// Range aggregation
				builder = builder.RangeAggregation(
					[]logical.ColumnRef{}, // no partition by
					physicalpb.AGGREGATE_RANGE_OP_COUNT,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute, // step
					5*time.Minute, // range interval
				)

				// Vector aggregation with groupby on ambiguous columns
				builder = builder.VectorAggregation(
					[]logical.ColumnRef{
						{Ref: types.ColumnRef{Column: "status", Type: physicalpb.COLUMN_TYPE_AMBIGUOUS}},
						{Ref: types.ColumnRef{Column: "code", Type: physicalpb.COLUMN_TYPE_AMBIGUOUS}},
					},
					physicalpb.AGGREGATE_VECTOR_OP_SUM,
				)
				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"code", "duration", "status"}, // sorted alphabetically
			expectedDataObjScanProjections: []string{"message", "timestamp"},
		},
		{
			name: "log query should request all keys even with filters",
			buildLogical: func() logical.Value {
				// Create a logical plan that represents a log query:
				// {app="test"} | logfmt | level="error" | limit 100
				// This is a log query (no range aggregation) so should parse all keys
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", physicalpb.COLUMN_TYPE_LABEL),
						Right: logical.NewLiteral("test"),
						Op:    physicalpb.BINARY_OP_EQ,
					},
					Shard: logical.NewShard(0, 1),
				})

				// Add parse without specifying RequestedKeys
				builder = builder.Parse(logical.ParserLogfmt)

				// Add filter on ambiguous column
				filterExpr := &logical.BinOp{
					Left:  logical.NewColumnRef("level", physicalpb.COLUMN_TYPE_AMBIGUOUS),
					Right: logical.NewLiteral("error"),
					Op:    physicalpb.BINARY_OP_EQ,
				}
				builder = builder.Select(filterExpr)

				// Add a limit (typical for log queries)
				builder = builder.Limit(0, 100)

				return builder.Value()
			},
		},
		{
			name: "ParseNodes consume ambiguous projections, they are not pushed down to DataObjScans",
			buildLogical: func() logical.Value {
				// Create a logical plan that represents:
				// sum by(app) (count_over_time({app="test"} | logfmt | level="error" [5m]) by (status, app))
				selectorPredicate := &logical.BinOp{
					Left:  logical.NewColumnRef("app", physicalpb.COLUMN_TYPE_LABEL),
					Right: logical.NewLiteral("test"),
					Op:    physicalpb.BINARY_OP_EQ,
				}
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector:   selectorPredicate,
					Predicates: []logical.Value{selectorPredicate},
					Shard:      logical.NewShard(0, 1), // noShard
				})

				// Don't set RequestedKeys here - optimization should determine them
				builder = builder.Parse(logical.ParserLogfmt)

				// Add filter with ambiguous column (different from grouping field)
				filterExpr := &logical.BinOp{
					Left:  logical.NewColumnRef("level", physicalpb.COLUMN_TYPE_AMBIGUOUS),
					Right: logical.NewLiteral("error"),
					Op:    physicalpb.BINARY_OP_EQ,
				}
				builder = builder.Select(filterExpr)

				// Range aggregation
				builder = builder.RangeAggregation(
					[]logical.ColumnRef{
						{Ref: types.ColumnRef{Column: "status", Type: physicalpb.COLUMN_TYPE_AMBIGUOUS}},
						{Ref: types.ColumnRef{Column: "app", Type: physicalpb.COLUMN_TYPE_LABEL}},
					}, // no partition by
					physicalpb.AGGREGATE_RANGE_OP_COUNT,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute, // step
					5*time.Minute, // range interval
				)

				// Vector aggregation with single groupby on parsed field (different from filter field)
				builder = builder.VectorAggregation(
					[]logical.ColumnRef{
						{Ref: types.ColumnRef{Column: "app", Type: physicalpb.COLUMN_TYPE_LABEL}},
					},
					physicalpb.AGGREGATE_VECTOR_OP_SUM,
				)
				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"level", "status"},
			expectedDataObjScanProjections: []string{"app", "message", "timestamp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build logical plan
			logicalValue := tt.buildLogical()
			builder := logical.NewBuilder(logicalValue)
			logicalPlan, err := builder.ToPlan()
			require.NoError(t, err)

			// Create physical planner with test catalog
			catalog := &catalog{}
			for i := 0; i < 10; i++ {
				catalog.sectionDescriptors = append(catalog.sectionDescriptors, &metastore.DataobjSectionDescriptor{
					SectionKey: metastore.SectionKey{ObjectPath: "/test/object", SectionIdx: int64(i)},
					StreamIDs:  []int64{1, 2},
					Start:      time.Unix(0, 0),
					End:        time.Unix(3600, 0),
				})
			}
			ctx := NewContext(time.Unix(0, 0), time.Unix(3600, 0))
			planner := NewPlanner(ctx, catalog)

			// Build physical plan
			physicalPlan, err := planner.Build(logicalPlan)
			require.NoError(t, err)

			// Optimize the plan - this should apply parseKeysPushdown
			optimizedPlan, err := planner.Optimize(physicalPlan)
			require.NoError(t, err)

			// Check that ParseNode and DataObjScan get the correct projections
			var parseNode *physicalpb.Parse
			projections := map[string]struct{}{}
			for node := range optimizedPlan.graph.Nodes() {
				if pn, ok := node.(*physicalpb.Parse); ok {
					parseNode = pn
					continue
				}
				if pn, ok := node.(*physicalpb.DataObjScan); ok {
					for _, colExpr := range pn.Projections {
						projections[colExpr.Name] = struct{}{}
					}
				}
			}

			var projectionArr []string
			for column := range projections {
				projectionArr = append(projectionArr, column)
			}
			sort.Strings(projectionArr)

			require.NotNil(t, parseNode, "ParseNode not found in plan")
			require.Equal(t, tt.expectedParseKeysRequested, parseNode.RequestedKeys)
			require.Equal(t, tt.expectedDataObjScanProjections, projectionArr)
		})
	}
}
