package physical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
)

type catalog struct {
	streamsByObject map[string][]int64
}

// ResolveDataObj implements Catalog.
func (t *catalog) ResolveDataObj(Expression) ([]DataObjLocation, [][]int64) {
	objects := make([]DataObjLocation, 0, len(t.streamsByObject))
	streams := make([][]int64, 0, len(t.streamsByObject))
	for o, s := range t.streamsByObject {
		objects = append(objects, DataObjLocation(o))
		streams = append(streams, s)
	}
	return objects, streams
}

var _ Catalog = (*catalog)(nil)

func TestPlanner_Convert(t *testing.T) {
	// Build a simple query plan:
	// { app="users" } | age > 21
	b := logical.NewBuilder(
		&logical.MakeTable{
			Selector: &logical.BinOp{
				Left:  &logical.ColumnRef{Column: "app", Type: types.ColumnTypeLabel},
				Right: logical.LiteralString("users"),
				Op:    types.BinaryOpEq,
			},
		},
	).Sort(
		logical.ColumnRef{
			Column: "timestamp",
			Type:   types.ColumnTypeBuiltin,
		},
		true,
		false,
	).Select(
		&logical.BinOp{
			Left:  &logical.ColumnRef{Column: "age", Type: types.ColumnTypeMetadata},
			Right: logical.LiteralInt64(21),
			Op:    types.BinaryOpGt,
		},
	).Select(
		&logical.BinOp{
			Left:  &logical.ColumnRef{Column: "timestamp", Type: types.ColumnTypeBuiltin},
			Right: logical.LiteralUint64(1742826126000000000),
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

	t.Logf("\n%s\n", PrintAsTree(physicalPlan))
}
