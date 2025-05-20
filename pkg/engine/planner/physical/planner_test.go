package physical

import (
	"sort"
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
func (c *catalog) ResolveDataObj(Expression) ([]DataObjLocation, [][]int64, error) {
	objects := make([]DataObjLocation, 0, len(c.streamsByObject))
	streams := make([][]int64, 0, len(c.streamsByObject))
	for o, s := range c.streamsByObject {
		objects = append(objects, DataObjLocation(o))
		streams = append(streams, s)
	}

	// The function needs to return objects and their streamIDs in predictable order
	sort.Slice(streams, func(i, j int) bool {
		return objects[i] < objects[j]
	})
	sort.Slice(objects, func(i, j int) bool {
		return objects[i] < objects[j]
	})

	return objects, streams, nil
}

var _ Catalog = (*catalog)(nil)

func locations(t *testing.T, nodes []Node) []string {
	res := make([]string, 0, len(nodes))
	for _, n := range nodes {
		obj, ok := n.(*DataObjScan)
		if !ok {
			t.Fatalf("failed to cast Node to DataObjScan, got %T", n)
		}
		res = append(res, string(obj.Location))
	}
	return res
}

func TestPlanner_ConvertMaketable(t *testing.T) {
	catalog := &catalog{
		streamsByObject: map[string][]int64{
			"obj1": {1, 2},
			"obj2": {3, 4},
			"obj3": {5, 1},
			"obj4": {2, 3},
			"obj5": {4, 5},
		},
	}
	planner := NewPlanner(catalog)

	streamSelector := &logical.BinOp{
		Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
		Right: logical.NewLiteral("users"),
		Op:    types.BinaryOpEq,
	}

	t.Run("0_of_1 - no shard", func(t *testing.T) {
		relation := &logical.MakeTable{
			Selector: streamSelector,
			Shard:    logical.NewShardRef(0, 1),
		}
		planner.reset()
		nodes, err := planner.processMakeTable(relation)
		require.NoError(t, err)
		require.Equal(t, len(catalog.streamsByObject), len(nodes)) // all data objects are part of the shard
	})

	t.Run("0_of_2 - shard one of two", func(t *testing.T) {
		relation := &logical.MakeTable{
			Selector: streamSelector,
			Shard:    logical.NewShardRef(0, 2),
		}
		planner.reset()
		nodes, err := planner.processMakeTable(relation)
		require.NoError(t, err)
		require.Equal(t, 3, len(nodes))
		require.Equal(t, []string{"obj1", "obj3", "obj5"}, locations(t, nodes))
	})

	t.Run("1_of_2 - shard two of two", func(t *testing.T) {
		relation := &logical.MakeTable{
			Selector: streamSelector,
			Shard:    logical.NewShardRef(1, 2),
		}
		planner.reset()
		nodes, err := planner.processMakeTable(relation)
		require.NoError(t, err)
		require.Equal(t, 2, len(nodes))
		require.Equal(t, []string{"obj2", "obj4"}, locations(t, nodes))
	})

	t.Run("0_of_4 - shard one of four", func(t *testing.T) {
		relation := &logical.MakeTable{
			Selector: streamSelector,
			Shard:    logical.NewShardRef(0, 4),
		}
		planner.reset()
		nodes, err := planner.processMakeTable(relation)
		require.NoError(t, err)
		require.Equal(t, 2, len(nodes))
		require.Equal(t, []string{"obj1", "obj5"}, locations(t, nodes))
	})

	t.Run("1_of_4 - shard two of four", func(t *testing.T) {
		relation := &logical.MakeTable{
			Selector: streamSelector,
			Shard:    logical.NewShardRef(1, 4),
		}
		planner.reset()
		nodes, err := planner.processMakeTable(relation)
		require.NoError(t, err)
		require.Equal(t, 1, len(nodes))
		require.Equal(t, []string{"obj2"}, locations(t, nodes))
	})
}

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
			Shard: logical.NewShardRef(0, 1),
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
