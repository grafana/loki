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
		predicate Expression
		want      bool
	}{
		{
			predicate: *NewLiteral(int64(123)).ToExpression(),
			want:      true,
		},
		{
			predicate: *newColumnExpr("timestamp", COLUMN_TYPE_BUILTIN).ToExpression(),
			want:      true,
		},
		{
			predicate: *newColumnExpr("foo", COLUMN_TYPE_LABEL).ToExpression(),
			want:      false,
		},
		{
			predicate: *newBinaryExpr(newColumnExpr("timestamp", COLUMN_TYPE_BUILTIN).ToExpression(),
				NewLiteral(types.Timestamp(3600000)).ToExpression(),
				BINARY_OP_GT,
			).ToExpression(),
			want: true,
		},
		{
			predicate: *(newBinaryExpr(newColumnExpr("level", COLUMN_TYPE_AMBIGUOUS).ToExpression(),
				NewLiteral("debug|info").ToExpression(),
				BINARY_OP_MATCH_RE,
			)).ToExpression(),
			want: false,
		},
		{
			predicate: *newBinaryExpr(newColumnExpr("level", COLUMN_TYPE_METADATA).ToExpression(),
				NewLiteral("debug|info").ToExpression(),
				BINARY_OP_MATCH_RE,
			).ToExpression(),
			want: true,
		},
		{
			predicate: *newBinaryExpr(newColumnExpr("foo", COLUMN_TYPE_LABEL).ToExpression(),
				NewLiteral("bar").ToExpression(),
				BINARY_OP_EQ,
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

var setID = PlanNodeID{Value: ulid.New()}
var filter1ID = PlanNodeID{Value: ulid.New()}
var filter2ID = PlanNodeID{Value: ulid.New()}
var filter3ID = PlanNodeID{Value: ulid.New()}

func dummyPlan() *Plan {

	plan := &Plan{}

	scanSet := plan.Add(&ScanSet{
		Id: setID,
		Targets: []*ScanTarget{
			{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
			{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
		},
	})
	filter1 := plan.Add(&Filter{Id: filter1ID, Predicates: []*Expression{
		(&BinaryExpression{
			Left:  newColumnExpr("timestamp", COLUMN_TYPE_BUILTIN).ToExpression(),
			Right: NewLiteral(time1000).ToExpression(),
			Op:    BINARY_OP_GT,
		}).ToExpression(),
	}})
	filter2 := plan.Add(&Filter{Id: filter2ID, Predicates: []*Expression{
		(&BinaryExpression{
			Left:  newColumnExpr("level", COLUMN_TYPE_AMBIGUOUS).ToExpression(),
			Right: NewLiteral("debug|info").ToExpression(),
			Op:    BINARY_OP_MATCH_RE,
		}).ToExpression(),
	}})
	filter3 := plan.Add(&Filter{Id: filter3ID, Predicates: []*Expression{}})

	_ = plan.AddEdge(dag.Edge[Node]{Parent: filter3.GetFilter(), Child: filter2.GetFilter()})
	_ = plan.AddEdge(dag.Edge[Node]{Parent: filter2.GetFilter(), Child: filter1.GetFilter()})
	_ = plan.AddEdge(dag.Edge[Node]{Parent: filter1.GetFilter(), Child: scanSet.GetScanSet()})

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

	optimized := &Plan{}
	scanSet := optimized.Add(&ScanSet{
		Id: setID,
		Targets: []*ScanTarget{
			{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
			{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
		},

		Predicates: []*Expression{
			(&BinaryExpression{
				Left:  newColumnExpr("timestamp", COLUMN_TYPE_BUILTIN).ToExpression(),
				Right: NewLiteral(time1000).ToExpression(),
				Op:    BINARY_OP_GT,
			}).ToExpression(),
		},
	})
	filter1 := optimized.Add(&Filter{Id: filter1ID, Predicates: []*Expression{}})
	filter2 := optimized.Add(&Filter{Id: filter2ID, Predicates: []*Expression{
		(&BinaryExpression{
			Left:  newColumnExpr("level", COLUMN_TYPE_AMBIGUOUS).ToExpression(),
			Right: NewLiteral("debug|info").ToExpression(),
			Op:    BINARY_OP_MATCH_RE,
		}).ToExpression(),
	}}) // ambiguous column predicates are not pushed down.
	filter3 := optimized.Add(&Filter{Id: filter3ID, Predicates: []*Expression{}})

	_ = optimized.AddEdge(dag.Edge[Node]{Parent: GetNode(filter3), Child: GetNode(filter2)})
	_ = optimized.AddEdge(dag.Edge[Node]{Parent: GetNode(filter2), Child: GetNode(filter1)})
	_ = optimized.AddEdge(dag.Edge[Node]{Parent: GetNode(filter1), Child: GetNode(scanSet)})

	expected := PrintAsTree(optimized)
	require.Equal(t, expected, actual)
}

func TestLimitPushdown(t *testing.T) {
	t.Run("pushdown limit to target nodes", func(t *testing.T) {
		setID := PlanNodeID{Value: ulid.New()}
		topK1ID := PlanNodeID{Value: ulid.New()}
		topK2ID := PlanNodeID{Value: ulid.New()}
		limit1ID := PlanNodeID{Value: ulid.New()}
		plan := &Plan{}
		{
			scanset := plan.Add(&ScanSet{
				Id: setID,
				Targets: []*ScanTarget{
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
				},
			})
			topK1 := plan.Add(&TopK{Id: topK1ID, SortBy: newColumnExpr("timestamp", COLUMN_TYPE_BUILTIN)})
			topK2 := plan.Add(&TopK{Id: topK2ID, SortBy: newColumnExpr("timestamp", COLUMN_TYPE_BUILTIN)})
			limit := plan.Add(&Limit{Id: limit1ID, Fetch: 100})

			_ = plan.AddEdge(dag.Edge[Node]{Parent: topK1.GetTopK(), Child: scanset.GetScanSet()})
			_ = plan.AddEdge(dag.Edge[Node]{Parent: limit.GetLimit(), Child: topK1.GetTopK()})
			_ = plan.AddEdge(dag.Edge[Node]{Parent: limit.GetLimit(), Child: topK2.GetTopK()})
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
			scanset := expectedPlan.Add(&ScanSet{
				Id: setID,
				Targets: []*ScanTarget{
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
				},
			})
			topK1 := expectedPlan.Add(&TopK{Id: topK1ID, SortBy: newColumnExpr("timestamp", COLUMN_TYPE_BUILTIN), K: 100})
			topK2 := expectedPlan.Add(&TopK{Id: topK2ID, SortBy: newColumnExpr("timestamp", COLUMN_TYPE_BUILTIN), K: 100})
			limit := expectedPlan.Add(&Limit{Id: limit1ID, Fetch: 100})

			_ = expectedPlan.AddEdge(dag.Edge[Node]{Parent: limit.GetLimit(), Child: topK1.GetTopK()})
			_ = expectedPlan.AddEdge(dag.Edge[Node]{Parent: limit.GetLimit(), Child: topK2.GetTopK()})
			_ = expectedPlan.AddEdge(dag.Edge[Node]{Parent: topK1.GetTopK(), Child: scanset.GetScanSet()})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("pushdown blocked by filter nodes", func(t *testing.T) {
		// Limit should not be propagated to child nodes when there are filters
		filterPredicates := []*Expression{
			(&BinaryExpression{
				Left:  (&ColumnExpression{Name: "level", Type: COLUMN_TYPE_LABEL}).ToExpression(),
				Right: NewLiteral("error").ToExpression(),
				Op:    BINARY_OP_EQ,
			}).ToExpression(),
		}
		setID := PlanNodeID{Value: ulid.New()}
		topK1ID := PlanNodeID{Value: ulid.New()}
		topK2ID := PlanNodeID{Value: ulid.New()}
		limit1ID := PlanNodeID{Value: ulid.New()}
		filter1ID := PlanNodeID{Value: ulid.New()}

		plan := &Plan{}
		{
			scanset := plan.Add(&ScanSet{
				Id: setID,
				Targets: []*ScanTarget{
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
				},
			})
			topK1 := plan.Add(&TopK{Id: topK1ID, SortBy: newColumnExpr("timestamp", COLUMN_TYPE_BUILTIN)})
			topK2 := plan.Add(&TopK{Id: topK2ID, SortBy: newColumnExpr("timestamp", COLUMN_TYPE_BUILTIN)})
			filter := plan.Add(&Filter{
				Id:         filter1ID,
				Predicates: filterPredicates,
			})
			limit := plan.Add(&Limit{Id: limit1ID, Fetch: 100})

			_ = plan.AddEdge(dag.Edge[Node]{Parent: limit.GetLimit(), Child: filter.GetFilter()})
			_ = plan.AddEdge(dag.Edge[Node]{Parent: filter.GetFilter(), Child: topK1.GetTopK()})
			_ = plan.AddEdge(dag.Edge[Node]{Parent: filter.GetFilter(), Child: topK2.GetTopK()})
			_ = plan.AddEdge(dag.Edge[Node]{Parent: topK1.GetTopK(), Child: scanset.GetScanSet()})
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
		setID := PlanNodeID{Value: ulid.New()}
		rangeAggID := PlanNodeID{Value: ulid.New()}
		sumOfID := PlanNodeID{Value: ulid.New()}

		groupBy := []*ColumnExpression{
			newColumnExpr("service", COLUMN_TYPE_LABEL),
			newColumnExpr("level", COLUMN_TYPE_LABEL),
		}

		// generate plan for sum by(service, instance) (count_over_time{...}[])
		plan := &Plan{}
		{
			scanSet := plan.Add(&ScanSet{
				Id: setID,

				Targets: []*ScanTarget{
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
				},

				Predicates: []*Expression{},
			})
			rangeAgg := plan.Add(&AggregateRange{
				Id:        rangeAggID,
				Operation: AGGREGATE_RANGE_OP_COUNT,
			})
			vectorAgg := plan.Add(&AggregateVector{
				Id:        sumOfID,
				Operation: AGGREGATE_VECTOR_OP_SUM,
				GroupBy:   groupBy,
			})

			_ = plan.AddEdge(dag.Edge[Node]{Parent: vectorAgg.GetAggregateVector(), Child: rangeAgg.GetAggregateRange()})
			_ = plan.AddEdge(dag.Edge[Node]{Parent: rangeAgg.GetAggregateRange(), Child: scanSet.GetScanSet()})
		}
		// apply optimisation
		optimizations := []*optimization{
			newOptimization("groupBy pushdown", plan).withRules(
				&groupByPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			scanSet := expectedPlan.Add(&ScanSet{
				Id: setID,
				Targets: []*ScanTarget{
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
				},
				Predicates: []*Expression{},
			})
			rangeAgg := expectedPlan.Add(&AggregateRange{
				Id:          rangeAggID,
				Operation:   AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: groupBy,
			})
			vectorAgg := expectedPlan.Add(&AggregateVector{
				Id:        sumOfID,
				Operation: AGGREGATE_VECTOR_OP_SUM,
				GroupBy:   groupBy,
			})

			_ = expectedPlan.AddEdge(dag.Edge[Node]{Parent: vectorAgg.GetAggregateVector(), Child: rangeAgg.GetAggregateRange()})
			_ = expectedPlan.AddEdge(dag.Edge[Node]{Parent: rangeAgg.GetAggregateRange(), Child: scanSet.GetScanSet()})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("MAX->SUM is not allowed", func(t *testing.T) {
		setID := PlanNodeID{Value: ulid.New()}
		rangeAggID := PlanNodeID{Value: ulid.New()}
		maxOfID := PlanNodeID{Value: ulid.New()}
		groupBy := []*ColumnExpression{
			newColumnExpr("service", COLUMN_TYPE_LABEL),
		}

		// generate plan for max by(service) (sum_over_time{...}[])
		plan := &Plan{}
		{
			scanSet := plan.Add(&ScanSet{
				Id: setID,
				Targets: []*ScanTarget{
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
				},
				Predicates: []*Expression{},
			})
			rangeAgg := plan.Add(&AggregateRange{
				Id:        rangeAggID,
				Operation: AGGREGATE_RANGE_OP_SUM,
			})
			vectorAgg := plan.Add(&AggregateVector{
				Id:        maxOfID,
				Operation: AGGREGATE_VECTOR_OP_MAX,
				GroupBy:   groupBy,
			})

			_ = plan.AddEdge(dag.Edge[Node]{Parent: vectorAgg.GetAggregateVector(), Child: rangeAgg.GetAggregateRange()})
			_ = plan.AddEdge(dag.Edge[Node]{Parent: rangeAgg.GetAggregateRange(), Child: scanSet.GetScanSet()})
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
		partitionBy := []*ColumnExpression{
			{Name: "level", Type: COLUMN_TYPE_LABEL},
			{Name: "service", Type: COLUMN_TYPE_LABEL},
		}
		setID := PlanNodeID{Value: ulid.New()}
		range1ID := PlanNodeID{Value: ulid.New()}
		plan := &Plan{}
		{
			scanset := plan.Add(&ScanSet{
				Id: setID,
				Targets: []*ScanTarget{
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
				},
			})
			rangeAgg := plan.Add(&AggregateRange{
				Id:          range1ID,
				Operation:   AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: partitionBy,
			})

			_ = plan.AddEdge(dag.Edge[Node]{Parent: rangeAgg.GetAggregateRange(), Child: scanset.GetScanSet()})
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
			projected := append(partitionBy, &ColumnExpression{Name: types.ColumnNameBuiltinTimestamp, Type: COLUMN_TYPE_BUILTIN})
			scanset := expectedPlan.Add(&ScanSet{
				Id: setID,
				Targets: []*ScanTarget{
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
				},
				Projections: projected,
			})

			rangeAgg := expectedPlan.Add(&AggregateRange{
				Id:          range1ID,
				Operation:   AGGREGATE_RANGE_OP_COUNT,
				PartitionBy: partitionBy,
			})

			_ = expectedPlan.AddEdge(dag.Edge[Node]{Parent: rangeAgg.GetAggregateRange(), Child: scanset.GetScanSet()})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("filter -> scanset", func(t *testing.T) {
		filterPredicates := []*Expression{
			(&BinaryExpression{
				Left:  (&ColumnExpression{Name: "level", Type: COLUMN_TYPE_LABEL}).ToExpression(),
				Right: NewLiteral("error").ToExpression(),
				Op:    BINARY_OP_EQ,
			}).ToExpression(),
			(&BinaryExpression{
				Left:  (&ColumnExpression{Name: "message", Type: COLUMN_TYPE_BUILTIN}).ToExpression(),
				Right: NewLiteral(".*exception.*").ToExpression(),
				Op:    BINARY_OP_MATCH_RE,
			}).ToExpression(),
		}

		setID := PlanNodeID{Value: ulid.New()}
		filter1ID := PlanNodeID{Value: ulid.New()}
		range1ID := PlanNodeID{Value: ulid.New()}
		plan := &Plan{}
		{
			scanset := plan.Add(&ScanSet{
				Id: setID,
				Targets: []*ScanTarget{
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
				},
				Projections: []*ColumnExpression{
					{Name: "existing", Type: COLUMN_TYPE_LABEL},
				},
			})
			filter := plan.Add(&Filter{
				Id:         filter1ID,
				Predicates: filterPredicates,
			})
			rangeAgg := plan.Add(&AggregateRange{
				Id:        range1ID,
				Operation: AGGREGATE_RANGE_OP_COUNT,
			})

			_ = plan.AddEdge(dag.Edge[Node]{Parent: rangeAgg.GetAggregateRange(), Child: filter.GetFilter()})
			_ = plan.AddEdge(dag.Edge[Node]{Parent: filter.GetFilter(), Child: scanset.GetScanSet()})
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
			expectedProjections := []*ColumnExpression{
				{Name: "existing", Type: COLUMN_TYPE_LABEL},
				{Name: "level", Type: COLUMN_TYPE_LABEL},
				{Name: "message", Type: COLUMN_TYPE_BUILTIN},
				{Name: types.ColumnNameBuiltinTimestamp, Type: COLUMN_TYPE_BUILTIN},
			}

			scanset := expectedPlan.Add(&ScanSet{
				Id: setID,
				Targets: []*ScanTarget{
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
				},
				Projections: expectedProjections,
			})
			filter := expectedPlan.Add(&Filter{
				Id:         filter1ID,
				Predicates: filterPredicates,
			})
			rangeAgg := expectedPlan.Add(&AggregateRange{
				Id:        range1ID,
				Operation: AGGREGATE_RANGE_OP_COUNT,
			})

			_ = expectedPlan.AddEdge(dag.Edge[Node]{Parent: rangeAgg.GetAggregateRange(), Child: filter.GetFilter()})
			_ = expectedPlan.AddEdge(dag.Edge[Node]{Parent: filter.GetFilter(), Child: scanset.GetScanSet()})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("unwrap -> scanset", func(t *testing.T) {
		setID := PlanNodeID{Value: ulid.New()}
		project1ID := PlanNodeID{Value: ulid.New()}
		range1ID := PlanNodeID{Value: ulid.New()}
		plan := &Plan{}
		{
			scanset := plan.Add(&ScanSet{
				Id: setID,
				Targets: []*ScanTarget{
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
				},
				Projections: []*ColumnExpression{
					{Name: "existing", Type: COLUMN_TYPE_LABEL},
				},
			})
			project := plan.Add(&Projection{
				Id:     project1ID,
				Expand: true,
				Expressions: []*Expression{
					(&UnaryExpression{
						Op:    UNARY_OP_CAST_FLOAT,
						Value: (&ColumnExpression{Name: "rows", Type: COLUMN_TYPE_AMBIGUOUS}).ToExpression(),
					}).ToExpression(),
				},
			})

			rangeAgg := plan.Add(&AggregateRange{
				Id:        range1ID,
				Operation: AGGREGATE_RANGE_OP_COUNT,
			})

			_ = plan.AddEdge(dag.Edge[Node]{Parent: rangeAgg.GetAggregateRange(), Child: project.GetProjection()})
			_ = plan.AddEdge(dag.Edge[Node]{Parent: project.GetProjection(), Child: scanset.GetScanSet()})
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
			expectedProjections := []*ColumnExpression{
				{Name: "existing", Type: COLUMN_TYPE_LABEL},
				{Name: "rows", Type: COLUMN_TYPE_AMBIGUOUS},
				{Name: types.ColumnNameBuiltinTimestamp, Type: COLUMN_TYPE_BUILTIN},
			}

			scanset := expectedPlan.Add(&ScanSet{
				Id: setID,
				Targets: []*ScanTarget{
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
					{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
				},
				Projections: expectedProjections,
			})
			project := expectedPlan.Add(&Projection{
				Id:     project1ID,
				Expand: true,
				Expressions: []*Expression{
					(&UnaryExpression{
						Op:    UNARY_OP_CAST_FLOAT,
						Value: (&ColumnExpression{Name: "rows", Type: COLUMN_TYPE_AMBIGUOUS}).ToExpression(),
					}).ToExpression(),
				},
			})
			rangeAgg := expectedPlan.Add(&AggregateRange{
				Id:        range1ID,
				Operation: AGGREGATE_RANGE_OP_COUNT,
			})

			_ = expectedPlan.AddEdge(dag.Edge[Node]{Parent: rangeAgg.GetAggregateRange(), Child: project.GetProjection()})
			_ = expectedPlan.AddEdge(dag.Edge[Node]{Parent: GetNode(project), Child: GetNode(scanset)})
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
				// This is a log query (no AggregateRange) so should parse all keys
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
			var parseNode *Parse
			projections := map[string]struct{}{}
			for _, node := range optimizedPlan.Nodes {
				switch kind := node.Kind.(type) {
				case *PlanNode_Parse:
					parseNode = kind.Parse
					continue
				}
				if pn, ok := node.Kind.(*PlanNode_ScanSet); ok {
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

	optimized := &Plan{}
	scanSet := optimized.Add(&ScanSet{
		Id: setID,

		Targets: []*ScanTarget{
			{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
			{Type: SCAN_TYPE_DATA_OBJECT, DataObject: &DataObjScan{}},
		},

		Predicates: []*Expression{},
	})
	filter1 := optimized.Add(&Filter{Id: filter1ID, Predicates: []*Expression{
		(&BinaryExpression{
			Left:  newColumnExpr("timestamp", COLUMN_TYPE_BUILTIN).ToExpression(),
			Right: NewLiteral(time1000).ToExpression(),
			Op:    BINARY_OP_GT,
		}).ToExpression(),
	}})
	filter2 := optimized.Add(&Filter{Id: filter2ID, Predicates: []*Expression{
		(&BinaryExpression{
			Left:  newColumnExpr("level", COLUMN_TYPE_AMBIGUOUS).ToExpression(),
			Right: NewLiteral("debug|info").ToExpression(),
			Op:    BINARY_OP_MATCH_RE,
		}).ToExpression(),
	}})

	_ = optimized.AddEdge(dag.Edge[Node]{Parent: filter2.GetFilter(), Child: filter1.GetFilter()})
	_ = optimized.AddEdge(dag.Edge[Node]{Parent: filter1.GetFilter(), Child: scanSet.GetScanSet()})

	expected := PrintAsTree(optimized)
	require.Equal(t, expected, actual)
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
				children: []Node{&DataObjScan{Id: PlanNodeID{Value: ulid.New()}}},
				expected: false,
			},
			{
				name:     "one child (Parallelize)",
				children: []Node{&Parallelize{Id: PlanNodeID{Value: ulid.New()}}},
				expected: true,
			},
			{
				name:     "multiple children (all Parallelize)",
				children: []Node{&Parallelize{Id: PlanNodeID{Value: ulid.New()}}, &Parallelize{Id: PlanNodeID{Value: ulid.New()}}},
				expected: true,
			},
			{
				name:     "multiple children (not all Parallelize)",
				children: []Node{&Parallelize{Id: PlanNodeID{Value: ulid.New()}}, &DataObjScan{Id: PlanNodeID{Value: ulid.New()}}},
				expected: false,
			},
		}

		for _, tc := range tt {
			t.Run(tc.name, func(t *testing.T) {
				var plan Plan
				parent := plan.Add(&Filter{})

				for _, child := range tc.children {
					plan.Add(child)
					require.NoError(t, plan.AddEdge(dag.Edge[Node]{Parent: GetNode(parent), Child: child}))
				}

				pass := parallelPushdown{plan: &plan}
				require.Equal(t, tc.expected, pass.canPushdown(GetNode(parent)))
			})
		}
	})

	t.Run("Shifts Filter", func(t *testing.T) {
		var plan Plan
		vectorAggID := PlanNodeID{Value: ulid.New()}
		rangeAggID := PlanNodeID{Value: ulid.New()}
		filterID := PlanNodeID{Value: ulid.New()}
		parallelizeID := PlanNodeID{Value: ulid.New()}
		scanID := PlanNodeID{Value: ulid.New()}
		{
			vectorAgg := plan.Add(&AggregateVector{Id: vectorAggID})
			rangeAgg := plan.Add(&AggregateRange{Id: rangeAggID})
			filter := plan.Add(&Filter{Id: filterID})
			parallelize := plan.Add(&Parallelize{Id: parallelizeID})
			scan := plan.Add(&DataObjScan{Id: scanID})

			require.NoError(t, plan.AddEdge(dag.Edge[Node]{Parent: GetNode(vectorAgg), Child: GetNode(rangeAgg)}))
			require.NoError(t, plan.AddEdge(dag.Edge[Node]{Parent: GetNode(rangeAgg), Child: GetNode(filter)}))
			require.NoError(t, plan.AddEdge(dag.Edge[Node]{Parent: GetNode(filter), Child: GetNode(parallelize)}))
			require.NoError(t, plan.AddEdge(dag.Edge[Node]{Parent: GetNode(parallelize), Child: GetNode(scan)}))
		}

		opt := newOptimizer(&plan, []*optimization{
			newOptimization("ParallelPushdown", &plan).withRules(&parallelPushdown{plan: &plan}),
		})
		root, _ := plan.Root()
		opt.optimize(root)

		var expectedPlan Plan
		{
			vectorAgg := expectedPlan.Add(&AggregateVector{Id: vectorAggID})
			rangeAgg := expectedPlan.Add(&AggregateRange{Id: rangeAggID})
			parallelize := expectedPlan.Add(&Parallelize{Id: parallelizeID})
			filter := expectedPlan.Add(&Filter{Id: filterID})
			scan := expectedPlan.Add(&DataObjScan{Id: scanID})

			require.NoError(t, expectedPlan.AddEdge(dag.Edge[Node]{Parent: GetNode(vectorAgg), Child: GetNode(rangeAgg)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[Node]{Parent: GetNode(rangeAgg), Child: GetNode(parallelize)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[Node]{Parent: GetNode(parallelize), Child: GetNode(filter)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[Node]{Parent: GetNode(filter), Child: GetNode(scan)}))
		}

		expected := PrintAsTree(&expectedPlan)
		require.Equal(t, expected, PrintAsTree(&plan))
	})

	t.Run("Shifts Parse", func(t *testing.T) {
		var plan Plan
		vectorAggID := PlanNodeID{Value: ulid.New()}
		rangeAggID := PlanNodeID{Value: ulid.New()}
		parseID := PlanNodeID{Value: ulid.New()}
		parallelizeID := PlanNodeID{Value: ulid.New()}
		scanID := PlanNodeID{Value: ulid.New()}
		{
			vectorAgg := plan.Add(&AggregateVector{Id: vectorAggID})
			rangeAgg := plan.Add(&AggregateRange{Id: rangeAggID})
			parse := plan.Add(&Parse{Id: parseID})
			parallelize := plan.Add(&Parallelize{Id: parallelizeID})
			scan := plan.Add(&DataObjScan{Id: scanID})

			require.NoError(t, plan.AddEdge(dag.Edge[Node]{Parent: GetNode(vectorAgg), Child: GetNode(rangeAgg)}))
			require.NoError(t, plan.AddEdge(dag.Edge[Node]{Parent: GetNode(rangeAgg), Child: GetNode(parse)}))
			require.NoError(t, plan.AddEdge(dag.Edge[Node]{Parent: GetNode(parse), Child: GetNode(parallelize)}))
			require.NoError(t, plan.AddEdge(dag.Edge[Node]{Parent: GetNode(parallelize), Child: GetNode(scan)}))
		}

		opt := newOptimizer(&plan, []*optimization{
			newOptimization("ParallelPushdown", &plan).withRules(&parallelPushdown{plan: &plan}),
		})
		root, _ := plan.Root()
		opt.optimize(root)

		var expectedPlan Plan
		{
			vectorAgg := expectedPlan.Add(&AggregateVector{Id: vectorAggID})
			rangeAgg := expectedPlan.Add(&AggregateRange{Id: rangeAggID})
			parallelize := expectedPlan.Add(&Parallelize{Id: parallelizeID})
			parse := expectedPlan.Add(&Parse{Id: parseID})
			scan := expectedPlan.Add(&DataObjScan{Id: scanID})

			require.NoError(t, expectedPlan.AddEdge(dag.Edge[Node]{Parent: GetNode(vectorAgg), Child: GetNode(rangeAgg)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[Node]{Parent: GetNode(rangeAgg), Child: GetNode(parallelize)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[Node]{Parent: GetNode(parallelize), Child: GetNode(parse)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[Node]{Parent: GetNode(parse), Child: GetNode(scan)}))
		}

		expected := PrintAsTree(&expectedPlan)
		require.Equal(t, expected, PrintAsTree(&plan))
	})

	t.Run("Splits TopK", func(t *testing.T) {
		var plan Plan
		limitID := PlanNodeID{Value: ulid.New()}
		topKID := PlanNodeID{Value: ulid.New()}
		parallelizeID := PlanNodeID{Value: ulid.New()}
		scanID := PlanNodeID{Value: ulid.New()}
		globalTopKID := PlanNodeID{Value: ulid.New()}
		localTopKID := PlanNodeID{Value: ulid.New()}
		{
			limit := plan.Add(&Limit{Id: limitID})
			topk := plan.Add(&TopK{SortBy: &ColumnExpression{}, Id: topKID})
			parallelize := plan.Add(&Parallelize{Id: parallelizeID})
			scan := plan.Add(&DataObjScan{Id: scanID})

			require.NoError(t, plan.AddEdge(dag.Edge[Node]{Parent: GetNode(limit), Child: GetNode(topk)}))
			require.NoError(t, plan.AddEdge(dag.Edge[Node]{Parent: GetNode(topk), Child: GetNode(parallelize)}))
			require.NoError(t, plan.AddEdge(dag.Edge[Node]{Parent: GetNode(parallelize), Child: GetNode(scan)}))
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

		var expectedPlan Plan
		{
			limit := expectedPlan.Add(&Limit{Id: limitID})
			globalTopK := expectedPlan.Add(&TopK{Id: globalTopKID, SortBy: &ColumnExpression{}})
			parallelize := expectedPlan.Add(&Parallelize{Id: parallelizeID})
			localTopK := expectedPlan.Add(&TopK{Id: localTopKID, SortBy: &ColumnExpression{}})
			scan := expectedPlan.Add(&DataObjScan{Id: scanID})

			require.NoError(t, expectedPlan.AddEdge(dag.Edge[Node]{Parent: GetNode(limit), Child: GetNode(globalTopK)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[Node]{Parent: GetNode(globalTopK), Child: GetNode(parallelize)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[Node]{Parent: GetNode(parallelize), Child: GetNode(localTopK)}))
			require.NoError(t, expectedPlan.AddEdge(dag.Edge[Node]{Parent: GetNode(localTopK), Child: GetNode(scan)}))
		}

		expected := PrintAsTree(&expectedPlan)
		require.Equal(t, expected, PrintAsTree(&plan))
	})
}
