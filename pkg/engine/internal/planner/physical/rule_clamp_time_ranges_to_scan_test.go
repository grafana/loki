package physical

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestScanTimeRangePushup(t *testing.T) {
	t.Run("pushup time range to RangeAggregation with 1 second step", func(t *testing.T) {
		plan := &Plan{}
		{
			dataObjScan := plan.graph.Add(&DataObjScan{MaxTimeRange: TimeRange{Start: time.Date(2026, 3, 14, 16, 45, 29, 0, time.UTC),
				End: time.Date(2026, 3, 14, 16, 46, 31, 0, time.UTC)}})
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Start: time.Date(2026, 3, 14, 16, 53, 30, 0, time.UTC),
				End:   time.Date(2026, 3, 14, 16, 53, 35, 0, time.UTC),
				Step:  1 * time.Second, Range: 10 * time.Minute})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: dataObjScan})
		}

		// apply optimisations
		optimizations := []*Optimization{
			newOptimization("scan time range pushup", plan).withRules(
				&clampTimeRangesToScan{plan: plan},
			),
		}
		o := NewOptimizer(plan, optimizations)
		o.Optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			dataObjScan := expectedPlan.graph.Add(&DataObjScan{MaxTimeRange: TimeRange{Start: time.Date(2026, 3, 14, 16, 45, 29, 0, time.UTC),
				End: time.Date(2026, 3, 14, 16, 46, 31, 0, time.UTC)}})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{
				Start: time.Date(2026, 3, 14, 16, 53, 30, 0, time.UTC),
				End:   time.Date(2026, 3, 14, 16, 53, 35, 0, time.UTC),
				Step:  1 * time.Second, Range: 10 * time.Minute})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: dataObjScan})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})
	t.Run("non-zero step rounds scan time range in RangeAggregation", func(t *testing.T) {
		plan := &Plan{}
		{
			dataObjScan := plan.graph.Add(&DataObjScan{MaxTimeRange: TimeRange{Start: time.Date(2026, 3, 14, 16, 44, 29, 0, time.UTC),
				End: time.Date(2026, 3, 14, 16, 46, 31, 0, time.UTC)}})
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Start: time.Date(2026, 3, 14, 16, 43, 30, 0, time.UTC),
				End:   time.Date(2026, 3, 14, 16, 53, 30, 0, time.UTC),
				Step:  time.Minute, Range: time.Minute})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: dataObjScan})
		}

		// apply optimisations
		optimizations := []*Optimization{
			newOptimization("scan time range pushup", plan).withRules(
				&clampTimeRangesToScan{plan: plan},
			),
		}
		o := NewOptimizer(plan, optimizations)
		o.Optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			dataObjScan := expectedPlan.graph.Add(&DataObjScan{MaxTimeRange: TimeRange{Start: time.Date(2026, 3, 14, 16, 44, 29, 0, time.UTC),
				End: time.Date(2026, 3, 14, 16, 46, 31, 0, time.UTC)}})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{
				Start: time.Date(2026, 3, 14, 16, 44, 0, 0, time.UTC),
				End:   time.Date(2026, 3, 14, 16, 48, 00, 0, time.UTC),
				Step:  time.Minute, Range: time.Minute})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: dataObjScan})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})
	t.Run("unusual step still rounds scan time range in RangeAggregation", func(t *testing.T) {
		plan := &Plan{}
		{
			dataObjScan := plan.graph.Add(&DataObjScan{MaxTimeRange: TimeRange{Start: time.Date(2026, 3, 20, 17, 53, 41, 0, time.UTC),
				End: time.Date(2026, 3, 20, 17, 54, 49, 0, time.UTC)}})
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Start: time.Date(2026, 3, 20, 16, 43, 30, 0, time.UTC),
				End:   time.Date(2026, 3, 20, 18, 53, 30, 0, time.UTC),
				Step:  63 * time.Second, Range: 0})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: dataObjScan})
		}

		// apply optimisations
		optimizations := []*Optimization{
			newOptimization("scan time range pushup", plan).withRules(
				&clampTimeRangesToScan{plan: plan},
			),
		}
		o := NewOptimizer(plan, optimizations)
		o.Optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			dataObjScan := expectedPlan.graph.Add(&DataObjScan{MaxTimeRange: TimeRange{Start: time.Date(2026, 3, 20, 17, 53, 41, 0, time.UTC),
				End: time.Date(2026, 3, 20, 17, 54, 49, 0, time.UTC)}})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{
				Start: time.Date(2026, 3, 20, 17, 52, 39, 0, time.UTC),
				End:   time.Date(2026, 3, 20, 17, 55, 48, 0, time.UTC),
				Step:  63 * time.Second, Range: 0})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: dataObjScan})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})
	t.Run("step of zero means no rounding of scan time range in RangeAggregation", func(t *testing.T) {
		plan := &Plan{}
		{
			dataObjScan := plan.graph.Add(&DataObjScan{MaxTimeRange: TimeRange{Start: time.Date(2026, 3, 14, 16, 45, 29, 0, time.UTC),
				End: time.Date(2026, 3, 14, 16, 46, 31, 0, time.UTC)}})
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Start: time.Date(2026, 3, 14, 16, 43, 30, 0, time.UTC),
				End:   time.Date(2026, 3, 14, 16, 43, 30, 0, time.UTC),
				Step:  0, Range: time.Minute})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: dataObjScan})
		}

		// apply optimisations
		optimizations := []*Optimization{
			newOptimization("scan time range pushup", plan).withRules(
				&clampTimeRangesToScan{plan: plan},
			),
		}
		o := NewOptimizer(plan, optimizations)
		o.Optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			dataObjScan := expectedPlan.graph.Add(&DataObjScan{MaxTimeRange: TimeRange{Start: time.Date(2026, 3, 14, 16, 45, 29, 0, time.UTC),
				End: time.Date(2026, 3, 14, 16, 46, 31, 0, time.UTC)}})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{
				Start: time.Date(2026, 3, 14, 16, 43, 30, 0, time.UTC),
				End:   time.Date(2026, 3, 14, 16, 43, 30, 0, time.UTC),
				Step:  0, Range: time.Minute})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: dataObjScan})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})
	t.Run("non-zero range means checking datascan end + range", func(t *testing.T) {
		plan := &Plan{}
		{
			dataObjScan := plan.graph.Add(&DataObjScan{MaxTimeRange: TimeRange{Start: time.Date(2026, 3, 20, 17, 53, 41, 0, time.UTC),
				End: time.Date(2026, 3, 20, 17, 54, 49, 0, time.UTC)}})
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Start: time.Date(2026, 3, 20, 16, 43, 30, 0, time.UTC),
				End:   time.Date(2026, 3, 20, 18, 53, 30, 0, time.UTC),
				Step:  63 * time.Second, Range: 1 * time.Minute})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: dataObjScan})
		}

		// apply optimisations
		optimizations := []*Optimization{
			newOptimization("scan time range pushup", plan).withRules(
				&clampTimeRangesToScan{plan: plan},
			),
		}
		o := NewOptimizer(plan, optimizations)
		o.Optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			dataObjScan := expectedPlan.graph.Add(&DataObjScan{MaxTimeRange: TimeRange{Start: time.Date(2026, 3, 20, 17, 53, 41, 0, time.UTC),
				End: time.Date(2026, 3, 20, 17, 54, 49, 0, time.UTC)}})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{
				Start: time.Date(2026, 3, 20, 17, 52, 39, 0, time.UTC),
				End:   time.Date(2026, 3, 20, 17, 56, 51, 0, time.UTC),
				Step:  63 * time.Second, Range: 1 * time.Minute})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: dataObjScan})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})
}

func TestClampTimeRangesToScan(t *testing.T) {
	t.Run("DataObjScan limit applies to parent Filter node", func(t *testing.T) {
		start := time.Date(2026, 3, 11, 12, 41, 44, 0, time.UTC)
		end := time.Date(2026, 3, 11, 12, 41, 46, 0, time.UTC)
		tr := TimeRange{Start: start, End: end}
		dataObjScanPredTime := time.Date(2026, 3, 11, 12, 41, 40, 0, time.UTC).UnixNano()
		filterPredTime1 := time.Date(2026, 3, 11, 12, 41, 42, 0, time.UTC).UnixNano()
		filterPredTime2 := time.Date(2026, 3, 11, 12, 41, 50, 0, time.UTC).UnixNano()
		plan := &Plan{}
		{
			dataObjScan := plan.graph.Add(&DataObjScan{MaxTimeRange: tr,
				Predicates: []Expression{&BinaryExpr{Op: types.BinaryOpGte,
					Left:  newColumnExpr(types.ColumnNameBuiltinTimestamp, types.ColumnTypeBuiltin),
					Right: NewLiteral(types.Timestamp(dataObjScanPredTime))}}})
			filter := plan.graph.Add(&Filter{Predicates: []Expression{&BinaryExpr{Op: types.BinaryOpGt,
				Left:  newColumnExpr(types.ColumnNameBuiltinTimestamp, types.ColumnTypeBuiltin),
				Right: NewLiteral(types.Timestamp(filterPredTime1))},
				&BinaryExpr{Op: types.BinaryOpLt,
					Left:  newColumnExpr(types.ColumnNameBuiltinTimestamp, types.ColumnTypeBuiltin),
					Right: NewLiteral(types.Timestamp(filterPredTime2))},
			}})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: dataObjScan})
		}

		// apply optimisations
		optimizations := []*Optimization{
			newOptimization("clamp time ranges to scan", plan).withRules(
				&clampTimeRangesToScan{plan: plan},
			),
		}
		o := NewOptimizer(plan, optimizations)
		o.Optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			dataObjScan := expectedPlan.graph.Add(&DataObjScan{MaxTimeRange: tr,
				Predicates: []Expression{&BinaryExpr{Op: types.BinaryOpGte,
					Left:  newColumnExpr(types.ColumnNameBuiltinTimestamp, types.ColumnTypeBuiltin),
					Right: NewLiteral(types.Timestamp(start.UnixNano()))}},
			})
			filter := expectedPlan.graph.Add(&Filter{Predicates: []Expression{&BinaryExpr{Op: types.BinaryOpGte,
				Left:  newColumnExpr(types.ColumnNameBuiltinTimestamp, types.ColumnTypeBuiltin),
				Right: NewLiteral(types.Timestamp(start.UnixNano()))},
				&BinaryExpr{Op: types.BinaryOpLte,
					Left:  newColumnExpr(types.ColumnNameBuiltinTimestamp, types.ColumnTypeBuiltin),
					Right: NewLiteral(types.Timestamp(end.UnixNano()))},
			}})

			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: dataObjScan})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

}

func TestClampTimeRangesToScan_ClampExpression(t *testing.T) {
	col := newColumnExpr(types.ColumnNameBuiltinTimestamp, types.ColumnTypeBuiltin)
	early := types.Timestamp(time.Date(2026, 3, 11, 12, 41, 44, 719000000, time.UTC).UnixNano())
	late := types.Timestamp(time.Date(2026, 3, 18, 9, 41, 44, 976217699, time.UTC).UnixNano())
	tests := []struct {
		desc    string
		e       Expression
		tr      TimeRange
		clamped bool
		want    string
	}{
		{
			desc: "GTE before range clamps to start",
			e:    &BinaryExpr{Left: col, Right: NewLiteral(early), Op: types.BinaryOpGte},
			tr: TimeRange{
				Start: time.Date(2026, 3, 14, 16, 43, 30, 0, time.UTC),
				End:   time.Date(2026, 3, 14, 16, 48, 0, 0, time.UTC),
			},
			clamped: true,
			want:    fmt.Sprintf("GTE(%s, %s)", col, util.FormatTimeRFC3339Nano(time.Date(2026, 3, 14, 16, 43, 30, 0, time.UTC))),
		},
		{
			desc: "GT before range clamps to start",
			e:    &BinaryExpr{Left: col, Right: NewLiteral(early), Op: types.BinaryOpGt},
			tr: TimeRange{
				Start: time.Date(2026, 3, 14, 16, 43, 30, 0, time.UTC),
				End:   time.Date(2026, 3, 14, 16, 48, 0, 0, time.UTC),
			},
			clamped: true,
			want:    fmt.Sprintf("GTE(%s, %s)", col, util.FormatTimeRFC3339Nano(time.Date(2026, 3, 14, 16, 43, 30, 0, time.UTC))),
		},
		{
			desc: "LT after range clamps to end",
			e:    &BinaryExpr{Left: col, Right: NewLiteral(late), Op: types.BinaryOpLt},
			tr: TimeRange{
				Start: time.Date(2026, 3, 14, 16, 43, 30, 0, time.UTC),
				End:   time.Date(2026, 3, 14, 16, 48, 0, 0, time.UTC),
			},
			clamped: true,
			want:    fmt.Sprintf("LTE(%s, %s)", col, util.FormatTimeRFC3339Nano(time.Date(2026, 3, 14, 16, 48, 0, 0, time.UTC))),
		},
		{
			desc: "LTE after range clamps to end",
			e:    &BinaryExpr{Left: col, Right: NewLiteral(late), Op: types.BinaryOpLte},
			tr: TimeRange{
				Start: time.Date(2026, 3, 14, 16, 43, 30, 0, time.UTC),
				End:   time.Date(2026, 3, 14, 16, 48, 0, 0, time.UTC),
			},
			clamped: true,
			want:    fmt.Sprintf("LTE(%s, %s)", col, util.FormatTimeRFC3339Nano(time.Date(2026, 3, 14, 16, 48, 0, 0, time.UTC))),
		},
		{
			desc:    "zero TimeRange leaves expression unchanged",
			e:       &BinaryExpr{Left: col, Right: NewLiteral(early), Op: types.BinaryOpGte},
			tr:      TimeRange{},
			clamped: false,
			want:    (&BinaryExpr{Left: col, Right: NewLiteral(early), Op: types.BinaryOpGte}).String(),
		},
		{
			desc: "non-timestamp binary expr unchange",
			e: &BinaryExpr{
				Left:  newColumnExpr("level", types.ColumnTypeLabel),
				Right: NewLiteral("info"),
				Op:    types.BinaryOpEq,
			},
			tr: TimeRange{
				Start: time.Date(2026, 3, 14, 16, 43, 30, 0, time.UTC),
				End:   time.Date(2026, 3, 14, 16, 48, 0, 0, time.UTC),
			},
			clamped: false,
			want: (&BinaryExpr{
				Left:  newColumnExpr("level", types.ColumnTypeLabel),
				Right: NewLiteral("info"),
				Op:    types.BinaryOpEq,
			}).String(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, clamped := (&clampTimeRangesToScan{}).clampExpression(tt.e, tt.tr)
			require.Equal(t, tt.clamped, clamped)
			require.Equal(t, tt.want, got.String())
		})
	}
}
