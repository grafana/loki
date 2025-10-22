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

func dummyPlan() *Plan {
	plan := &Plan{}

	scanSet := plan.graph.Add(&ScanSet{
		id: "set",

		Targets: []*ScanTarget{
			{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
			{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
		},
	})
	filter1 := plan.graph.Add(&Filter{id: "filter1", Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
			Right: NewLiteral(time1000),
			Op:    types.BinaryOpGt,
		},
	}})
	filter2 := plan.graph.Add(&Filter{id: "filter2", Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
			Right: NewLiteral("debug|info"),
			Op:    types.BinaryOpMatchRe,
		},
	}})
	filter3 := plan.graph.Add(&Filter{id: "filter3", Predicates: []Expression{}})

	_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter3, Child: filter2})
	_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter2, Child: filter1})
	_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter1, Child: scanSet})

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
		scanSet := optimized.graph.Add(&ScanSet{
			id: "set",

			Targets: []*ScanTarget{
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
			},

			Predicates: []Expression{
				&BinaryExpr{
					Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
					Right: NewLiteral(time1000),
					Op:    types.BinaryOpGt,
				},
			},
		})
		filter1 := optimized.graph.Add(&Filter{id: "filter1", Predicates: []Expression{}})
		filter2 := optimized.graph.Add(&Filter{id: "filter2", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
				Right: NewLiteral("debug|info"),
				Op:    types.BinaryOpMatchRe,
			},
		}})
		filter3 := optimized.Add(&physicalpb.Filter{Id: filter3Id, Predicates: []*physicalpb.Expression{}})

		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter3, Child: filter2})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter2, Child: filter1})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter1, Child: scanSet})

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
		scanSet := optimized.graph.Add(&ScanSet{
			id: "set",

			Targets: []*ScanTarget{
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
			},

			Predicates: []Expression{},
		})
		filter1 := optimized.graph.Add(&Filter{id: "filter1", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time1000),
				Op:    types.BinaryOpGt,
			},
		}})
		filter2 := optimized.graph.Add(&Filter{id: "filter2", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
				Right: NewLiteral("debug|info"),
				Op:    types.BinaryOpMatchRe,
			},
		}})

		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter2, Child: filter1})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter1, Child: scanSet})

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
		plan := &physicalpb.Plan{}
		{
			scan1 := plan.Add(&physicalpb.DataObjScan{Id: scan1Id})
			rangeAgg := plan.Add(&physicalpb.AggregateRange{
				Id:        countOverTimeId,
				Operation: physicalpb.AGGREGATE_RANGE_OP_COUNT,
			})
			vectorAgg := plan.Add(&physicalpb.AggregateVector{
				Id:        sumOfId,
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

			scan1 := expectedPlan.Add(&physicalpb.DataObjScan{Id: scan1Id, Projections: expectedProjections})
			rangeAgg := expectedPlan.Add(&physicalpb.AggregateRange{
				Id:          countOverTimeId,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: groupBy,
			})
			vectorAgg := expectedPlan.Add(&physicalpb.AggregateVector{
				Id:        sumOfId,
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
		sumOverTimeId := physicalpb.PlanNodeID{Value: ulid.New()}
		maxOfId := physicalpb.PlanNodeID{Value: ulid.New()}
		groupBy := []*physicalpb.ColumnExpression{
			newColumnExpr("service", physicalpb.COLUMN_TYPE_LABEL),
		}

		partitionBy := []*physicalpb.ColumnExpression{
			newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL),
		}

		// generate plan for max by(service) (sum_over_time{...}[])
		plan := &physicalpb.Plan{}
		{
			scan1 := plan.Add(&physicalpb.DataObjScan{Id: scan1Id})
			rangeAgg := plan.Add(&physicalpb.AggregateRange{
				Id:          sumOverTimeId,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_SUM,
				PartitionBy: partitionBy,
			})
			vectorAgg := plan.Add(&physicalpb.AggregateVector{
				Id:        maxOfId,
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

			scan1 := expectedPlan.Add(&physicalpb.DataObjScan{Id: scan1Id, Projections: expectedProjections})
			rangeAgg := expectedPlan.Add(&physicalpb.AggregateRange{
				Id:          sumOverTimeId,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_SUM,
				PartitionBy: partitionBy,
			})
			vectorAgg := expectedPlan.Add(&physicalpb.AggregateVector{
				Id:        maxOfId,
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
		range1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		partitionBy := []*physicalpb.ColumnExpression{
			newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL),
			newColumnExpr("service", physicalpb.COLUMN_TYPE_LABEL),
		}

		plan := &physicalpb.Plan{}
		{
			scan1 := plan.Add(&physicalpb.DataObjScan{
				Id: scan1Id,
			})
			scan2 := plan.Add(&physicalpb.DataObjScan{
				Id: scan2Id,
			})
			rangeAgg := plan.Add(&physicalpb.AggregateRange{
				Id:          range1Id,
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
				Id:          scan1Id,
				Projections: projected,
			})
			scan2 := expectedPlan.Add(&physicalpb.DataObjScan{
				Id:          scan2Id,
				Projections: projected,
			})

			rangeAgg := expectedPlan.Add(&physicalpb.AggregateRange{
				Id:          range1Id,
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
		range1Id := physicalpb.PlanNodeID{Value: ulid.New()}

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
			scan1 := plan.Add(&physicalpb.DataObjScan{Id: scan1Id})
			scan2 := plan.Add(&physicalpb.DataObjScan{Id: scan2Id})
			filter := plan.Add(&physicalpb.Filter{
				Id:         filter1Id,
				Predicates: filterPredicates,
			})
			rangeAgg := plan.Add(&physicalpb.AggregateRange{
				Id:          range1Id,
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
				Id:          scan1Id,
				Projections: expectedProjections,
			})
			scan2 := expectedPlan.Add(&physicalpb.DataObjScan{
				Id:          scan2Id,
				Projections: expectedProjections,
			})
			filter := expectedPlan.Add(&physicalpb.Filter{
				Id:         filter1Id,
				Predicates: filterPredicates,
			})
			rangeAgg := expectedPlan.Add(&physicalpb.AggregateRange{
				Id:          range1Id,
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
		limit1Id := physicalpb.PlanNodeID{Value: ulid.New()}
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
			scan1 := plan.Add(&physicalpb.DataObjScan{Id: scan1Id})
			scan2 := plan.Add(&physicalpb.DataObjScan{Id: scan2Id})
			filter := plan.Add(&physicalpb.Filter{
				Id:         filter1Id,
				Predicates: filterPredicates,
			})
			limit := plan.Add(&physicalpb.Limit{Id: limit1Id, Fetch: 100})

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
			scan1 := expectedPlan.Add(&physicalpb.DataObjScan{Id: scan1Id})
			scan2 := expectedPlan.Add(&physicalpb.DataObjScan{Id: scan2Id})
			filter := expectedPlan.Add(&physicalpb.Filter{
				Id:         filter1Id,
				Predicates: filterPredicates,
			})
			limit := expectedPlan.Add(&physicalpb.Limit{Id: limit1Id, Fetch: 100})

			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: filter.GetFilter()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: scan1.GetScan()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: scan2.GetScan()})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("limit pushdown with filter should not propagate limit to child nodes", func(t *testing.T) {
		limit1Id := physicalpb.PlanNodeID{Value: ulid.New()}
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
			scan1 := plan.graph.Add(&DataObjScan{id: "scan1"})
			scan2 := plan.graph.Add(&DataObjScan{id: "scan2"})
			topK1 := plan.graph.Add(&TopK{id: "topK1", SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin)})
			topK2 := plan.graph.Add(&TopK{id: "topK2", SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin)})
			filter := plan.graph.Add(&Filter{
				id:         "filter1",
				Predicates: filterPredicates,
			})
			limit := plan.Add(&physicalpb.Limit{Id: limit1Id, Fetch: 100})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: filter})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: topK1})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: topK2})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: topK1, Child: scan1})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: topK2, Child: scan2})
		}
		orig := PrintAsTree(plan)

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
			scan1 := expectedPlan.Add(&physicalpb.DataObjScan{Id: scan1Id})
			scan2 := expectedPlan.Add(&physicalpb.DataObjScan{Id: scan2Id})
			filter := expectedPlan.Add(&physicalpb.Filter{
				Id:         filter1Id,
				Predicates: filterPredicates,
			})
			limit := expectedPlan.Add(&physicalpb.Limit{Id: limit1Id, Fetch: 100})

			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: filter.GetFilter()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: scan1.GetScan()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: scan2.GetScan()})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("limit pushdown without filter should propagate limit to child nodes", func(t *testing.T) {
		limit1Id := physicalpb.PlanNodeID{Value: ulid.New()}
		plan := &physicalpb.Plan{}
		{
			scan1 := plan.graph.Add(&DataObjScan{id: "scan1"})
			scan2 := plan.graph.Add(&DataObjScan{id: "scan2"})
			topK1 := plan.graph.Add(&TopK{id: "topK1", SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin)})
			topK2 := plan.graph.Add(&TopK{id: "topK2", SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin)})
			limit := plan.graph.Add(&Limit{id: "limit1", Fetch: 100})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: topK1, Child: scan1})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: topK2, Child: scan2})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: topK1})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: topK2})
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
			scan1 := expectedPlan.Add(&physicalpb.DataObjScan{Id: scan1Id, Limit: 100})
			scan2 := expectedPlan.Add(&physicalpb.DataObjScan{Id: scan2Id, Limit: 100})
			limit := expectedPlan.Add(&physicalpb.Limit{Id: limit1Id, Fetch: 100})
			scan1 := expectedPlan.graph.Add(&DataObjScan{id: "scan1"})
			scan2 := expectedPlan.graph.Add(&DataObjScan{id: "scan2"})
			topK1 := expectedPlan.graph.Add(&TopK{id: "topK1", SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin), K: 100})
			topK2 := expectedPlan.graph.Add(&TopK{id: "topK2", SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin), K: 100})
			limit := expectedPlan.graph.Add(&Limit{id: "limit1", Fetch: 100})

			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: scan1.GetScan()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: scan2.GetScan()})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: topK1})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: topK2})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: topK1, Child: scan1})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: topK2, Child: scan2})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	// both predicate pushdown and limits pushdown should work together
	t.Run("predicate and limits pushdown", func(t *testing.T) {
		plan := &Plan{}

		scanSet := plan.graph.Add(&ScanSet{
			id: "set",

			Targets: []*ScanTarget{
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
			},
		})
		filter := plan.graph.Add(&Filter{id: "filter", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time1000),
				Op:    types.BinaryOpGt,
			},
		}})
		limit := plan.graph.Add(&Limit{id: "limit", Fetch: 100})

		_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: filter})
		_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scanSet})

		planner := NewPlanner(NewContext(time.Unix(0, 0), time.Unix(3600, 0)), &catalog{})
		actual, err := planner.Optimize(plan)
		require.NoError(t, err)

		optimized := &physicalpb.Plan{}
		{
			scanSet := optimized.graph.Add(&ScanSet{
				id: "set",

				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},

				Predicates: []Expression{
					&BinaryExpr{
						Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
						Right: NewLiteral(time1000),
						Op:    types.BinaryOpGt,
					},
				},
			})
			limit := optimized.graph.Add(&Limit{id: "limit1", Fetch: 100})

			_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: scanSet})
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
			for _, node := range optimizedPlan.Nodes {
				switch kind := node.Kind.(type) {
				case *physicalpb.PlanNode_Parse:
					parseNode = kind.Parse
					continue
				}
				if pn, ok := node.(*ScanSet); ok {
					for _, colExpr := range pn.Projections {
						expr := colExpr.(*ColumnExpr)
						projections[expr.Ref.Column] = struct{}{}
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
			children []Node
			expected bool
		}{
			{
				name:     "no children",
				children: nil,
				expected: false,
			},
			{
				name:     "one child (not Parallelize)",
				children: []Node{&DataObjScan{}},
				expected: false,
			},
			{
				name:     "one child (Parallelize)",
				children: []Node{&Parallelize{}},
				expected: true,
			},
			{
				name:     "multiple children (all Parallelize)",
				children: []Node{&Parallelize{}, &Parallelize{}},
				expected: true,
			},
			{
				name:     "multiple children (not all Parallelize)",
				children: []Node{&Parallelize{}, &DataObjScan{}},
				expected: false,
			},
		}

		for _, tc := range tt {
			t.Run(tc.name, func(t *testing.T) {
				var plan Plan
				parent := plan.graph.Add(&Filter{})

				for _, child := range tc.children {
					plan.graph.Add(child)
					require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: parent, Child: child}))
				}

				pass := parallelPushdown{plan: &plan}
				require.Equal(t, tc.expected, pass.canPushdown(parent))
			})
		}
	})

	t.Run("Shifts Filter", func(t *testing.T) {
		var plan Plan
		{
			vectorAgg := plan.graph.Add(&VectorAggregation{})
			rangeAgg := plan.graph.Add(&RangeAggregation{})
			filter := plan.graph.Add(&Filter{})
			parallelize := plan.graph.Add(&Parallelize{})
			scan := plan.graph.Add(&DataObjScan{})

			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: filter}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: parallelize}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: scan}))
		}

		opt := newOptimizer(&plan, []*optimization{
			newOptimization("ParallelPushdown", &plan).withRules(&parallelPushdown{plan: &plan}),
		})
		root, _ := plan.graph.Root()
		opt.optimize(root)

		var expectedPlan Plan
		{
			vectorAgg := expectedPlan.graph.Add(&VectorAggregation{})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{})
			parallelize := expectedPlan.graph.Add(&Parallelize{})
			filter := expectedPlan.graph.Add(&Filter{})
			scan := expectedPlan.graph.Add(&DataObjScan{})

			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: parallelize}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: filter}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scan}))
		}

		expected := PrintAsTree(&expectedPlan)
		require.Equal(t, expected, PrintAsTree(&plan))
	})

	t.Run("Shifts Parse", func(t *testing.T) {
		var plan Plan
		{
			vectorAgg := plan.graph.Add(&VectorAggregation{})
			rangeAgg := plan.graph.Add(&RangeAggregation{})
			parse := plan.graph.Add(&ParseNode{})
			parallelize := plan.graph.Add(&Parallelize{})
			scan := plan.graph.Add(&DataObjScan{})

			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: parse}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: parse, Child: parallelize}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: scan}))
		}

		opt := newOptimizer(&plan, []*optimization{
			newOptimization("ParallelPushdown", &plan).withRules(&parallelPushdown{plan: &plan}),
		})
		root, _ := plan.graph.Root()
		opt.optimize(root)

		var expectedPlan Plan
		{
			vectorAgg := expectedPlan.graph.Add(&VectorAggregation{})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{})
			parallelize := expectedPlan.graph.Add(&Parallelize{})
			parse := expectedPlan.graph.Add(&ParseNode{})
			scan := expectedPlan.graph.Add(&DataObjScan{})

			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: parallelize}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: parse}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parse, Child: scan}))
		}

		expected := PrintAsTree(&expectedPlan)
		require.Equal(t, expected, PrintAsTree(&plan))
	})

	t.Run("Splits TopK", func(t *testing.T) {
		var plan Plan
		{
			limit := plan.graph.Add(&Limit{})
			topk := plan.graph.Add(&TopK{SortBy: &ColumnExpr{}})
			parallelize := plan.graph.Add(&Parallelize{})
			scan := plan.graph.Add(&DataObjScan{})

			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: topk}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: topk, Child: parallelize}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: scan}))
		}

		opt := newOptimizer(&plan, []*optimization{
			newOptimization("ParallelPushdown", &plan).withRules(&parallelPushdown{plan: &plan}),
		})
		root, _ := plan.graph.Root()

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

		var expectedPlan Plan
		{
			limit := expectedPlan.graph.Add(&Limit{})
			globalTopK := expectedPlan.graph.Add(&TopK{SortBy: &ColumnExpr{}})
			parallelize := expectedPlan.graph.Add(&Parallelize{})
			localTopK := expectedPlan.graph.Add(&TopK{SortBy: &ColumnExpr{}})
			scan := expectedPlan.graph.Add(&DataObjScan{})

			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: globalTopK}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: globalTopK, Child: parallelize}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: localTopK}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: localTopK, Child: scan}))
		}

		expected := PrintAsTree(&expectedPlan)
		require.Equal(t, expected, PrintAsTree(&plan))
	})
}
