package physical

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestCanApplyPredicate(t *testing.T) {
	tests := []struct {
		predicate Expression
		want      bool
	}{
		{
			predicate: NewLiteral(int64(123)),
			want:      true,
		},
		{
			predicate: newColumnExpr("timestamp", types.ColumnTypeBuiltin),
			want:      true,
		},
		{
			predicate: newColumnExpr("foo", types.ColumnTypeLabel),
			want:      false,
		},
		{
			predicate: &BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(types.Timestamp(3600000)),
				Op:    types.BinaryOpGt,
			},
			want: true,
		},
		{
			predicate: &BinaryExpr{
				Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
				Right: NewLiteral("debug|info"),
				Op:    types.BinaryOpMatchRe,
			},
			want: false,
		},
		{
			predicate: &BinaryExpr{
				Left:  newColumnExpr("level", types.ColumnTypeMetadata),
				Right: NewLiteral("debug|info"),
				Op:    types.BinaryOpMatchRe,
			},
			want: true,
		},
		{
			predicate: &BinaryExpr{
				Left:  newColumnExpr("foo", types.ColumnTypeLabel),
				Right: NewLiteral("bar"),
				Op:    types.BinaryOpEq,
			},
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
		Targets: []*ScanTarget{
			{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
			{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
		},
	})
	filter1 := plan.graph.Add(&Filter{Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
			Right: NewLiteral(time1000),
			Op:    types.BinaryOpGt,
		},
	}})
	filter2 := plan.graph.Add(&Filter{Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
			Right: NewLiteral("debug|info"),
			Op:    types.BinaryOpMatchRe,
		},
	}})
	filter3 := plan.graph.Add(&Filter{Predicates: []Expression{}})

	_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter3, Child: filter2})
	_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter2, Child: filter1})
	_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter1, Child: scanSet})

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
	scanSet := optimized.graph.Add(&ScanSet{
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
	filter1 := optimized.graph.Add(&Filter{Predicates: []Expression{}})
	filter2 := optimized.graph.Add(&Filter{Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
			Right: NewLiteral("debug|info"),
			Op:    types.BinaryOpMatchRe,
		},
	}}) // ambiguous column predicates are not pushed down.
	filter3 := optimized.graph.Add(&Filter{Predicates: []Expression{}})

	_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter3, Child: filter2})
	_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter2, Child: filter1})
	_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter1, Child: scanSet})

	expected := PrintAsTree(optimized)
	require.Equal(t, expected, actual)
}

func TestLimitPushdown(t *testing.T) {
	t.Run("pushdown limit to target nodes", func(t *testing.T) {
		plan := &Plan{}
		{
			scanset := plan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
			})
			topK1 := plan.graph.Add(&TopK{SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin)})
			topK2 := plan.graph.Add(&TopK{SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin)})
			limit := plan.graph.Add(&Limit{Fetch: 100})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: topK1, Child: scanset})
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

		expectedPlan := &Plan{}
		{
			scanset := expectedPlan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
			})
			topK1 := expectedPlan.graph.Add(&TopK{SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin), K: 100})
			topK2 := expectedPlan.graph.Add(&TopK{SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin), K: 100})
			limit := expectedPlan.graph.Add(&Limit{Fetch: 100})

			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: topK1})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: topK2})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: topK1, Child: scanset})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("pushdown blocked by filter nodes", func(t *testing.T) {
		// Limit should not be propagated to child nodes when there are filters
		filterPredicates := []Expression{
			&BinaryExpr{
				Left:  &ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
				Right: NewLiteral("error"),
				Op:    types.BinaryOpEq,
			},
		}

		plan := &Plan{}
		{
			scanset := plan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
			})
			topK1 := plan.graph.Add(&TopK{SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin)})
			topK2 := plan.graph.Add(&TopK{SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin)})
			filter := plan.graph.Add(&Filter{
				Predicates: filterPredicates,
			})
			limit := plan.graph.Add(&Limit{Fetch: 100})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: filter})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: topK1})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: topK2})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: topK1, Child: scanset})
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
		grouping := Grouping{
			Columns: []ColumnExpression{
				&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
				&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
			},
			Without: false,
		}

		// generate plan for sum by(service, instance) (count_over_time{...}[])
		plan := &Plan{}
		{
			scanSet := plan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},

				Predicates: []Expression{},
			})
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeCount,
			})
			vectorAgg := plan.graph.Add(&VectorAggregation{
				Operation: types.VectorAggregationTypeSum,
				Grouping:  grouping,
			})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scanSet})
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
			scanSet := expectedPlan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
				Predicates: []Expression{},
			})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeCount,
				Grouping:  grouping,
			})
			vectorAgg := expectedPlan.graph.Add(&VectorAggregation{
				Operation: types.VectorAggregationTypeSum,
				Grouping:  grouping,
			})

			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scanSet})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("MAX->SUM is not allowed", func(t *testing.T) {
		grouping := Grouping{
			Columns: []ColumnExpression{
				&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
			},
			Without: false,
		}

		// generate plan for max by(service) (sum_over_time{...}[])
		plan := &Plan{}
		{
			scanSet := plan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
				Predicates: []Expression{},
			})
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeSum,
			})
			vectorAgg := plan.graph.Add(&VectorAggregation{
				Operation: types.VectorAggregationTypeMax,
				Grouping:  grouping,
			})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scanSet})
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
		grouping := Grouping{
			Columns: []ColumnExpression{
				&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
				&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
			},
			Without: false,
		}

		plan := &Plan{}
		{
			scanset := plan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
			})
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeCount,
				Grouping:  grouping,
			})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scanset})
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
			projected := append(grouping.Columns, &ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}})
			scanset := expectedPlan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
				Projections: projected,
			})

			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeCount,
				Grouping:  grouping,
			})

			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scanset})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("filter -> scanset", func(t *testing.T) {
		filterPredicates := []Expression{
			&BinaryExpr{
				Left:  &ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
				Right: NewLiteral("error"),
				Op:    types.BinaryOpEq,
			},
			&BinaryExpr{
				Left:  &ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
				Right: NewLiteral(".*exception.*"),
				Op:    types.BinaryOpMatchRe,
			},
		}

		plan := &Plan{}
		{
			scanset := plan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
				Projections: []ColumnExpression{
					&ColumnExpr{Ref: types.ColumnRef{Column: "existing", Type: types.ColumnTypeLabel}},
				},
			})
			filter := plan.graph.Add(&Filter{
				Predicates: filterPredicates,
			})
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeCount,
			})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: filter})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scanset})
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
			expectedProjections := []ColumnExpression{
				&ColumnExpr{Ref: types.ColumnRef{Column: "existing", Type: types.ColumnTypeLabel}},
				&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
				&ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
				&ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}},
			}

			scanset := expectedPlan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
				Projections: expectedProjections,
			})
			filter := expectedPlan.graph.Add(&Filter{
				Predicates: filterPredicates,
			})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeCount,
			})

			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: filter})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scanset})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("unwrap -> scanset", func(t *testing.T) {
		plan := &Plan{}
		{
			scanset := plan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
				Projections: []ColumnExpression{
					&ColumnExpr{Ref: types.ColumnRef{Column: "existing", Type: types.ColumnTypeLabel}},
				},
			})
			project := plan.graph.Add(&Projection{
				Expand: true,
				Expressions: []Expression{
					&UnaryExpr{
						Op:   types.UnaryOpCastFloat,
						Left: &ColumnExpr{Ref: types.ColumnRef{Column: "rows", Type: types.ColumnTypeAmbiguous}},
					},
				},
			})

			rangeAgg := plan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeCount,
			})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: project})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: project, Child: scanset})
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
			expectedProjections := []ColumnExpression{
				&ColumnExpr{Ref: types.ColumnRef{Column: "existing", Type: types.ColumnTypeLabel}},
				&ColumnExpr{Ref: types.ColumnRef{Column: "rows", Type: types.ColumnTypeAmbiguous}},
				&ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}},
			}

			scanset := expectedPlan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
				Projections: expectedProjections,
			})
			project := expectedPlan.graph.Add(&Projection{
				Expand: true,
				Expressions: []Expression{
					&UnaryExpr{
						Op:   types.UnaryOpCastFloat,
						Left: &ColumnExpr{Ref: types.ColumnRef{Column: "rows", Type: types.ColumnTypeAmbiguous}},
					},
				},
			})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeCount,
			})

			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: project})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: project, Child: scanset})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})
}

func TestProjectionPushdown_PushesRequestedKeysToParseOperations(t *testing.T) {
	tests := []struct {
		name                           string
		buildLogical                   func() logical.Value
		expectedParseKeysRequested     []string
		expectedDataObjScanProjections []string
	}{
		{
			name: "requested keys remain empty when no operations need parsed fields",
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
				builder = builder.Parse(types.VariadicOpParseLogfmt, false, false)
				return builder.Value()
			},
		},
		{
			name: "parse operation extracts all keys for log queries",
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
				builder = builder.Parse(types.VariadicOpParseLogfmt, false, false)

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
			name: "skips label and builtin columns, only collects ambiguous",
			buildLogical: func() logical.Value {
				// {app="test"} | logfmt | app="frontend" | level="error"
				// This is a log query (no RangeAggregation) so should parse all keys
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})

				builder = builder.Parse(types.VariadicOpParseLogfmt, false, false)

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
			name: "RangeAggregation with GroupBy on ambiguous columns",
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

				builder = builder.Parse(types.VariadicOpParseLogfmt, false, false)

				// Range aggregation with GroupBy
				builder = builder.RangeAggregation(
					logical.Grouping{
						Columns: []logical.ColumnRef{
							{Ref: types.ColumnRef{Column: "duration", Type: types.ColumnTypeAmbiguous}},
							{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}}, // Label should be skipped
						},
						Without: false,
					},
					types.RangeAggregationTypeCount,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute,
					5*time.Minute,
				)

				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"duration"}, // Only ambiguous column from GroupBy
			expectedDataObjScanProjections: []string{"duration", "message", "service", "timestamp"},
		},
		{
			name: "parse operation collects ambiguous columns from RangeAggregation and Filter",
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
				builder = builder.Parse(types.VariadicOpParseLogfmt, false, false)

				// Add filter with ambiguous column
				filterExpr := &logical.BinOp{
					Left:  logical.NewColumnRef("duration", types.ColumnTypeAmbiguous),
					Right: logical.NewLiteral(int64(100)),
					Op:    types.BinaryOpGt,
				}
				builder = builder.Select(filterExpr)

				// Range aggregation
				builder = builder.RangeAggregation(
					logical.NoGrouping,
					types.RangeAggregationTypeCount,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute, // step
					5*time.Minute, // range interval
				)

				// Vector aggregation with groupby on ambiguous columns
				builder = builder.VectorAggregation(
					logical.Grouping{
						Columns: []logical.ColumnRef{
							{Ref: types.ColumnRef{Column: "status", Type: types.ColumnTypeAmbiguous}},
							{Ref: types.ColumnRef{Column: "code", Type: types.ColumnTypeAmbiguous}},
						},
						Without: false,
					},
					types.VectorAggregationTypeSum,
				)
				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"code", "duration", "status"}, // sorted alphabetically
			expectedDataObjScanProjections: []string{"code", "duration", "message", "status", "timestamp"},
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
				builder = builder.Parse(types.VariadicOpParseLogfmt, false, false)

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

			// Build physical plan
			physicalPlan, err := planner.Build(logicalPlan)
			require.NoError(t, err)

			// Optimize the plan - this should apply parseKeysPushdown
			optimizedPlan, err := planner.Optimize(physicalPlan)
			require.NoError(t, err)

			var projectionNode *Projection
			projections := map[string]struct{}{}
			for node := range optimizedPlan.graph.Nodes() {
				if pn, ok := node.(*Projection); ok {
					projectionNode = pn
					continue
				}
				if pn, ok := node.(*ScanSet); ok {
					for _, colExpr := range pn.Projections {
						expr := colExpr.(*ColumnExpr)
						projections[expr.Ref.Column] = struct{}{}
					}
				}
			}

			require.NotNil(t, projectionNode, "Projection not found in plan")
			var requestedKeys *LiteralExpr
			for _, expr := range projectionNode.Expressions {
				switch expr := expr.(type) {
				case *VariadicExpr:
					// Parse expressions: [sourceCol, requestedKeys, strict, keepEmpty]
					// We want the requestedKeys (index 1)
					if len(expr.Expressions) >= 2 {
						if e, ok := expr.Expressions[1].(*LiteralExpr); ok {
							requestedKeys = e
						}
					}
				}
			}

			if len(tt.expectedParseKeysRequested) == 0 {
				// When no keys are requested, we expect either nil or a NullLiteral or an empty list
				if requestedKeys != nil {
					switch lit := requestedKeys.Literal().(type) {
					case types.NullLiteral:
						// OK - null literal
					case types.StringListLiteral:
						require.Empty(t, lit.Value(), "Projection should have no requested keys")
					default:
						t.Fatalf("Unexpected literal type: %T", requestedKeys.Literal)
					}
				}
			} else {
				require.NotNil(t, requestedKeys, "Projection should have requested keys")
				actual := requestedKeys.Literal().(types.StringListLiteral)
				require.Equal(t, tt.expectedParseKeysRequested, actual.Value())
			}

			var projectionArr []string
			for column := range projections {
				projectionArr = append(projectionArr, column)
			}
			sort.Strings(projectionArr)
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
	scanSet := optimized.graph.Add(&ScanSet{
		Targets: []*ScanTarget{
			{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
			{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
		},

		Predicates: []Expression{},
	})
	filter1 := optimized.graph.Add(&Filter{Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
			Right: NewLiteral(time1000),
			Op:    types.BinaryOpGt,
		},
	}})
	filter2 := optimized.graph.Add(&Filter{Predicates: []Expression{
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
