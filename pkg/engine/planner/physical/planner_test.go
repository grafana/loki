package physical

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
)

type catalog struct {
	streamsByObject map[string][]int64
}

// ResolveDataObj implements Catalog.
func (t *catalog) ResolveDataObj(Expression) ([]DataObjLocation, [][]int64, error) {
	objects := make([]DataObjLocation, 0, len(t.streamsByObject))
	streams := make([][]int64, 0, len(t.streamsByObject))
	for o, s := range t.streamsByObject {
		objects = append(objects, DataObjLocation(o))
		streams = append(streams, s)
	}
	return objects, streams, nil
}

var _ Catalog = (*catalog)(nil)

func TestPlanner_Convert(t *testing.T) {
	// Build a simple query plan:
	// { app="users" } | age > 21
	b := logical.NewBuilder(
		&logical.MakeTable{
			Selector: &logical.BinOp{
				Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
				Right: logical.NewLiteral("users"),
				Op:    types.BinaryOpEq,
			},
		},
	).Sort(
		*logical.NewColumnRef("timestamp", types.ColumnTypeBuiltin),
		true,
		false,
	).Select(
		&logical.BinOp{
			Left:  logical.NewColumnRef("age", types.ColumnTypeMetadata),
			Right: logical.NewLiteral(int64(21)),
			Op:    types.BinaryOpGt,
		},
	).Select(
		&logical.BinOp{
			Left:  logical.NewColumnRef("timestamp", types.ColumnTypeBuiltin),
			Right: logical.NewLiteral(time.Unix(0, 1742826126000000000)),
			Op:    types.BinaryOpLt,
		},
	).Limit(0, 1000)

	logicalPlan, err := b.ToPlan()
	require.NoError(t, err)

	catalog := &catalog{
		streamsByObject: map[string][]int64{
			"obj1": {1, 2},
			"obj2": {3, 4},
		},
	}
	planner := NewPlanner(catalog)

	physicalPlan, err := planner.Build(logicalPlan)
	require.NoError(t, err)
	t.Logf("Physical plan\n%s\n", PrintAsTree(physicalPlan))

	physicalPlan, err = planner.Optimize(physicalPlan)
	require.NoError(t, err)
	t.Logf("Optimized plan\n%s\n", PrintAsTree(physicalPlan))
}

func TestPlanner_Convert_RangeAggregations(t *testing.T) {
	// logical plan for count_over_time({ app="users" } | age > 21[5m])
	b := logical.NewBuilder(
		&logical.MakeTable{
			Selector: &logical.BinOp{
				Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
				Right: logical.NewLiteral("users"),
				Op:    types.BinaryOpEq,
			},
		},
	).Select(
		&logical.BinOp{
			Left:  logical.NewColumnRef("age", types.ColumnTypeMetadata),
			Right: logical.NewLiteral(int64(21)),
			Op:    types.BinaryOpGt,
		},
	).Select(
		&logical.BinOp{
			Left:  logical.NewColumnRef("timestamp", types.ColumnTypeBuiltin),
			Right: logical.NewLiteral(time.Unix(0, 1742826126000000000)),
			Op:    types.BinaryOpLt,
		},
	).RangeAggregation(
		[]logical.ColumnRef{*logical.NewColumnRef("label1", types.ColumnTypeAmbiguous), *logical.NewColumnRef("label2", types.ColumnTypeMetadata)},
		types.RangeAggregationTypeCount,
		time.Date(2023, 10, 1, 0, 0, 0, 0, time.UTC), // Start Time
		time.Date(2023, 10, 1, 1, 0, 0, 0, time.UTC), // End Time
		nil,           // Step
		time.Minute*5, // Range
	)

	logicalPlan, err := b.ToPlan()
	require.NoError(t, err)

	catalog := &catalog{
		streamsByObject: map[string][]int64{
			"obj1": {1, 2},
			"obj2": {3, 4},
		},
	}
	planner := NewPlanner(catalog)

	physicalPlan, err := planner.Build(logicalPlan)
	require.NoError(t, err)
	t.Logf("Physical plan\n%s\n", PrintAsTree(physicalPlan))

	physicalPlan, err = planner.Optimize(physicalPlan)
	require.NoError(t, err)
	t.Logf("Optimized plan\n%s\n", PrintAsTree(physicalPlan))
}
