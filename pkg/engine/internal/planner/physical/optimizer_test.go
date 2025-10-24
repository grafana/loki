package physical

import (
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
			predicate: *NewLiteral(int64(123)).ToExpression(),
			want:      true,
		},
		{
			predicate: *newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN).ToExpression(),
			want:      true,
		},
		{
			predicate: *newColumnExpr("foo", physicalpb.COLUMN_TYPE_LABEL).ToExpression(),
			want:      false,
		},
		{
			predicate: *newBinaryExpr(newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN).ToExpression(),
				NewLiteral(types.Timestamp(3600000)).ToExpression(),
				physicalpb.BINARY_OP_GT,
			).ToExpression(),
			want: true,
		},
		{
			predicate: *(newBinaryExpr(newColumnExpr("level", physicalpb.COLUMN_TYPE_AMBIGUOUS).ToExpression(),
				NewLiteral("debug|info").ToExpression(),
				physicalpb.BINARY_OP_MATCH_RE,
			)).ToExpression(),
			want: false,
		},
		{
			predicate: *newBinaryExpr(newColumnExpr("level", physicalpb.COLUMN_TYPE_METADATA).ToExpression(),
				NewLiteral("debug|info").ToExpression(),
				physicalpb.BINARY_OP_MATCH_RE,
			).ToExpression(),
			want: true,
		},
		{
			predicate: *newBinaryExpr(newColumnExpr("foo", physicalpb.COLUMN_TYPE_LABEL).ToExpression(),
				NewLiteral("bar").ToExpression(),
				physicalpb.BINARY_OP_EQ,
			).ToExpression(),
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

func dummyPlan() *physicalpb.Plan {
	setID := physicalpb.PlanNodeID{Value: ulid.New()}
	filter1ID := physicalpb.PlanNodeID{Value: ulid.New()}
	filter2ID := physicalpb.PlanNodeID{Value: ulid.New()}
	filter3ID := physicalpb.PlanNodeID{Value: ulid.New()}

	plan := &physicalpb.Plan{}

	scanSet := plan.Add(&physicalpb.ScanSet{
		Id: setID,
		Targets: []*physicalpb.ScanTarget{
			{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
			{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
		},
	})
	filter1 := plan.Add(&physicalpb.Filter{Id: filter1ID, Predicates: []*physicalpb.Expression{
		(&physicalpb.BinaryExpression{
			Left:  newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN).ToExpression(),
			Right: NewLiteral(time1000).ToExpression(),
			Op:    physicalpb.BINARY_OP_GT,
		}).ToExpression(),
	}})
	filter2 := plan.Add(&physicalpb.Filter{Id: filter2ID, Predicates: []*physicalpb.Expression{
		(&physicalpb.BinaryExpression{
			Left:  newColumnExpr("level", physicalpb.COLUMN_TYPE_AMBIGUOUS).ToExpression(),
			Right: NewLiteral("debug|info").ToExpression(),
			Op:    physicalpb.BINARY_OP_MATCH_RE,
		}).ToExpression(),
	}})
	filter3 := plan.Add(&physicalpb.Filter{Id: filter3ID, Predicates: []*physicalpb.Expression{}})

	_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter3.GetFilter(), Child: filter2.GetFilter()})
	_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter2.GetFilter(), Child: filter1.GetFilter()})
	_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter1.GetFilter(), Child: scanSet.GetFilter()})

	return plan
}

func TestOptimizer(t *testing.T) {
	setID := physicalpb.PlanNodeID{Value: ulid.New()}
	filter1ID := physicalpb.PlanNodeID{Value: ulid.New()}
	filter2ID := physicalpb.PlanNodeID{Value: ulid.New()}
	filter3ID := physicalpb.PlanNodeID{Value: ulid.New()}

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

		optimized := &physicalpb.Plan{}
		scanSet := optimized.Add(&physicalpb.ScanSet{
			Id: setID,
			Targets: []*physicalpb.ScanTarget{
				{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
			},

			Predicates: []*physicalpb.Expression{
				(&physicalpb.BinaryExpression{
					Left:  newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN).ToExpression(),
					Right: NewLiteral(time1000).ToExpression(),
					Op:    physicalpb.BINARY_OP_GT,
				}).ToExpression(),
			},
		})
		filter1 := optimized.Add(&physicalpb.Filter{Id: filter1ID, Predicates: []*physicalpb.Expression{}})
		filter2 := optimized.Add(&physicalpb.Filter{Id: filter2ID, Predicates: []*physicalpb.Expression{
			(&physicalpb.BinaryExpression{
				Left:  newColumnExpr("level", physicalpb.COLUMN_TYPE_AMBIGUOUS).ToExpression(),
				Right: NewLiteral("debug|info").ToExpression(),
				Op:    physicalpb.BINARY_OP_MATCH_RE,
			}).ToExpression(),
		}})
		filter3 := optimized.Add(&physicalpb.Filter{Id: filter3ID, Predicates: []*physicalpb.Expression{}})

		_ = optimized.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(filter3), Child: physicalpb.GetNode(filter2)})
		_ = optimized.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(filter2), Child: physicalpb.GetNode(filter1)})
		_ = optimized.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(filter1), Child: physicalpb.GetNode(scanSet)})

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

		optimized := &physicalpb.Plan{}
		scanSet := optimized.Add(&physicalpb.ScanSet{
			Id: setID,

			Targets: []*physicalpb.ScanTarget{
				{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
			},

			Predicates: []*physicalpb.Expression{},
		})
		filter1 := optimized.Add(&physicalpb.Filter{Id: filter1ID, Predicates: []*physicalpb.Expression{
			(&physicalpb.BinaryExpression{
				Left:  newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN).ToExpression(),
				Right: NewLiteral(time1000).ToExpression(),
				Op:    physicalpb.BINARY_OP_GT,
			}).ToExpression(),
		}})
		filter2 := optimized.Add(&physicalpb.Filter{Id: filter2ID, Predicates: []*physicalpb.Expression{
			(&physicalpb.BinaryExpression{
				Left:  newColumnExpr("level", physicalpb.COLUMN_TYPE_AMBIGUOUS).ToExpression(),
				Right: NewLiteral("debug|info").ToExpression(),
				Op:    physicalpb.BINARY_OP_MATCH_RE,
			}).ToExpression(),
		}})

		_ = optimized.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(filter2), Child: physicalpb.GetNode(filter1)})
		_ = optimized.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(filter1), Child: physicalpb.GetNode(scanSet)})

		expected := PrintAsTree(optimized)
		require.Equal(t, expected, actual)
	})

	t.Run("projection pushdown handles groupby for SUM->COUNT", func(t *testing.T) {
		scan1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		countOverTimeID := physicalpb.PlanNodeID{Value: ulid.New()}
		sumOfID := physicalpb.PlanNodeID{Value: ulid.New()}

		groupBy := []*physicalpb.ColumnExpression{
			newColumnExpr("service", physicalpb.COLUMN_TYPE_LABEL),
			newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL),
		}

		// generate plan for sum by(service, instance) (count_over_time{...}[])
		plan := &physicalpb.Plan{}
		{
			scan1 := plan.Add(&physicalpb.DataObjScan{Id: scan1ID})
			rangeAgg := plan.Add(&physicalpb.AggregateRange{
				Id:        countOverTimeID,
				Operation: physicalpb.AGGREGATE_RANGE_OP_COUNT,
			})
			vectorAgg := plan.Add(&physicalpb.AggregateVector{
				Id:        sumOfID,
				Operation: physicalpb.AGGREGATE_VECTOR_OP_SUM,
				GroupBy:   groupBy,
			})

			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: vectorAgg.GetAggregateVector(), Child: rangeAgg.GetAggregateRange()})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: scan1.GetScan()})
		}

		// apply optimisation
		optimizations := []*optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &physicalpb.Plan{}
		{
			// pushed down from group and partition by, with range aggregations adding timestamp
			expectedProjections := []*physicalpb.ColumnExpression{
				newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL),
				newColumnExpr("service", physicalpb.COLUMN_TYPE_LABEL),
				newColumnExpr(types.ColumnNameBuiltinTimestamp, physicalpb.COLUMN_TYPE_BUILTIN),
			}

			scan1 := expectedPlan.Add(&physicalpb.DataObjScan{Id: scan1ID, Projections: expectedProjections})
			rangeAgg := expectedPlan.Add(&physicalpb.AggregateRange{
				Id:          countOverTimeID,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: groupBy,
			})
			vectorAgg := expectedPlan.Add(&physicalpb.AggregateVector{
				Id:        sumOfID,
				Operation: physicalpb.AGGREGATE_VECTOR_OP_SUM,
				GroupBy:   groupBy,
			})

			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: vectorAgg.GetAggregateVector(), Child: rangeAgg.GetAggregateRange()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: scan1.GetScan()})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("projection pushdown does not handle groupby for MAX->SUM", func(t *testing.T) {
		scan1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		sumOverTimeID := physicalpb.PlanNodeID{Value: ulid.New()}
		maxOfID := physicalpb.PlanNodeID{Value: ulid.New()}
		groupBy := []*physicalpb.ColumnExpression{
			newColumnExpr("service", physicalpb.COLUMN_TYPE_LABEL),
		}

		partitionBy := []*physicalpb.ColumnExpression{
			newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL),
		}

		// generate plan for max by(service) (sum_over_time{...}[])
		plan := &physicalpb.Plan{}
		{
			scan1 := plan.Add(&physicalpb.DataObjScan{Id: scan1ID})
			rangeAgg := plan.Add(&physicalpb.AggregateRange{
				Id:          sumOverTimeID,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_SUM,
				PartitionBy: partitionBy,
			})
			vectorAgg := plan.Add(&physicalpb.AggregateVector{
				Id:        maxOfID,
				Operation: physicalpb.AGGREGATE_VECTOR_OP_MAX,
				GroupBy:   groupBy,
			})

			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: vectorAgg.GetAggregateVector(), Child: rangeAgg.GetAggregateRange()})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: scan1.GetScan()})
		}

		// apply optimisation
		optimizations := []*optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &physicalpb.Plan{}
		{
			// groupby was not pushed down
			expectedProjections := []*physicalpb.ColumnExpression{
				newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL),
				newColumnExpr(types.ColumnNameBuiltinTimestamp, physicalpb.COLUMN_TYPE_BUILTIN),
			}

			scan1 := expectedPlan.Add(&physicalpb.DataObjScan{Id: scan1ID, Projections: expectedProjections})
			rangeAgg := expectedPlan.Add(&physicalpb.AggregateRange{
				Id:          sumOverTimeID,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_SUM,
				PartitionBy: partitionBy,
			})
			vectorAgg := expectedPlan.Add(&physicalpb.AggregateVector{
				Id:        maxOfID,
				Operation: physicalpb.AGGREGATE_VECTOR_OP_MAX,
				GroupBy:   groupBy,
			})

			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: vectorAgg.GetAggregateVector(), Child: rangeAgg.GetAggregateRange()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: scan1.GetScan()})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("projection pushdown handles partition by", func(t *testing.T) {
		range1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		scan1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		scan2ID := physicalpb.PlanNodeID{Value: ulid.New()}
		partitionBy := []*physicalpb.ColumnExpression{
			newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL),
			newColumnExpr("service", physicalpb.COLUMN_TYPE_LABEL),
		}

		plan := &physicalpb.Plan{}
		{
			scan1 := plan.Add(&physicalpb.DataObjScan{
				Id: scan1ID,
			})
			scan2 := plan.Add(&physicalpb.DataObjScan{
				Id: scan2ID,
			})
			rangeAgg := plan.Add(&physicalpb.AggregateRange{
				Id:          range1ID,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: partitionBy,
			})

			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: scan1.GetScan()})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: scan2.GetScan()})
		}

		// apply optimisations
		optimizations := []*optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &physicalpb.Plan{}
		{
			projected := append(partitionBy, newColumnExpr(types.ColumnNameBuiltinTimestamp, physicalpb.COLUMN_TYPE_BUILTIN))
			scan1 := expectedPlan.Add(&physicalpb.DataObjScan{
				Id:          scan1ID,
				Projections: projected,
			})
			scan2 := expectedPlan.Add(&physicalpb.DataObjScan{
				Id:          scan2ID,
				Projections: projected,
			})

			rangeAgg := expectedPlan.Add(&physicalpb.AggregateRange{
				Id:          range1ID,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: partitionBy,
			})

			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: scan1.GetScan()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: scan2.GetScan()})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("predicate column projection pushdown with existing projections", func(t *testing.T) {
		// Predicate columns should be projected when there are existing projections (metric query)
		range1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		scan1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		scan2ID := physicalpb.PlanNodeID{Value: ulid.New()}
		filter1ID := physicalpb.PlanNodeID{Value: ulid.New()}

		partitionBy := []*physicalpb.ColumnExpression{
			newColumnExpr("service", physicalpb.COLUMN_TYPE_LABEL),
		}

		filterPredicates := []*physicalpb.Expression{
			newBinaryExpr(
				newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL).ToExpression(),
				NewLiteral("error").ToExpression(),
				physicalpb.BINARY_OP_EQ,
			).ToExpression(),
			newBinaryExpr(
				newColumnExpr("message", physicalpb.COLUMN_TYPE_BUILTIN).ToExpression(),
				NewLiteral(".*exception.*").ToExpression(),
				physicalpb.BINARY_OP_MATCH_RE,
			).ToExpression(),
		}

		plan := &physicalpb.Plan{}
		{
			scan1 := plan.Add(&physicalpb.DataObjScan{Id: scan1ID})
			scan2 := plan.Add(&physicalpb.DataObjScan{Id: scan2ID})
			filter := plan.Add(&physicalpb.Filter{
				Id:         filter1ID,
				Predicates: filterPredicates,
			})
			rangeAgg := plan.Add(&physicalpb.AggregateRange{
				Id:          range1ID,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: partitionBy,
			})

			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: filter.GetFilter()})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: scan1.GetScan()})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: scan2.GetScan()})
		}

		// apply optimisations
		optimizations := []*optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &physicalpb.Plan{}
		{
			expectedProjections := []*physicalpb.ColumnExpression{
				newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL),
				newColumnExpr("message", physicalpb.COLUMN_TYPE_BUILTIN),
				newColumnExpr("service", physicalpb.COLUMN_TYPE_LABEL),
				newColumnExpr(types.ColumnNameBuiltinTimestamp, physicalpb.COLUMN_TYPE_BUILTIN),
			}

			scan1 := expectedPlan.Add(&physicalpb.DataObjScan{
				Id:          scan1ID,
				Projections: expectedProjections,
			})
			scan2 := expectedPlan.Add(&physicalpb.DataObjScan{
				Id:          scan2ID,
				Projections: expectedProjections,
			})
			filter := expectedPlan.Add(&physicalpb.Filter{
				Id:         filter1ID,
				Predicates: filterPredicates,
			})
			rangeAgg := expectedPlan.Add(&physicalpb.AggregateRange{
				Id:          range1ID,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: partitionBy,
			})

			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: filter.GetFilter()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: scan1.GetScan()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: scan2.GetScan()})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("predicate column projection pushdown without existing projections", func(t *testing.T) {
		limit1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		scan1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		scan2ID := physicalpb.PlanNodeID{Value: ulid.New()}
		filter1ID := physicalpb.PlanNodeID{Value: ulid.New()}

		// Predicate columns should NOT be projected when there are no existing projections (log query)
		filterPredicates := []*physicalpb.Expression{
			newBinaryExpr(
				newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL).ToExpression(),
				NewLiteral("error").ToExpression(),
				physicalpb.BINARY_OP_EQ,
			).ToExpression(),
			newBinaryExpr(
				newColumnExpr("message", physicalpb.COLUMN_TYPE_BUILTIN).ToExpression(),
				NewLiteral(".*exception.*").ToExpression(),
				physicalpb.BINARY_OP_MATCH_RE,
			).ToExpression(),
		}

		plan := &physicalpb.Plan{}
		{
			scan1 := plan.Add(&physicalpb.DataObjScan{Id: scan1ID})
			scan2 := plan.Add(&physicalpb.DataObjScan{Id: scan2ID})
			filter := plan.Add(&physicalpb.Filter{
				Id:         filter1ID,
				Predicates: filterPredicates,
			})
			limit := plan.Add(&physicalpb.Limit{Id: limit1ID, Fetch: 100})

			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: filter.GetFilter()})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: scan1.GetScan()})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: scan2.GetScan()})
		}

		// apply optimisations
		optimizations := []*optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &physicalpb.Plan{}
		{
			scan1 := expectedPlan.Add(&physicalpb.DataObjScan{Id: scan1ID})
			scan2 := expectedPlan.Add(&physicalpb.DataObjScan{Id: scan2ID})
			filter := expectedPlan.Add(&physicalpb.Filter{
				Id:         filter1ID,
				Predicates: filterPredicates,
			})
			limit := expectedPlan.Add(&physicalpb.Limit{Id: limit1ID, Fetch: 100})

			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: filter.GetFilter()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: scan1.GetScan()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: scan2.GetScan()})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("limit pushdown with filter should not propagate limit to child nodes", func(t *testing.T) {
		limit1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		scan1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		scan2ID := physicalpb.PlanNodeID{Value: ulid.New()}
		topK1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		topK2ID := physicalpb.PlanNodeID{Value: ulid.New()}

		// Limit should not be propagated to child nodes when there are filters
		filterPredicates := []*physicalpb.Expression{
			newBinaryExpr(
				newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL).ToExpression(),
				NewLiteral("error").ToExpression(),
				physicalpb.BINARY_OP_EQ,
			).ToExpression(),
		}

		plan := &physicalpb.Plan{}
		{
			scan1 := plan.Add(&physicalpb.DataObjScan{Id: scan1ID})
			scan2 := plan.Add(&physicalpb.DataObjScan{Id: scan2ID})
			topK1 := plan.Add(&physicalpb.TopK{Id: topK1ID, SortBy: newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)})
			topK2 := plan.Add(&physicalpb.TopK{Id: topK2ID, SortBy: newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)})
			filter := plan.Add(&physicalpb.Filter{
				Id:         filter1ID,
				Predicates: filterPredicates,
			})
			limit := plan.Add(&physicalpb.Limit{Id: limit1ID, Fetch: 100})

			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(limit), Child: physicalpb.GetNode(filter)})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(filter), Child: physicalpb.GetNode(topK1)})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(filter), Child: physicalpb.GetNode(topK2)})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(topK1), Child: physicalpb.GetNode(scan1)})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(topK2), Child: physicalpb.GetNode(scan2)})
		}

		// apply optimisations
		optimizations := []*optimization{
			newOptimization("limit pushdown", plan).withRules(
				&limitPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &physicalpb.Plan{}
		{
			scan1 := expectedPlan.Add(&physicalpb.DataObjScan{Id: scan1ID})
			scan2 := expectedPlan.Add(&physicalpb.DataObjScan{Id: scan2ID})
			filter := expectedPlan.Add(&physicalpb.Filter{
				Id:         filter1ID,
				Predicates: filterPredicates,
			})
			limit := expectedPlan.Add(&physicalpb.Limit{Id: limit1ID, Fetch: 100})

			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: filter.GetFilter()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: scan1.GetScan()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: scan2.GetScan()})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("limit pushdown without filter should propagate limit to child nodes", func(t *testing.T) {
		limit1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		scan1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		scan2ID := physicalpb.PlanNodeID{Value: ulid.New()}
		topK1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		topK2ID := physicalpb.PlanNodeID{Value: ulid.New()}
		plan := &physicalpb.Plan{}
		{
			scan1 := plan.Add(&physicalpb.DataObjScan{Id: scan1ID})
			scan2 := plan.Add(&physicalpb.DataObjScan{Id: scan2ID})
			topK1 := plan.Add(&physicalpb.TopK{Id: topK1ID, SortBy: newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)})
			topK2 := plan.Add(&physicalpb.TopK{Id: topK2ID, SortBy: newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)})
			limit := plan.Add(&physicalpb.Limit{Id: limit1ID, Fetch: 100})

			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(topK1), Child: physicalpb.GetNode(scan1)})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(topK2), Child: physicalpb.GetNode(scan2)})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(limit), Child: physicalpb.GetNode(topK1)})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(limit), Child: physicalpb.GetNode(topK2)})
		}

		// apply optimisations
		optimizations := []*optimization{
			newOptimization("limit pushdown", plan).withRules(
				&limitPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &physicalpb.Plan{}
		{
			scan1 := expectedPlan.Add(&physicalpb.DataObjScan{Id: scan1ID, Limit: 100})
			scan2 := expectedPlan.Add(&physicalpb.DataObjScan{Id: scan2ID, Limit: 100})
			limit := expectedPlan.Add(&physicalpb.Limit{Id: limit1ID, Fetch: 100})
			topK1 := expectedPlan.Add(&physicalpb.TopK{Id: topK1ID, SortBy: newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN), K: 100})
			topK2 := expectedPlan.Add(&physicalpb.TopK{Id: topK2ID, SortBy: newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN), K: 100})

			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: scan1.GetScan()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: scan2.GetScan()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(limit), Child: physicalpb.GetNode(topK1)})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(limit), Child: physicalpb.GetNode(topK2)})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(topK1), Child: physicalpb.GetNode(scan1)})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(topK2), Child: physicalpb.GetNode(scan2)})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	// both predicate pushdown and limits pushdown should work together
	t.Run("predicate and limits pushdown", func(t *testing.T) {
		filterID := physicalpb.PlanNodeID{Value: ulid.New()}
		limitID := physicalpb.PlanNodeID{Value: ulid.New()}
		limit1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		plan := &physicalpb.Plan{}

		scanSet := plan.Add(&physicalpb.ScanSet{
			Id: setID,

			Targets: []*physicalpb.ScanTarget{
				{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
			},
		})
		filter := plan.Add(&physicalpb.Filter{Id: filterID, Predicates: []*physicalpb.Expression{
			(&physicalpb.BinaryExpression{
				Left:  newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN).ToExpression(),
				Right: NewLiteral(time1000).ToExpression(),
				Op:    physicalpb.BINARY_OP_GT,
			}).ToExpression(),
		}})
		limit := plan.Add(&physicalpb.Limit{Id: limitID, Fetch: 100})

		_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(limit), Child: physicalpb.GetNode(filter)})
		_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(filter), Child: physicalpb.GetNode(scanSet)})

		planner := NewPlanner(NewContext(time.Unix(0, 0), time.Unix(3600, 0)), &catalog{})
		actual, err := planner.Optimize(plan)
		require.NoError(t, err)

		optimized := &physicalpb.Plan{}
		{
			scanSet := optimized.Add(&physicalpb.ScanSet{
				Id: setID,

				Targets: []*physicalpb.ScanTarget{
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				},

				Predicates: []*physicalpb.Expression{
					(&physicalpb.BinaryExpression{
						Left:  newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN).ToExpression(),
						Right: NewLiteral(time1000).ToExpression(),
						Op:    physicalpb.BINARY_OP_GT,
					}).ToExpression(),
				},
			})
			limit := optimized.Add(&physicalpb.Limit{Id: limit1ID, Fetch: 100})

			_ = optimized.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(limit), Child: physicalpb.GetNode(scanSet)})
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
					Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
					Right: logical.NewLiteral("test"),
					Op:    types.BinaryOpEq,
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
				// This is a log query (no physicalpb.AggregateRange) so should parse all keys
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})

				builder = builder.Parse(logical.ParserLogfmt)

				// Add filter on label column (should be skipped)
				labelFilter := &logical.BinOp{
					Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
					Right: logical.NewLiteral("frontend"),
					Op:    types.BinaryOpEq,
				}
				builder = builder.Select(labelFilter)

				// Add filter on ambiguous column (should be collected)
				ambiguousFilter := &logical.BinOp{
					Left:  logical.NewColumnRef("level", types.ColumnTypeAmbiguous),
					Right: logical.NewLiteral("error"),
					Op:    types.BinaryOpEq,
				}
				builder = builder.Select(ambiguousFilter)

				return builder.Value()
			},
		},
		{
			name: "physicalpb.AggregateRange with PartitionBy on ambiguous columns",
			buildLogical: func() logical.Value {
				// count_over_time({app="test"} | logfmt [5m]) by (duration, service)
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})

				builder = builder.Parse(logical.ParserLogfmt)

				// Range aggregation with PartitionBy
				builder = builder.RangeAggregation(
					[]logical.ColumnRef{
						{Ref: types.ColumnRef{Column: "duration", Type: types.ColumnTypeAmbiguous}},
						{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}}, // Label should be skipped
					},
					types.RangeAggregationTypeCount,
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
				// This is a log query (no physicalpb.AggregateRange) so should parse all keys
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1), // noShard
				})

				// Don't set RequestedKeys here - optimization should determine them
				builder = builder.Parse(logical.ParserLogfmt)

				// Add filter with ambiguous column
				filterExpr := &logical.BinOp{
					Left:  logical.NewColumnRef("level", types.ColumnTypeAmbiguous),
					Right: logical.NewLiteral("error"),
					Op:    types.BinaryOpEq,
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
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1), // noShard
				})

				// Don't set RequestedKeys here - optimization should determine them
				builder = builder.Parse(logical.ParserLogfmt)

				// Range aggregation
				builder = builder.RangeAggregation(
					[]logical.ColumnRef{}, // no partition by
					types.RangeAggregationTypeCount,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute, // step
					5*time.Minute, // range interval
				)

				// Vector aggregation with groupby on ambiguous column
				builder = builder.VectorAggregation(
					[]logical.ColumnRef{
						{Ref: types.ColumnRef{Column: "status", Type: types.ColumnTypeAmbiguous}},
					},
					types.VectorAggregationTypeSum,
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
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1), // noShard
				})

				// Don't set RequestedKeys here - optimization should determine them
				builder = builder.Parse(logical.ParserLogfmt)

				// Add filter with ambiguous column
				filterExpr := &logical.BinOp{
					Left:  logical.NewColumnRef("duration", types.ColumnTypeAmbiguous),
					Right: logical.NewLiteral(int64(100)),
					Op:    types.BinaryOpGt,
				}
				builder = builder.Select(filterExpr)

				// Range aggregation
				builder = builder.RangeAggregation(
					[]logical.ColumnRef{}, // no partition by
					types.RangeAggregationTypeCount,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute, // step
					5*time.Minute, // range interval
				)

				// Vector aggregation with groupby on ambiguous columns
				builder = builder.VectorAggregation(
					[]logical.ColumnRef{
						{Ref: types.ColumnRef{Column: "status", Type: types.ColumnTypeAmbiguous}},
						{Ref: types.ColumnRef{Column: "code", Type: types.ColumnTypeAmbiguous}},
					},
					types.VectorAggregationTypeSum,
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
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})

				// Add parse without specifying RequestedKeys
				builder = builder.Parse(logical.ParserLogfmt)

				// Add filter on ambiguous column
				filterExpr := &logical.BinOp{
					Left:  logical.NewColumnRef("level", types.ColumnTypeAmbiguous),
					Right: logical.NewLiteral("error"),
					Op:    types.BinaryOpEq,
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
					Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
					Right: logical.NewLiteral("test"),
					Op:    types.BinaryOpEq,
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
					Left:  logical.NewColumnRef("level", types.ColumnTypeAmbiguous),
					Right: logical.NewLiteral("error"),
					Op:    types.BinaryOpEq,
				}
				builder = builder.Select(filterExpr)

				// Range aggregation
				builder = builder.RangeAggregation(
					[]logical.ColumnRef{
						{Ref: types.ColumnRef{Column: "status", Type: types.ColumnTypeAmbiguous}},
						{Ref: types.ColumnRef{Column: "app", Type: types.ColumnTypeLabel}},
					}, // no partition by
					types.RangeAggregationTypeCount,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute, // step
					5*time.Minute, // range interval
				)

				// Vector aggregation with single groupby on parsed field (different from filter field)
				builder = builder.VectorAggregation(
					[]logical.ColumnRef{
						{Ref: types.ColumnRef{Column: "app", Type: types.ColumnTypeLabel}},
					},
					types.VectorAggregationTypeSum,
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
			for _, node := range optimizedPlan.Nodes {
				switch kind := node.Kind.(type) {
				case *physicalpb.PlanNode_Parse:
					parseNode = kind.Parse
					continue
				}
				if pn, ok := node.Kind.(*physicalpb.PlanNode_ScanSet); ok {
					for _, colExpr := range pn.ScanSet.Projections {
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

func Test_parallelPushdown(t *testing.T) {
	t.Run("canPushdown", func(t *testing.T) {
		tt := []struct {
			name     string
			children []physicalpb.Node
			expected bool
		}{
			{
				name:     "no children",
				children: nil,
				expected: false,
			},
			{
				name:     "one child (not Parallelize)",
				children: []physicalpb.Node{&physicalpb.DataObjScan{}},
				expected: false,
			},
			{
				name:     "one child (Parallelize)",
				children: []physicalpb.Node{&physicalpb.Parallelize{}},
				expected: true,
			},
			{
				name:     "multiple children (all Parallelize)",
				children: []physicalpb.Node{&physicalpb.Parallelize{}, &physicalpb.Parallelize{}},
				expected: true,
			},
			{
				name:     "multiple children (not all Parallelize)",
				children: []physicalpb.Node{&physicalpb.Parallelize{}, &physicalpb.DataObjScan{}},
				expected: false,
			},
		}

		for _, tc := range tt {
			t.Run(tc.name, func(t *testing.T) {
				var plan physicalpb.Plan
				parent := plan.Add(&physicalpb.Filter{})

				for _, child := range tc.children {
					plan.Add(child)
					require.NoError(t, plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(parent), Child: child}))
				}

				pass := parallelPushdown{plan: &plan}
				require.Equal(t, tc.expected, pass.canPushdown(physicalpb.GetNode(parent)))
			})
		}
	})

	t.Run("Shifts Filter", func(t *testing.T) {
		var plan physicalpb.Plan
		{
			vectorAgg := plan.Add(&physicalpb.AggregateVector{})
			rangeAgg := plan.Add(&physicalpb.AggregateRange{})
			filter := plan.Add(&physicalpb.Filter{})
			parallelize := plan.Add(&physicalpb.Parallelize{})
			scan := plan.Add(&physicalpb.DataObjScan{})

			require.NoError(t, plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(vectorAgg), Child: physicalpb.GetNode(rangeAgg)}))
			require.NoError(t, plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(rangeAgg), Child: physicalpb.GetNode(filter)}))
			require.NoError(t, plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(filter), Child: physicalpb.GetNode(parallelize)}))
			require.NoError(t, plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(parallelize), Child: physicalpb.GetNode(scan)}))
		}

		opt := newOptimizer(&plan, []*optimization{
			newOptimization("ParallelPushdown", &plan).withRules(&parallelPushdown{plan: &plan}),
		})
		root, _ := plan.Root()
		opt.optimize(root)

		var expectedPlan physicalpb.Plan
		{
			vectorAgg := expectedPlan.Add(&physicalpb.AggregateVector{})
			rangeAgg := expectedPlan.Add(&physicalpb.AggregateRange{})
			parallelize := expectedPlan.Add(&physicalpb.Parallelize{})
			filter := expectedPlan.Add(&physicalpb.Filter{})
			scan := expectedPlan.Add(&physicalpb.DataObjScan{})

			require.NoError(t, expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(vectorAgg), Child: physicalpb.GetNode(rangeAgg)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(rangeAgg), Child: physicalpb.GetNode(parallelize)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(parallelize), Child: physicalpb.GetNode(filter)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(filter), Child: physicalpb.GetNode(scan)}))
		}

		expected := PrintAsTree(&expectedPlan)
		require.Equal(t, expected, PrintAsTree(&plan))
	})

	t.Run("Shifts Parse", func(t *testing.T) {
		var plan physicalpb.Plan
		{
			vectorAgg := plan.Add(&physicalpb.AggregateVector{})
			rangeAgg := plan.Add(&physicalpb.AggregateRange{})
			parse := plan.Add(&physicalpb.Parse{})
			parallelize := plan.Add(&physicalpb.Parallelize{})
			scan := plan.Add(&physicalpb.DataObjScan{})

			require.NoError(t, plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(vectorAgg), Child: physicalpb.GetNode(rangeAgg)}))
			require.NoError(t, plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(rangeAgg), Child: physicalpb.GetNode(parse)}))
			require.NoError(t, plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(parse), Child: physicalpb.GetNode(parallelize)}))
			require.NoError(t, plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(parallelize), Child: physicalpb.GetNode(scan)}))
		}

		opt := newOptimizer(&plan, []*optimization{
			newOptimization("ParallelPushdown", &plan).withRules(&parallelPushdown{plan: &plan}),
		})
		root, _ := plan.Root()
		opt.optimize(root)

		var expectedPlan physicalpb.Plan
		{
			vectorAgg := expectedPlan.Add(&physicalpb.AggregateVector{})
			rangeAgg := expectedPlan.Add(&physicalpb.AggregateRange{})
			parallelize := expectedPlan.Add(&physicalpb.Parallelize{})
			parse := expectedPlan.Add(&physicalpb.Parse{})
			scan := expectedPlan.Add(&physicalpb.DataObjScan{})

			require.NoError(t, expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(vectorAgg), Child: physicalpb.GetNode(rangeAgg)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(rangeAgg), Child: physicalpb.GetNode(parallelize)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(parallelize), Child: physicalpb.GetNode(parse)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(parse), Child: physicalpb.GetNode(scan)}))
		}

		expected := PrintAsTree(&expectedPlan)
		require.Equal(t, expected, PrintAsTree(&plan))
	})

	t.Run("Splits TopK", func(t *testing.T) {
		var plan physicalpb.Plan
		{
			limit := plan.Add(&physicalpb.Limit{})
			topk := plan.Add(&physicalpb.TopK{SortBy: &physicalpb.ColumnExpression{}})
			parallelize := plan.Add(&physicalpb.Parallelize{})
			scan := plan.Add(&physicalpb.DataObjScan{})

			require.NoError(t, plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(limit), Child: physicalpb.GetNode(topk)}))
			require.NoError(t, plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(topk), Child: physicalpb.GetNode(parallelize)}))
			require.NoError(t, plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(parallelize), Child: physicalpb.GetNode(scan)}))
		}

		opt := newOptimizer(&plan, []*optimization{
			newOptimization("ParallelPushdown", &plan).withRules(&parallelPushdown{plan: &plan}),
		})
		root, _ := plan.Root()

		// Since [optimization.optimize] does up to three passes,
		// parallelPushdown must ignore a node after it's already been
		// processed. Otherwise, it will cause TopK to be sharded three times,
		// ending up with this plan:
		//
		//   TopK
		//     Parallelize
		//       TopK # Shard from first iteration
		//         TopK # Shard from second iteration
		//           TopK # Shard from third iteration
		//             DataObjScan
		opt.optimize(root)

		var expectedPlan physicalpb.Plan
		{
			limit := expectedPlan.Add(&physicalpb.Limit{})
			globalTopK := expectedPlan.Add(&physicalpb.TopK{SortBy: &physicalpb.ColumnExpression{}})
			parallelize := expectedPlan.Add(&physicalpb.Parallelize{})
			localTopK := expectedPlan.Add(&physicalpb.TopK{SortBy: &physicalpb.ColumnExpression{}})
			scan := expectedPlan.Add(&physicalpb.DataObjScan{})

			require.NoError(t, expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(limit), Child: physicalpb.GetNode(globalTopK)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(globalTopK), Child: physicalpb.GetNode(parallelize)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(parallelize), Child: physicalpb.GetNode(localTopK)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(localTopK), Child: physicalpb.GetNode(scan)}))
		}

		expected := PrintAsTree(&expectedPlan)
		require.Equal(t, expected, PrintAsTree(&plan))
	})
}
