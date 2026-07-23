package physical

import (
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/logql/log"
)

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
		optimizations := []*Optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := NewOptimizer(plan, optimizations)
		o.Optimize(plan.Roots()[0])

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

	t.Run("range aggregation empty by() grouping -> scanset", func(t *testing.T) {
		grouping := Grouping{
			Columns: []ColumnExpression{},
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
		optimizations := []*Optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := NewOptimizer(plan, optimizations)
		firings := o.Optimize(plan.Roots()[0])
		require.True(t, firings["projection pushdown"])

		expectedPlan := &Plan{}
		{
			projected := []ColumnExpression{&ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}}}
			scanset := expectedPlan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
				Projections: projected, // Only Timestamp is required by the RangeAggregation.
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

	t.Run("range aggregation empty without() grouping -> scanset", func(t *testing.T) {
		grouping := Grouping{
			Columns: []ColumnExpression{},
			Without: true,
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
		optimizations := []*Optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := NewOptimizer(plan, optimizations)
		firings := o.Optimize(plan.Roots()[0])
		require.False(t, firings["projection pushdown"])

		expectedPlan := &Plan{}
		{
			scanset := expectedPlan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
				// Nothing is projected because the RangeAggregation requires all columns.
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
		optimizations := []*Optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := NewOptimizer(plan, optimizations)
		o.Optimize(plan.Roots()[0])

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
		optimizations := []*Optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := NewOptimizer(plan, optimizations)
		o.Optimize(plan.Roots()[0])

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
		// expectedDataObjScanProjectionRefs, when non-nil, additionally
		// asserts the scan's projections as (name, type) pairs. Use this
		// when the case is sensitive to a column's type (e.g. a parsed
		// field name colliding with a builtin column of the same name).
		// Order is not asserted; the runner sorts before comparing.
		expectedDataObjScanProjectionRefs []types.ColumnRef
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
			name: "RangeAggregation with GroupBy on regexp named-capture output",
			buildLogical: func() logical.Value {
				// count_over_time({app="test"} | regexp "(?P<referrer>[^ ]+)" [5m]) by (referrer)
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})
				builder = builder.ParseRegexp("(?P<referrer>[^ ]+)")
				builder = builder.RangeAggregation(
					logical.Grouping{
						Columns: []logical.ColumnRef{
							{Ref: types.ColumnRef{Column: "referrer", Type: types.ColumnTypeAmbiguous}},
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
			// Regexp has no requestedKeys; we only assert the scan projections.
			expectedParseKeysRequested:     nil,
			expectedDataObjScanProjections: []string{"message", "referrer", "timestamp"},
		},
		{
			name: "Filter on regexp named-capture output",
			buildLogical: func() logical.Value {
				// sum by (app) (count_over_time({app="test"} | regexp "(?P<lvl>[^ ]+)" | lvl="error" [5m]))
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})
				builder = builder.ParseRegexp("(?P<lvl>[^ ]+)")
				builder = builder.Select(&logical.BinOp{
					Left:  logical.NewColumnRef("lvl", types.ColumnTypeAmbiguous),
					Right: logical.NewLiteral("error"),
					Op:    types.BinaryOpEq,
				})
				builder = builder.RangeAggregation(
					logical.NoGrouping,
					types.RangeAggregationTypeCount,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute,
					5*time.Minute,
				)
				builder = builder.VectorAggregation(
					logical.Grouping{
						Columns: []logical.ColumnRef{
							{Ref: types.ColumnRef{Column: "app", Type: types.ColumnTypeLabel}},
						},
						Without: false,
					},
					types.VectorAggregationTypeSum,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute,
				)
				return builder.Value()
			},
			expectedParseKeysRequested:     nil,
			expectedDataObjScanProjections: []string{"app", "lvl", "message", "timestamp"},
		},
		{
			name: "Filter on parsed field that collides with builtin (message)",
			buildLogical: func() logical.Value {
				// sum by (app) (count_over_time({app="test"} | json | message="hello" [5m]))
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})
				builder = builder.Parse(types.VariadicOpParseJSON, false, false)
				builder = builder.Select(&logical.BinOp{
					Left:  logical.NewColumnRef("message", types.ColumnTypeAmbiguous),
					Right: logical.NewLiteral("hello"),
					Op:    types.BinaryOpEq,
				})
				builder = builder.RangeAggregation(
					logical.NoGrouping,
					types.RangeAggregationTypeCount,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute,
					5*time.Minute,
				)
				builder = builder.VectorAggregation(
					logical.Grouping{
						Columns: []logical.ColumnRef{
							{Ref: types.ColumnRef{Column: "app", Type: types.ColumnTypeLabel}},
						},
						Without: false,
					},
					types.VectorAggregationTypeSum,
				)
				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"message"},
			expectedDataObjScanProjections: []string{"app", "message", "timestamp"},
			expectedDataObjScanProjectionRefs: []types.ColumnRef{
				{Column: "app", Type: types.ColumnTypeLabel},
				// Critical: the [Builtin] `message` must survive so the
				// scan loads the raw log line for the JSON parser to
				// consume. Pre-fix (name-only dedup) this entry was
				// silently dropped in favour of the earlier-added
				// [Ambiguous] entry.
				{Column: "message", Type: types.ColumnTypeBuiltin},
				// The [Ambiguous] `message` (from the [Filter]) also
				// survives dedup since it has a different type; the
				// scan silently ignores it (no metadata column of that
				// name in this data) so it costs nothing.
				{Column: "message", Type: types.ColumnTypeAmbiguous},
				{Column: "timestamp", Type: types.ColumnTypeBuiltin},
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
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute,
				)
				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"code", "duration", "status"}, // sorted alphabetically
			expectedDataObjScanProjections: []string{"code", "duration", "message", "status", "timestamp"},
		},
		{
			// Regression test for the collision-renamed "_extracted" bug: a parsed
			// field that collides with a stream label is referenced downstream
			// as "namespace_extracted" (e.g. json "namespace" vs the "namespace" stream label).
			// The parser only sees real json keys, so we must
			// also request the de-suffixed source key "namespace"; otherwise nothing
			// is extracted and group-by on the "_extracted" name silently drops to
			// null (returning 0 in v2 while v1 returns the correct value).
			name: "GroupBy on collision-renamed _extracted label requests the source key",
			buildLogical: func() logical.Value {
				// count_over_time({app="test"} | json [5m]) by (namespace_extracted)
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})
				builder = builder.Parse(types.VariadicOpParseJSON, false, false)
				builder = builder.RangeAggregation(
					logical.Grouping{
						Columns: []logical.ColumnRef{
							{Ref: types.ColumnRef{Column: "namespace_extracted", Type: types.ColumnTypeAmbiguous}},
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
			expectedParseKeysRequested: []string{"namespace", "namespace_extracted"},
			// "namespace" is also projected so the colliding label/metadata is loaded
			// and ColumnCompat can rename the parsed value to "namespace_extracted".
			expectedDataObjScanProjections: []string{"message", "namespace", "namespace_extracted", "timestamp"},
		},
		{
			// Same fix from the label-filter side, combined with a clean group-by
			// column: the "_extracted" filter column also contributes its source key,
			// while the clean group-by column is left untouched.
			name: "Filter on _extracted label plus clean GroupBy requests both source and clean keys",
			buildLogical: func() logical.Value {
				// sum by (component) (count_over_time({app="test"} | json | name_extracted="x" [5m]))
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})
				builder = builder.Parse(types.VariadicOpParseJSON, false, false)
				builder = builder.Select(&logical.BinOp{
					Left:  logical.NewColumnRef("name_extracted", types.ColumnTypeAmbiguous),
					Right: logical.NewLiteral("x"),
					Op:    types.BinaryOpEq,
				})
				builder = builder.RangeAggregation(
					logical.NoGrouping,
					types.RangeAggregationTypeCount,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute,
					5*time.Minute,
				)
				builder = builder.VectorAggregation(
					logical.Grouping{
						Columns: []logical.ColumnRef{
							{Ref: types.ColumnRef{Column: "component", Type: types.ColumnTypeAmbiguous}},
						},
						Without: false,
					},
					types.VectorAggregationTypeSum,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute,
				)
				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"component", "name", "name_extracted"},
			expectedDataObjScanProjections: []string{"component", "message", "name", "name_extracted", "timestamp"},
		},
		{
			// Edge case: a column literally named "_extracted" must not produce an
			// empty source key (CutSuffix yields ""), which would request every key.
			name: "GroupBy on bare _extracted does not request an empty key",
			buildLogical: func() logical.Value {
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})
				builder = builder.Parse(types.VariadicOpParseJSON, false, false)
				builder = builder.RangeAggregation(
					logical.Grouping{
						Columns: []logical.ColumnRef{
							{Ref: types.ColumnRef{Column: "_extracted", Type: types.ColumnTypeAmbiguous}},
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
			expectedParseKeysRequested:     []string{"_extracted"},
			expectedDataObjScanProjections: []string{"_extracted", "message", "timestamp"},
		},
		{
			// Edge case: a doubly-suffixed name (nested collision) strips exactly one
			// level, requesting "foo_extracted" as the source.
			name: "GroupBy on double-suffixed _extracted strips only one level",
			buildLogical: func() logical.Value {
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})
				builder = builder.Parse(types.VariadicOpParseJSON, false, false)
				builder = builder.RangeAggregation(
					logical.Grouping{
						Columns: []logical.ColumnRef{
							{Ref: types.ColumnRef{Column: "foo_extracted_extracted", Type: types.ColumnTypeAmbiguous}},
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
			expectedParseKeysRequested:     []string{"foo_extracted", "foo_extracted_extracted"},
			expectedDataObjScanProjections: []string{"foo_extracted", "foo_extracted_extracted", "message", "timestamp"},
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
		{
			name: "line_format template fields are added to upstream logfmt requestedKeys",
			buildLogical: func() logical.Value {
				// sum by (app) (sum_over_time({app="test"} | logfmt | line_format "{{.mint}}" | regexp "(?P<val>\d+)" | unwrap val [5m]))
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})
				builder = builder.Parse(types.VariadicOpParseLogfmt, false, false)
				builder = builder.Format(types.VariadicOpParseLinefmt, logical.NewLiteral("{{.mint}}"))
				builder = builder.ParseRegexp(`(?P<val>\d+)`)
				builder = builder.Cast("val", types.UnaryOpCastFloat)
				builder = builder.ProjectDrop(&logical.ColumnRef{
					Ref: types.ColumnRef{Column: "val", Type: types.ColumnTypeAmbiguous},
				})
				builder = builder.RangeAggregation(
					logical.NoGrouping,
					types.RangeAggregationTypeSum,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute,
					5*time.Minute,
				)
				builder = builder.VectorAggregation(
					logical.Grouping{
						Columns: []logical.ColumnRef{
							{Ref: types.ColumnRef{Column: "app", Type: types.ColumnTypeLabel}},
						},
						Without: false,
					},
					types.VectorAggregationTypeSum,
				)
				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"mint", "val"},
			expectedDataObjScanProjections: []string{"app", "message", "mint", "timestamp", "val"},
		},
		{
			name: "label_format rename source is added to upstream logfmt requestedKeys",
			buildLogical: func() logical.Value {
				// sum by (msg_new) (count_over_time({app="test"} | logfmt | label_format msg_new=msg [5m]))
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})
				builder = builder.Parse(types.VariadicOpParseLogfmt, false, false)
				builder = builder.Format(types.VariadicOpParseLabelfmt, logical.NewLiteral([]log.LabelFmt{
					log.NewRenameLabelFmt("msg_new", "msg"),
				}))
				builder = builder.RangeAggregation(
					logical.NoGrouping,
					types.RangeAggregationTypeCount,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute,
					5*time.Minute,
				)
				builder = builder.VectorAggregation(
					logical.Grouping{
						Columns: []logical.ColumnRef{
							{Ref: types.ColumnRef{Column: "msg_new", Type: types.ColumnTypeAmbiguous}},
						},
						Without: false,
					},
					types.VectorAggregationTypeSum,
				)
				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"msg", "msg_new"},
			expectedDataObjScanProjections: []string{"message", "msg", "msg_new", "timestamp"},
		},
		{
			name: "label_format template fields are added to upstream logfmt requestedKeys",
			buildLogical: func() logical.Value {
				// sum by (tag) (count_over_time({app="test"} | logfmt | label_format tag=`x{{.error}}y` [5m]))
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})
				builder = builder.Parse(types.VariadicOpParseLogfmt, false, false)
				builder = builder.Format(types.VariadicOpParseLabelfmt, logical.NewLiteral([]log.LabelFmt{
					log.NewTemplateLabelFmt("tag", "x{{.error}}y"),
				}))
				builder = builder.RangeAggregation(
					logical.NoGrouping,
					types.RangeAggregationTypeCount,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute,
					5*time.Minute,
				)
				builder = builder.VectorAggregation(
					logical.Grouping{
						Columns: []logical.ColumnRef{
							{Ref: types.ColumnRef{Column: "tag", Type: types.ColumnTypeAmbiguous}},
						},
						Without: false,
					},
					types.VectorAggregationTypeSum,
				)
				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"error", "tag"},
			expectedDataObjScanProjections: []string{"error", "message", "tag", "timestamp"},
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

			// Collect ALL Projection nodes rather than the last one iterated
			// (map iteration order is non-deterministic), and find the one
			// carrying the parse (JSON / logfmt) VariadicExpr — that's the
			// node whose requestedKeys we assert on. A pipeline can have
			// several Projection nodes (parser + line_format + regexp +
			// cast + drop each become one) so picking the last-iterated
			// makes the assertion flaky when more than one is present.
			var projectionNodes []*Projection
			projections := map[string]struct{}{}
			projectionRefs := map[types.ColumnRef]struct{}{}
			for node := range optimizedPlan.graph.Nodes() {
				if pn, ok := node.(*Projection); ok {
					projectionNodes = append(projectionNodes, pn)
					continue
				}
				if pn, ok := node.(*ScanSet); ok {
					for _, colExpr := range pn.Projections {
						expr := colExpr.(*ColumnExpr)
						projections[expr.Ref.Column] = struct{}{}
						projectionRefs[expr.Ref] = struct{}{}
					}
				}
			}

			require.NotEmpty(t, projectionNodes, "Projection not found in plan")
			var requestedKeys *LiteralExpr
			for _, projectionNode := range projectionNodes {
				for _, expr := range projectionNode.Expressions {
					ve, ok := expr.(*VariadicExpr)
					if !ok {
						continue
					}
					// Only JSON / logfmt carry requestedKeys at arg index 1. The regexp
					// parser's arg index 1 is the pattern literal (a StringLiteral),
					// not a requested-keys list — interpreting it as requestedKeys
					// would falsely fail the literal-type assertion below.
					if ve.Op != types.VariadicOpParseJSON && ve.Op != types.VariadicOpParseLogfmt {
						continue
					}
					if len(ve.Expressions) >= 2 {
						if e, ok := ve.Expressions[1].(*LiteralExpr); ok {
							requestedKeys = e
						}
					}
				}
				if requestedKeys != nil {
					break
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

			// Optional stricter assertion: also verify each projection's
			// [types.ColumnType]. This catches "same name kept, wrong
			// type" regressions that the name-only slice above cannot
			// distinguish — e.g. a parsed field colliding with a builtin.
			if tt.expectedDataObjScanProjectionRefs != nil {
				gotRefs := make([]types.ColumnRef, 0, len(projectionRefs))
				for r := range projectionRefs {
					gotRefs = append(gotRefs, r)
				}
				sort.Slice(gotRefs, func(i, j int) bool {
					if gotRefs[i].Column != gotRefs[j].Column {
						return gotRefs[i].Column < gotRefs[j].Column
					}
					return gotRefs[i].Type < gotRefs[j].Type
				})
				wantRefs := slices.Clone(tt.expectedDataObjScanProjectionRefs)
				sort.Slice(wantRefs, func(i, j int) bool {
					if wantRefs[i].Column != wantRefs[j].Column {
						return wantRefs[i].Column < wantRefs[j].Column
					}
					return wantRefs[i].Type < wantRefs[j].Type
				})
				require.Equal(t, wantRefs, gotRefs)
			}
		})
	}
}

func TestAddUniqueColumnExpr(t *testing.T) {
	col := func(name string, typ types.ColumnType) *ColumnExpr {
		return &ColumnExpr{Ref: types.ColumnRef{Column: name, Type: typ}}
	}

	refs := func(projections []ColumnExpression) []types.ColumnRef {
		var out []types.ColumnRef
		for _, p := range projections {
			if pe, ok := p.(*ColumnExpr); ok {
				out = append(out, pe.Ref)
			}
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Column != out[j].Column {
				return out[i].Column < out[j].Column
			}
			return out[i].Type < out[j].Type
		})
		return out
	}

	tests := []struct {
		name     string
		start    []ColumnExpression
		add      []*ColumnExpr
		expected []types.ColumnRef
	}{
		{
			name:  "Ambiguous added first absorbs Label added second",
			start: nil,
			add: []*ColumnExpr{
				col("level", types.ColumnTypeAmbiguous),
				col("level", types.ColumnTypeLabel),
			},
			expected: []types.ColumnRef{
				{Column: "level", Type: types.ColumnTypeAmbiguous},
			},
		},
		{
			name:  "Ambiguous added last absorbs Label already present",
			start: nil,
			add: []*ColumnExpr{
				col("level", types.ColumnTypeLabel),
				col("level", types.ColumnTypeAmbiguous),
			},
			expected: []types.ColumnRef{
				{Column: "level", Type: types.ColumnTypeAmbiguous},
			},
		},
		{
			name:  "Ambiguous added first absorbs Metadata added second",
			start: nil,
			add: []*ColumnExpr{
				col("trace_id", types.ColumnTypeAmbiguous),
				col("trace_id", types.ColumnTypeMetadata),
			},
			expected: []types.ColumnRef{
				{Column: "trace_id", Type: types.ColumnTypeAmbiguous},
			},
		},
		{
			name:  "Ambiguous added last absorbs Metadata already present",
			start: nil,
			add: []*ColumnExpr{
				col("trace_id", types.ColumnTypeMetadata),
				col("trace_id", types.ColumnTypeAmbiguous),
			},
			expected: []types.ColumnRef{
				{Column: "trace_id", Type: types.ColumnTypeAmbiguous},
			},
		},
		{
			name:  "Ambiguous absorbs both Label and Metadata with same name at once",
			start: nil,
			add: []*ColumnExpr{
				col("foo", types.ColumnTypeLabel),
				col("foo", types.ColumnTypeMetadata),
				col("foo", types.ColumnTypeAmbiguous),
			},
			expected: []types.ColumnRef{
				{Column: "foo", Type: types.ColumnTypeAmbiguous},
			},
		},
		{
			name:  "Builtin coexists with Ambiguous of same name (PR #22907 case)",
			start: nil,
			add: []*ColumnExpr{
				col("message", types.ColumnTypeBuiltin),
				col("message", types.ColumnTypeAmbiguous),
			},
			expected: []types.ColumnRef{
				// Sorted by type ordinal: Builtin (1) < Ambiguous (5).
				{Column: "message", Type: types.ColumnTypeBuiltin},
				{Column: "message", Type: types.ColumnTypeAmbiguous},
			},
		},
		{
			name:  "Label and Metadata with same name coexist (different storage sections)",
			start: nil,
			add: []*ColumnExpr{
				col("status", types.ColumnTypeLabel),
				col("status", types.ColumnTypeMetadata),
			},
			expected: []types.ColumnRef{
				{Column: "status", Type: types.ColumnTypeLabel},
				{Column: "status", Type: types.ColumnTypeMetadata},
			},
		},
		{
			name:  "Exact duplicate (name, type) is skipped",
			start: nil,
			add: []*ColumnExpr{
				col("level", types.ColumnTypeLabel),
				col("level", types.ColumnTypeLabel),
			},
			expected: []types.ColumnRef{
				{Column: "level", Type: types.ColumnTypeLabel},
			},
		},
		{
			name:  "Different names never absorb each other",
			start: nil,
			add: []*ColumnExpr{
				col("level", types.ColumnTypeAmbiguous),
				col("service", types.ColumnTypeLabel),
			},
			expected: []types.ColumnRef{
				{Column: "level", Type: types.ColumnTypeAmbiguous},
				{Column: "service", Type: types.ColumnTypeLabel},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projections := tt.start
			for _, c := range tt.add {
				projections, _ = addUniqueColumnExpr(projections, c)
			}
			require.Equal(t, tt.expected, refs(projections))
		})
	}
}
