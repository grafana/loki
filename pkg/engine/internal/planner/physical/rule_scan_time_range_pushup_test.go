package physical

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestScanTimeRangePushup(t *testing.T) {
	t.Run("pushup time range to RangeAggregation with zero step", func(t *testing.T) {
		plan := &Plan{}
		{
			dataObjScan := plan.graph.Add(&DataObjScan{MaxTimeRange: TimeRange{Start: time.Date(2026, 3, 14, 16, 45, 29, 0, time.UTC),
				End: time.Date(2026, 3, 14, 16, 46, 31, 0, time.UTC)}})
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Start: time.Date(2026, 3, 14, 16, 53, 30, 0, time.UTC),
				End:   time.Date(2026, 3, 14, 16, 53, 30, 0, time.UTC),
				Step:  0, Range: 10 * time.Minute})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: dataObjScan})
		}

		// apply optimisations
		optimizations := []*Optimization{
			newOptimization("scan time range pushup", plan).withRules(
				&scanTimeRangePushup{plan: plan},
			),
		}
		o := NewOptimizer(plan, optimizations)
		o.Optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			dataObjScan := expectedPlan.graph.Add(&DataObjScan{MaxTimeRange: TimeRange{Start: time.Date(2026, 3, 14, 16, 45, 29, 0, time.UTC),
				End: time.Date(2026, 3, 14, 16, 46, 31, 0, time.UTC)}})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{
				Start: time.Date(2026, 3, 14, 16, 46, 31, 0, time.UTC),
				End:   time.Date(2026, 3, 14, 16, 46, 31, 0, time.UTC),
				Step:  0, Range: 62 * time.Second})
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
				&scanTimeRangePushup{plan: plan},
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
				&scanTimeRangePushup{plan: plan},
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
				&scanTimeRangePushup{plan: plan},
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
