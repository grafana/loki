package physical

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
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
				Right: NewLiteral(datatype.Timestamp(3600000)),
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

var (
	time1000 = datatype.Timestamp(1000000000)
)

func dummyPlan() *Plan {
	plan := &Plan{}
	scan1 := plan.addNode(&DataObjScan{id: "scan1"})
	scan2 := plan.addNode(&DataObjScan{id: "scan2"})
	merge := plan.addNode(&SortMerge{id: "merge"})
	filter1 := plan.addNode(&Filter{id: "filter1", Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
			Right: NewLiteral(time1000),
			Op:    types.BinaryOpGt,
		},
	}})
	filter2 := plan.addNode(&Filter{id: "filter2", Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
			Right: NewLiteral("debug|info"),
			Op:    types.BinaryOpMatchRe,
		},
	}})
	filter3 := plan.addNode(&Filter{id: "filter3", Predicates: []Expression{}})

	_ = plan.addEdge(Edge{Parent: filter3, Child: filter2})
	_ = plan.addEdge(Edge{Parent: filter2, Child: filter1})
	_ = plan.addEdge(Edge{Parent: filter1, Child: merge})
	_ = plan.addEdge(Edge{Parent: merge, Child: scan1})
	_ = plan.addEdge(Edge{Parent: merge, Child: scan2})

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
		scan1 := optimized.addNode(&DataObjScan{id: "scan1", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time1000),
				Op:    types.BinaryOpGt,
			},
		}})
		scan2 := optimized.addNode(&DataObjScan{id: "scan2", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time1000),
				Op:    types.BinaryOpGt,
			},
		}})
		merge := optimized.addNode(&SortMerge{id: "merge"})
		filter1 := optimized.addNode(&Filter{id: "filter1", Predicates: []Expression{}})
		filter2 := optimized.addNode(&Filter{id: "filter2", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
				Right: NewLiteral("debug|info"),
				Op:    types.BinaryOpMatchRe,
			},
		}})
		filter3 := optimized.addNode(&Filter{id: "filter3", Predicates: []Expression{}})

		_ = optimized.addEdge(Edge{Parent: filter3, Child: filter2})
		_ = optimized.addEdge(Edge{Parent: filter2, Child: filter1})
		_ = optimized.addEdge(Edge{Parent: filter1, Child: merge})
		_ = optimized.addEdge(Edge{Parent: merge, Child: scan1})
		_ = optimized.addEdge(Edge{Parent: merge, Child: scan2})

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
		scan1 := optimized.addNode(&DataObjScan{id: "scan1", Predicates: []Expression{}})
		scan2 := optimized.addNode(&DataObjScan{id: "scan2", Predicates: []Expression{}})
		merge := optimized.addNode(&SortMerge{id: "merge"})
		filter1 := optimized.addNode(&Filter{id: "filter1", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time1000),
				Op:    types.BinaryOpGt,
			},
		}})
		filter2 := optimized.addNode(&Filter{id: "filter2", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
				Right: NewLiteral("debug|info"),
				Op:    types.BinaryOpMatchRe,
			},
		}})

		_ = optimized.addEdge(Edge{Parent: filter2, Child: filter1})
		_ = optimized.addEdge(Edge{Parent: filter1, Child: merge})
		_ = optimized.addEdge(Edge{Parent: merge, Child: scan1})
		_ = optimized.addEdge(Edge{Parent: merge, Child: scan2})

		expected := PrintAsTree(optimized)
		require.Equal(t, expected, actual)
	})

	t.Run("groupby pushdown", func(t *testing.T) {
		groupBy := []ColumnExpression{
			&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
			&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
		}

		// generate plan for sum by(service, instance) (count_over_time{...}[])
		plan := &Plan{}
		{
			scan1 := plan.addNode(&DataObjScan{id: "scan1"})
			rangeAgg := plan.addNode(&RangeAggregation{
				id:        "count_over_time",
				Operation: types.RangeAggregationTypeCount,
			})
			vectorAgg := plan.addNode(&VectorAggregation{
				id:        "sum_of",
				Operation: types.VectorAggregationTypeSum,
				GroupBy:   groupBy,
			})

			_ = plan.addEdge(Edge{Parent: vectorAgg, Child: rangeAgg})
			_ = plan.addEdge(Edge{Parent: rangeAgg, Child: scan1})
		}

		// apply optimisation
		optimizations := []*optimization{
			newOptimization("group by pushdown", plan).withRules(
				&groupByPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			scan1 := expectedPlan.addNode(&DataObjScan{id: "scan1"})
			rangeAgg := expectedPlan.addNode(&RangeAggregation{
				id:          "count_over_time",
				Operation:   types.RangeAggregationTypeCount,
				PartitionBy: groupBy,
			})
			vectorAgg := expectedPlan.addNode(&VectorAggregation{
				id:        "sum_of",
				Operation: types.VectorAggregationTypeSum,
				GroupBy:   groupBy,
			})

			_ = expectedPlan.addEdge(Edge{Parent: vectorAgg, Child: rangeAgg})
			_ = expectedPlan.addEdge(Edge{Parent: rangeAgg, Child: scan1})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("projection pushdown", func(t *testing.T) {
		partitionBy := []ColumnExpression{
			&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
			&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
		}

		plan := &Plan{}
		{
			scan1 := plan.addNode(&DataObjScan{
				id: "scan1",
			})
			scan2 := plan.addNode(&DataObjScan{
				id: "scan2",
			})
			rangeAgg := plan.addNode(&RangeAggregation{
				id:          "range1",
				Operation:   types.RangeAggregationTypeCount,
				PartitionBy: partitionBy,
			})

			_ = plan.addEdge(Edge{Parent: rangeAgg, Child: scan1})
			_ = plan.addEdge(Edge{Parent: rangeAgg, Child: scan2})
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
			projected := append(partitionBy, &ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}})
			scan1 := expectedPlan.addNode(&DataObjScan{
				id:          "scan1",
				Projections: projected,
			})
			scan2 := expectedPlan.addNode(&DataObjScan{
				id:          "scan2",
				Projections: projected,
			})

			rangeAgg := expectedPlan.addNode(&RangeAggregation{
				id:          "range1",
				Operation:   types.RangeAggregationTypeCount,
				PartitionBy: partitionBy,
			})

			_ = expectedPlan.addEdge(Edge{Parent: rangeAgg, Child: scan1})
			_ = expectedPlan.addEdge(Edge{Parent: rangeAgg, Child: scan2})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("predicate column projection pushdown with existing projections", func(t *testing.T) {
		// Predicate columns should be projected when there are existing projections (metric query)
		partitionBy := []ColumnExpression{
			&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
		}

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
			scan1 := plan.addNode(&DataObjScan{id: "scan1"})
			scan2 := plan.addNode(&DataObjScan{id: "scan2"})
			filter := plan.addNode(&Filter{
				id:         "filter1",
				Predicates: filterPredicates,
			})
			rangeAgg := plan.addNode(&RangeAggregation{
				id:          "range1",
				Operation:   types.RangeAggregationTypeCount,
				PartitionBy: partitionBy,
			})

			_ = plan.addEdge(Edge{Parent: rangeAgg, Child: filter})
			_ = plan.addEdge(Edge{Parent: filter, Child: scan1})
			_ = plan.addEdge(Edge{Parent: filter, Child: scan2})
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
				&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
				&ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}},
				&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
				&ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
			}

			scan1 := expectedPlan.addNode(&DataObjScan{
				id:          "scan1",
				Projections: expectedProjections,
			})
			scan2 := expectedPlan.addNode(&DataObjScan{
				id:          "scan2",
				Projections: expectedProjections,
			})
			filter := expectedPlan.addNode(&Filter{
				id:         "filter1",
				Predicates: filterPredicates,
			})
			rangeAgg := expectedPlan.addNode(&RangeAggregation{
				id:          "range1",
				Operation:   types.RangeAggregationTypeCount,
				PartitionBy: partitionBy,
			})

			_ = expectedPlan.addEdge(Edge{Parent: rangeAgg, Child: filter})
			_ = expectedPlan.addEdge(Edge{Parent: filter, Child: scan1})
			_ = expectedPlan.addEdge(Edge{Parent: filter, Child: scan2})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("predicate column projection pushdown without existing projections", func(t *testing.T) {
		// Predicate columns should NOT be projected when there are no existing projections (log query)
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
			scan1 := plan.addNode(&DataObjScan{id: "scan1"})
			scan2 := plan.addNode(&DataObjScan{id: "scan2"})
			filter := plan.addNode(&Filter{
				id:         "filter1",
				Predicates: filterPredicates,
			})
			limit := plan.addNode(&Limit{id: "limit1", Fetch: 100})

			_ = plan.addEdge(Edge{Parent: limit, Child: filter})
			_ = plan.addEdge(Edge{Parent: filter, Child: scan1})
			_ = plan.addEdge(Edge{Parent: filter, Child: scan2})
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
			scan1 := expectedPlan.addNode(&DataObjScan{id: "scan1"})
			scan2 := expectedPlan.addNode(&DataObjScan{id: "scan2"})
			filter := expectedPlan.addNode(&Filter{
				id:         "filter1",
				Predicates: filterPredicates,
			})
			limit := expectedPlan.addNode(&Limit{id: "limit1", Fetch: 100})

			_ = expectedPlan.addEdge(Edge{Parent: limit, Child: filter})
			_ = expectedPlan.addEdge(Edge{Parent: filter, Child: scan1})
			_ = expectedPlan.addEdge(Edge{Parent: filter, Child: scan2})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})
}

func TestParseKeysPushdownRuleApplies(t *testing.T) {
	buildRule := func() *parseKeysPushdown {
		plan := &Plan{}
		rule := &parseKeysPushdown{plan: plan}
		return rule
	}
	t.Run("rule does not apply to non-ParseNode", func(t *testing.T) {
		rule := buildRule()

		// Create a Filter node (not a ParseNode)
		filterNode := &Filter{
			id: "filter1",
			Predicates: []Expression{
				&BinaryExpr{
					Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
					Right: NewLiteral("error"),
					Op:    types.BinaryOpEq,
				},
			},
		}

		// The rule should not apply to a Filter node
		applied := rule.apply(filterNode)
		require.False(t, applied, "rule should not apply to non-ParseNode")
	})

	t.Run("rule applies to Filter with ambiguous columns and ParseNode child", func(t *testing.T) {
		// Create a plan with Filter -> ParseNode -> DataObjScan
		plan := &Plan{}
		scan := plan.addNode(&DataObjScan{id: "scan1"})
		parseNode := &ParseNode{
			id:   "parse1",
			Kind: logical.ParserLogfmt,
		}
		plan.addNode(parseNode)
		filterNode := plan.addNode(&Filter{
			id: "filter1",
			Predicates: []Expression{
				&BinaryExpr{
					Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
					Right: NewLiteral("error"),
					Op:    types.BinaryOpEq,
				},
			},
		})
		
		// Connect nodes: Filter -> ParseNode -> DataObjScan
		_ = plan.addEdge(Edge{Parent: filterNode, Child: parseNode})
		_ = plan.addEdge(Edge{Parent: parseNode, Child: scan})

		rule := &parseKeysPushdown{plan: plan}
		
		// The rule should apply to the Filter node and push keys to ParseNode
		applied := rule.apply(filterNode)
		require.True(t, applied, "rule should apply to Filter with ambiguous columns")
		
		// Verify the ParseNode got the expected keys
		require.Equal(t, []string{"level"}, parseNode.RequestedKeys)
	})

	t.Run("rule applies to VectorAggregation with ambiguous GroupBy", func(t *testing.T) {
		// Create a plan with VectorAggregation -> ParseNode -> DataObjScan
		plan := &Plan{}
		scan := plan.addNode(&DataObjScan{id: "scan1"})
		parseNode := &ParseNode{
			id:   "parse1",
			Kind: logical.ParserLogfmt,
		}
		plan.addNode(parseNode)
		vecAgg := plan.addNode(&VectorAggregation{
			id:        "vecagg1",
			Operation: types.VectorAggregationTypeSum,
			GroupBy: []ColumnExpression{
				&ColumnExpr{Ref: types.ColumnRef{Column: "status", Type: types.ColumnTypeAmbiguous}},
				&ColumnExpr{Ref: types.ColumnRef{Column: "app", Type: types.ColumnTypeLabel}}, // Should be ignored
			},
		})
		
		// Connect nodes: VectorAggregation -> ParseNode -> DataObjScan
		_ = plan.addEdge(Edge{Parent: vecAgg, Child: parseNode})
		_ = plan.addEdge(Edge{Parent: parseNode, Child: scan})

		rule := &parseKeysPushdown{plan: plan}
		
		// The rule should apply to the VectorAggregation node
		applied := rule.apply(vecAgg)
		require.True(t, applied, "rule should apply to VectorAggregation with ambiguous columns")
		
		// Verify the ParseNode got only the ambiguous column
		require.Equal(t, []string{"status"}, parseNode.RequestedKeys)
	})

	t.Run("rule applies to RangeAggregation with ambiguous PartitionBy", func(t *testing.T) {
		// Create a plan with RangeAggregation -> ParseNode -> DataObjScan
		plan := &Plan{}
		scan := plan.addNode(&DataObjScan{id: "scan1"})
		parseNode := &ParseNode{
			id:   "parse1",
			Kind: logical.ParserLogfmt,
		}
		plan.addNode(parseNode)
		rangeAgg := plan.addNode(&RangeAggregation{
			id:        "rangeagg1",
			Operation: types.RangeAggregationTypeCount,
			PartitionBy: []ColumnExpression{
				&ColumnExpr{Ref: types.ColumnRef{Column: "duration", Type: types.ColumnTypeAmbiguous}},
				&ColumnExpr{Ref: types.ColumnRef{Column: "timestamp", Type: types.ColumnTypeBuiltin}}, // Should be ignored
			},
		})
		
		// Connect nodes: RangeAggregation -> ParseNode -> DataObjScan
		_ = plan.addEdge(Edge{Parent: rangeAgg, Child: parseNode})
		_ = plan.addEdge(Edge{Parent: parseNode, Child: scan})

		rule := &parseKeysPushdown{plan: plan}
		
		// The rule should apply to the RangeAggregation node
		applied := rule.apply(rangeAgg)
		require.True(t, applied, "rule should apply to RangeAggregation with ambiguous columns")
		
		// Verify the ParseNode got only the ambiguous column
		require.Equal(t, []string{"duration"}, parseNode.RequestedKeys)
	})
}


func TestParseKeysPushdown(t *testing.T) {
	tests := []struct {
		name         string
		buildLogical func() logical.Value
		expectedKeys []string
	}{
		{
			name: "ParseNode remains empty when no operations need parsed fields",
			buildLogical: func() logical.Value {
				// Create a simple log query with no filters that need parsed fields
				// {app="test"} | logfmt
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})
				
				// Add parse but no filters requiring parsed fields
				builder = builder.Parse(logical.ParserLogfmt)
				return builder.Value()
			},
			expectedKeys: nil, // No keys needed
		},
		{
			name: "ParseNode skips label and builtin columns, only collects ambiguous",
			buildLogical: func() logical.Value {
				// {app="test"} | logfmt | app="frontend" | level="error"
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
			expectedKeys: []string{"level"}, // Only ambiguous column, not the label
		},
		{
			name: "RangeAggregation with PartitionBy on ambiguous columns",
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
					5 * time.Minute,
					5 * time.Minute,
				)
				
				return builder.Value()
			},
			expectedKeys: []string{"duration"}, // Only ambiguous column from PartitionBy
		},
		{
			name: "log query with logfmt and filter on ambiguous column",
			buildLogical: func() logical.Value {
				// Create a logical plan that represents:
				// {app="test"} | logfmt | level="error"
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
			expectedKeys: []string{"level"},
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
					5 * time.Minute, // step
					5 * time.Minute, // range interval
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
			expectedKeys: []string{"status"},
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
					5 * time.Minute, // step
					5 * time.Minute, // range interval
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
			expectedKeys: []string{"code", "duration", "status"}, // sorted alphabetically
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
			catalog := &catalog{
				streamsByObject: map[string]objectMeta{
					"/test/object": {streamIDs: []int64{1, 2}, sections: 10},
				},
			}
			ctx := NewContext(time.Unix(0, 0), time.Unix(3600, 0))
			planner := NewPlanner(ctx, catalog)
			
			// Build physical plan
			physicalPlan, err := planner.Build(logicalPlan)
			require.NoError(t, err)
			
			// Optimize the plan - this should apply parseKeysPushdown
			optimizedPlan, err := planner.Optimize(physicalPlan)
			require.NoError(t, err)
			
			// Check that ParseNode has the expected keys
			var parseNode *ParseNode
			for node := range optimizedPlan.nodes {
				if pn, ok := node.(*ParseNode); ok {
					parseNode = pn
					break
				}
			}
			require.NotNil(t, parseNode, "ParseNode not found in plan")
			require.Equal(t, tt.expectedKeys, parseNode.RequestedKeys)
		})
	}
}

