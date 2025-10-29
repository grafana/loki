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

var setID = physicalpb.PlanNodeID{Value: ulid.New()}
var filter1ID = physicalpb.PlanNodeID{Value: ulid.New()}
var filter2ID = physicalpb.PlanNodeID{Value: ulid.New()}
var filter3ID = physicalpb.PlanNodeID{Value: ulid.New()}

func dummyPlan() *physicalpb.Plan {

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
	_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter1.GetFilter(), Child: scanSet.GetScanSet()})

	return plan
}

func TestPredicatePushdown(t *testing.T) {
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
	}}) // ambiguous column predicates are not pushed down.
	filter3 := optimized.Add(&physicalpb.Filter{Id: filter3ID, Predicates: []*physicalpb.Expression{}})

	_ = optimized.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(filter3), Child: physicalpb.GetNode(filter2)})
	_ = optimized.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(filter2), Child: physicalpb.GetNode(filter1)})
	_ = optimized.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(filter1), Child: physicalpb.GetNode(scanSet)})

	expected := PrintAsTree(optimized)
	require.Equal(t, expected, actual)
}

func TestLimitPushdown(t *testing.T) {
	t.Run("pushdown limit to target nodes", func(t *testing.T) {
		setID := physicalpb.PlanNodeID{Value: ulid.New()}
		topK1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		topK2ID := physicalpb.PlanNodeID{Value: ulid.New()}
		limit1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		plan := &physicalpb.Plan{}
		{
			scanset := plan.Add(&physicalpb.ScanSet{
				Id: setID,
				Targets: []*physicalpb.ScanTarget{
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				},
			})
			topK1 := plan.Add(&physicalpb.TopK{Id: topK1ID, SortBy: newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)})
			topK2 := plan.Add(&physicalpb.TopK{Id: topK2ID, SortBy: newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)})
			limit := plan.Add(&physicalpb.Limit{Id: limit1ID, Fetch: 100})

			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: topK1.GetTopK(), Child: scanset.GetScanSet()})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: topK1.GetTopK()})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: topK2.GetTopK()})
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
			scanset := expectedPlan.Add(&physicalpb.ScanSet{
				Id: setID,
				Targets: []*physicalpb.ScanTarget{
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				},
			})
			topK1 := expectedPlan.Add(&physicalpb.TopK{Id: topK1ID, SortBy: newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN), K: 100})
			topK2 := expectedPlan.Add(&physicalpb.TopK{Id: topK2ID, SortBy: newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN), K: 100})
			limit := expectedPlan.Add(&physicalpb.Limit{Id: limit1ID, Fetch: 100})

			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: topK1.GetTopK()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: topK2.GetTopK()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: topK1.GetTopK(), Child: scanset.GetScanSet()})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("pushdown blocked by filter nodes", func(t *testing.T) {
		// Limit should not be propagated to child nodes when there are filters
		filterPredicates := []*physicalpb.Expression{
			(&physicalpb.BinaryExpression{
				Left:  (&physicalpb.ColumnExpression{Name: "level", Type: physicalpb.COLUMN_TYPE_LABEL}).ToExpression(),
				Right: NewLiteral("error").ToExpression(),
				Op:    physicalpb.BINARY_OP_EQ,
			}).ToExpression(),
		}
		setID := physicalpb.PlanNodeID{Value: ulid.New()}
		topK1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		topK2ID := physicalpb.PlanNodeID{Value: ulid.New()}
		limit1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		filter1ID := physicalpb.PlanNodeID{Value: ulid.New()}

		plan := &physicalpb.Plan{}
		{
			scanset := plan.Add(&physicalpb.ScanSet{
				Id: setID,
				Targets: []*physicalpb.ScanTarget{
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				},
			})
			topK1 := plan.Add(&physicalpb.TopK{Id: topK1ID, SortBy: newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)})
			topK2 := plan.Add(&physicalpb.TopK{Id: topK2ID, SortBy: newColumnExpr("timestamp", physicalpb.COLUMN_TYPE_BUILTIN)})
			filter := plan.Add(&physicalpb.Filter{
				Id:         filter1ID,
				Predicates: filterPredicates,
			})
			limit := plan.Add(&physicalpb.Limit{Id: limit1ID, Fetch: 100})

			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: limit.GetLimit(), Child: filter.GetFilter()})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: topK1.GetTopK()})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: topK2.GetTopK()})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: topK1.GetTopK(), Child: scanset.GetScanSet()})
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

		actual := PrintAsTree(plan)
		require.Equal(t, orig, actual)
	})
}

func TestGroupByPushdown(t *testing.T) {
	t.Run("pushdown to RangeAggregation", func(t *testing.T) {
		setID := physicalpb.PlanNodeID{Value: ulid.New()}
		rangeAggID := physicalpb.PlanNodeID{Value: ulid.New()}
		sumOfID := physicalpb.PlanNodeID{Value: ulid.New()}

		groupBy := []*physicalpb.ColumnExpression{
			newColumnExpr("service", physicalpb.COLUMN_TYPE_LABEL),
			newColumnExpr("level", physicalpb.COLUMN_TYPE_LABEL),
		}

		// generate plan for sum by(service, instance) (count_over_time{...}[])
		plan := &physicalpb.Plan{}
		{
			scanSet := plan.Add(&physicalpb.ScanSet{
				Id: setID,

				Targets: []*physicalpb.ScanTarget{
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				},

				Predicates: []*physicalpb.Expression{},
			})
			rangeAgg := plan.Add(&physicalpb.AggregateRange{
				Id:        rangeAggID,
				Operation: physicalpb.AGGREGATE_RANGE_OP_COUNT,
			})
			vectorAgg := plan.Add(&physicalpb.AggregateVector{
				Id:        sumOfID,
				Operation: physicalpb.AGGREGATE_VECTOR_OP_SUM,
				GroupBy:   groupBy,
			})

			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: vectorAgg.GetAggregateVector(), Child: rangeAgg.GetAggregateRange()})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: scanSet.GetScanSet()})
		}
		// apply optimisation
		optimizations := []*optimization{
			newOptimization("groupBy pushdown", plan).withRules(
				&groupByPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &physicalpb.Plan{}
		{
			scanSet := expectedPlan.Add(&physicalpb.ScanSet{
				Id: setID,
				Targets: []*physicalpb.ScanTarget{
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				},
				Predicates: []*physicalpb.Expression{},
			})
			rangeAgg := expectedPlan.Add(&physicalpb.AggregateRange{
				Id:          rangeAggID,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: groupBy,
			})
			vectorAgg := expectedPlan.Add(&physicalpb.AggregateVector{
				Id:        sumOfID,
				Operation: physicalpb.AGGREGATE_VECTOR_OP_SUM,
				GroupBy:   groupBy,
			})

			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: vectorAgg.GetAggregateVector(), Child: rangeAgg.GetAggregateRange()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: scanSet.GetScanSet()})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("MAX->SUM is not allowed", func(t *testing.T) {
		setID := physicalpb.PlanNodeID{Value: ulid.New()}
		rangeAggID := physicalpb.PlanNodeID{Value: ulid.New()}
		maxOfID := physicalpb.PlanNodeID{Value: ulid.New()}
		groupBy := []*physicalpb.ColumnExpression{
			newColumnExpr("service", physicalpb.COLUMN_TYPE_LABEL),
		}

		// generate plan for max by(service) (sum_over_time{...}[])
		plan := &physicalpb.Plan{}
		{
			scanSet := plan.Add(&physicalpb.ScanSet{
				Id: setID,
				Targets: []*physicalpb.ScanTarget{
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				},
				Predicates: []*physicalpb.Expression{},
			})
			rangeAgg := plan.Add(&physicalpb.AggregateRange{
				Id:        rangeAggID,
				Operation: physicalpb.AGGREGATE_RANGE_OP_SUM,
			})
			vectorAgg := plan.Add(&physicalpb.AggregateVector{
				Id:        maxOfID,
				Operation: physicalpb.AGGREGATE_VECTOR_OP_MAX,
				GroupBy:   groupBy,
			})

			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: vectorAgg.GetAggregateVector(), Child: rangeAgg.GetAggregateRange()})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: scanSet.GetScanSet()})
		}

		orig := PrintAsTree(plan)

		// apply optimisation
		optimizations := []*optimization{
			newOptimization("projection pushdown", plan).withRules(
				&groupByPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		actual := PrintAsTree(plan)
		require.Equal(t, orig, actual)
	})
}

func TestProjectionPushdown(t *testing.T) {
	t.Run("range aggreagation groupBy -> scanset", func(t *testing.T) {
		partitionBy := []*physicalpb.ColumnExpression{
			{Name: "level", Type: physicalpb.COLUMN_TYPE_LABEL},
			{Name: "service", Type: physicalpb.COLUMN_TYPE_LABEL},
		}
		setID := physicalpb.PlanNodeID{Value: ulid.New()}
		range1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		plan := &physicalpb.Plan{}
		{
			scanset := plan.Add(&physicalpb.ScanSet{
				Id: setID,
				Targets: []*physicalpb.ScanTarget{
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				},
			})
			rangeAgg := plan.Add(&physicalpb.AggregateRange{
				Id:          range1ID,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: partitionBy,
			})

			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: scanset.GetScanSet()})
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
			projected := append(partitionBy, &physicalpb.ColumnExpression{Name: types.ColumnNameBuiltinTimestamp, Type: physicalpb.COLUMN_TYPE_BUILTIN})
			scanset := expectedPlan.Add(&physicalpb.ScanSet{
				Id: setID,
				Targets: []*physicalpb.ScanTarget{
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				},
				Projections: projected,
			})

			rangeAgg := expectedPlan.Add(&physicalpb.AggregateRange{
				Id:          range1ID,
				Operation:   physicalpb.AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: partitionBy,
			})

			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: scanset.GetScanSet()})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("filter -> scanset", func(t *testing.T) {
		filterPredicates := []*physicalpb.Expression{
			(&physicalpb.BinaryExpression{
				Left:  (&physicalpb.ColumnExpression{Name: "level", Type: physicalpb.COLUMN_TYPE_LABEL}).ToExpression(),
				Right: NewLiteral("error").ToExpression(),
				Op:    physicalpb.BINARY_OP_EQ,
			}).ToExpression(),
			(&physicalpb.BinaryExpression{
				Left:  (&physicalpb.ColumnExpression{Name: "message", Type: physicalpb.COLUMN_TYPE_BUILTIN}).ToExpression(),
				Right: NewLiteral(".*exception.*").ToExpression(),
				Op:    physicalpb.BINARY_OP_MATCH_RE,
			}).ToExpression(),
		}

		setID := physicalpb.PlanNodeID{Value: ulid.New()}
		filter1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		range1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		plan := &physicalpb.Plan{}
		{
			scanset := plan.Add(&physicalpb.ScanSet{
				Id: setID,
				Targets: []*physicalpb.ScanTarget{
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				},
				Projections: []*physicalpb.ColumnExpression{
					{Name: "existing", Type: physicalpb.COLUMN_TYPE_LABEL},
				},
			})
			filter := plan.Add(&physicalpb.Filter{
				Id:         filter1ID,
				Predicates: filterPredicates,
			})
			rangeAgg := plan.Add(&physicalpb.AggregateRange{
				Id:        range1ID,
				Operation: physicalpb.AGGREGATE_RANGE_OP_COUNT,
			})

			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: filter.GetFilter()})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: scanset.GetScanSet()})
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
				{Name: "existing", Type: physicalpb.COLUMN_TYPE_LABEL},
				{Name: "level", Type: physicalpb.COLUMN_TYPE_LABEL},
				{Name: "message", Type: physicalpb.COLUMN_TYPE_BUILTIN},
				{Name: types.ColumnNameBuiltinTimestamp, Type: physicalpb.COLUMN_TYPE_BUILTIN},
			}

			scanset := expectedPlan.Add(&physicalpb.ScanSet{
				Id: setID,
				Targets: []*physicalpb.ScanTarget{
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				},
				Projections: expectedProjections,
			})
			filter := expectedPlan.Add(&physicalpb.Filter{
				Id:         filter1ID,
				Predicates: filterPredicates,
			})
			rangeAgg := expectedPlan.Add(&physicalpb.AggregateRange{
				Id:        range1ID,
				Operation: physicalpb.AGGREGATE_RANGE_OP_COUNT,
			})

			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: filter.GetFilter()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter.GetFilter(), Child: scanset.GetScanSet()})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("unwrap -> scanset", func(t *testing.T) {
		setID := physicalpb.PlanNodeID{Value: ulid.New()}
		project1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		range1ID := physicalpb.PlanNodeID{Value: ulid.New()}
		plan := &physicalpb.Plan{}
		{
			scanset := plan.Add(&physicalpb.ScanSet{
				Id: setID,
				Targets: []*physicalpb.ScanTarget{
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				},
				Projections: []*physicalpb.ColumnExpression{
					{Name: "existing", Type: physicalpb.COLUMN_TYPE_LABEL},
				},
			})
			project := plan.Add(&physicalpb.Projection{
				Id:     project1ID,
				Expand: true,
				Expressions: []*physicalpb.Expression{
					(&physicalpb.UnaryExpression{
						Op:    physicalpb.UNARY_OP_CAST_FLOAT,
						Value: (&physicalpb.ColumnExpression{Name: "rows", Type: physicalpb.COLUMN_TYPE_AMBIGUOUS}).ToExpression(),
					}).ToExpression(),
				},
			})

			rangeAgg := plan.Add(&physicalpb.AggregateRange{
				Id:        range1ID,
				Operation: physicalpb.AGGREGATE_RANGE_OP_COUNT,
			})

			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: project.GetProjection()})
			_ = plan.AddEdge(dag.Edge[physicalpb.Node]{Parent: project.GetProjection(), Child: scanset.GetScanSet()})
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
				{Name: "existing", Type: physicalpb.COLUMN_TYPE_LABEL},
				{Name: "rows", Type: physicalpb.COLUMN_TYPE_AMBIGUOUS},
				{Name: types.ColumnNameBuiltinTimestamp, Type: physicalpb.COLUMN_TYPE_BUILTIN},
			}

			scanset := expectedPlan.Add(&physicalpb.ScanSet{
				Id: setID,
				Targets: []*physicalpb.ScanTarget{
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
					{Type: physicalpb.SCAN_TYPE_DATA_OBJECT, DataObject: &physicalpb.DataObjScan{}},
				},
				Projections: expectedProjections,
			})
			project := expectedPlan.Add(&physicalpb.Projection{
				Id:     project1ID,
				Expand: true,
				Expressions: []*physicalpb.Expression{
					(&physicalpb.UnaryExpression{
						Op:    physicalpb.UNARY_OP_CAST_FLOAT,
						Value: (&physicalpb.ColumnExpression{Name: "rows", Type: physicalpb.COLUMN_TYPE_AMBIGUOUS}).ToExpression(),
					}).ToExpression(),
				},
			})
			rangeAgg := expectedPlan.Add(&physicalpb.AggregateRange{
				Id:        range1ID,
				Operation: physicalpb.AGGREGATE_RANGE_OP_COUNT,
			})

			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: rangeAgg.GetAggregateRange(), Child: project.GetProjection()})
			_ = expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(project), Child: physicalpb.GetNode(scanset)})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
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
			name: "ParseNode extracts all keys for log queries",
			buildLogical: func() logical.Value {
				// Create a logical plan that represents:
				// {app="test"} | logfmt | level="error"
				// This is a log query (no RangeAggregation) so should parse all keys
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
				builder = builder.RangeAggregation(
					nil,
					types.RangeAggregationTypeCount,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute,
					5*time.Minute,
				)

				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"level"},
			expectedDataObjScanProjections: []string{"app", "level", "message", "timestamp"},
		},
		{
			name: "ParseNode collects AggregateRange PartitionBy ambiguous columns",
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
			expectedDataObjScanProjections: []string{"duration", "message", "service", "timestamp"},
		},
		{
			name: "ParseNode collects ambiguous columns from RangeAggregation and Filter",
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
			expectedDataObjScanProjections: []string{"code", "duration", "message", "status", "timestamp"},
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
			for i := range 10 {
				catalog.sectionDescriptors = append(catalog.sectionDescriptors, &metastore.DataobjSectionDescriptor{
					SectionKey: metastore.SectionKey{ObjectPath: "/test/object", SectionIdx: int64(i)},
					StreamIDs:  []int64{1, 2},
					Start:      time.Unix(0, 0),
					End:        time.Unix(3600, 0),
				})
			}
			ctx := NewContext(time.Unix(0, 0), time.Unix(3600, 0))
			planner := NewPlanner(ctx, catalog)
			if tt.expectedParseKeysRequested != nil {
				fmt.Println("Interesting test case")
			}

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

func TestRemoveNoopFilter(t *testing.T) {
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

	_ = optimized.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter2.GetFilter(), Child: filter1.GetFilter()})
	_ = optimized.AddEdge(dag.Edge[physicalpb.Node]{Parent: filter1.GetFilter(), Child: scanSet.GetScanSet()})

	expected := PrintAsTree(optimized)
	require.Equal(t, expected, actual)
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
				children: []physicalpb.Node{&physicalpb.DataObjScan{Id: physicalpb.PlanNodeID{Value: ulid.New()}}},
				expected: false,
			},
			{
				name:     "one child (Parallelize)",
				children: []physicalpb.Node{&physicalpb.Parallelize{Id: physicalpb.PlanNodeID{Value: ulid.New()}}},
				expected: true,
			},
			{
				name:     "multiple children (all Parallelize)",
				children: []physicalpb.Node{&physicalpb.Parallelize{Id: physicalpb.PlanNodeID{Value: ulid.New()}}, &physicalpb.Parallelize{Id: physicalpb.PlanNodeID{Value: ulid.New()}}},
				expected: true,
			},
			{
				name:     "multiple children (not all Parallelize)",
				children: []physicalpb.Node{&physicalpb.Parallelize{Id: physicalpb.PlanNodeID{Value: ulid.New()}}, &physicalpb.DataObjScan{Id: physicalpb.PlanNodeID{Value: ulid.New()}}},
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
		vectorAggID := physicalpb.PlanNodeID{Value: ulid.New()}
		rangeAggID := physicalpb.PlanNodeID{Value: ulid.New()}
		filterID := physicalpb.PlanNodeID{Value: ulid.New()}
		parallelizeID := physicalpb.PlanNodeID{Value: ulid.New()}
		scanID := physicalpb.PlanNodeID{Value: ulid.New()}
		{
			vectorAgg := plan.Add(&physicalpb.AggregateVector{Id: vectorAggID})
			rangeAgg := plan.Add(&physicalpb.AggregateRange{Id: rangeAggID})
			filter := plan.Add(&physicalpb.Filter{Id: filterID})
			parallelize := plan.Add(&physicalpb.Parallelize{Id: parallelizeID})
			scan := plan.Add(&physicalpb.DataObjScan{Id: scanID})

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
			vectorAgg := expectedPlan.Add(&physicalpb.AggregateVector{Id: vectorAggID})
			rangeAgg := expectedPlan.Add(&physicalpb.AggregateRange{Id: rangeAggID})
			parallelize := expectedPlan.Add(&physicalpb.Parallelize{Id: parallelizeID})
			filter := expectedPlan.Add(&physicalpb.Filter{Id: filterID})
			scan := expectedPlan.Add(&physicalpb.DataObjScan{Id: scanID})

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
		vectorAggID := physicalpb.PlanNodeID{Value: ulid.New()}
		rangeAggID := physicalpb.PlanNodeID{Value: ulid.New()}
		parseID := physicalpb.PlanNodeID{Value: ulid.New()}
		parallelizeID := physicalpb.PlanNodeID{Value: ulid.New()}
		scanID := physicalpb.PlanNodeID{Value: ulid.New()}
		{
			vectorAgg := plan.Add(&physicalpb.AggregateVector{Id: vectorAggID})
			rangeAgg := plan.Add(&physicalpb.AggregateRange{Id: rangeAggID})
			parse := plan.Add(&physicalpb.Parse{Id: parseID})
			parallelize := plan.Add(&physicalpb.Parallelize{Id: parallelizeID})
			scan := plan.Add(&physicalpb.DataObjScan{Id: scanID})

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
			vectorAgg := expectedPlan.Add(&physicalpb.AggregateVector{Id: vectorAggID})
			rangeAgg := expectedPlan.Add(&physicalpb.AggregateRange{Id: rangeAggID})
			parallelize := expectedPlan.Add(&physicalpb.Parallelize{Id: parallelizeID})
			parse := expectedPlan.Add(&physicalpb.Parse{Id: parseID})
			scan := expectedPlan.Add(&physicalpb.DataObjScan{Id: scanID})

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
		limitID := physicalpb.PlanNodeID{Value: ulid.New()}
		topKID := physicalpb.PlanNodeID{Value: ulid.New()}
		parallelizeID := physicalpb.PlanNodeID{Value: ulid.New()}
		scanID := physicalpb.PlanNodeID{Value: ulid.New()}
		globalTopKID := physicalpb.PlanNodeID{Value: ulid.New()}
		localTopKID := physicalpb.PlanNodeID{Value: ulid.New()}
		{
			limit := plan.Add(&physicalpb.Limit{Id: limitID})
			topk := plan.Add(&physicalpb.TopK{SortBy: &physicalpb.ColumnExpression{}, Id: topKID})
			parallelize := plan.Add(&physicalpb.Parallelize{Id: parallelizeID})
			scan := plan.Add(&physicalpb.DataObjScan{Id: scanID})

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
			limit := expectedPlan.Add(&physicalpb.Limit{Id: limitID})
			globalTopK := expectedPlan.Add(&physicalpb.TopK{Id: globalTopKID, SortBy: &physicalpb.ColumnExpression{}})
			parallelize := expectedPlan.Add(&physicalpb.Parallelize{Id: parallelizeID})
			localTopK := expectedPlan.Add(&physicalpb.TopK{Id: localTopKID, SortBy: &physicalpb.ColumnExpression{}})
			scan := expectedPlan.Add(&physicalpb.DataObjScan{Id: scanID})

			require.NoError(t, expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(limit), Child: physicalpb.GetNode(globalTopK)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(globalTopK), Child: physicalpb.GetNode(parallelize)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(parallelize), Child: physicalpb.GetNode(localTopK)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[physicalpb.Node]{Parent: physicalpb.GetNode(localTopK), Child: physicalpb.GetNode(scan)}))
		}

		expected := PrintAsTree(&expectedPlan)
		require.Equal(t, expected, PrintAsTree(&plan))
	})
}
