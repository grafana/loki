package physical

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
)

type objectMeta struct {
	streamIDs []int64
	sections  int
}

type catalog struct {
	streamsByObject map[string]objectMeta
}

// ResolveDataObj implements Catalog.
func (c *catalog) ResolveDataObj(e Expression, from, through time.Time) ([]DataObjLocation, [][]int64, [][]int, error) {
	return c.ResolveDataObjWithShard(e, noShard, from, through)
}

// ResolveDataObjForShard implements Catalog.
func (c *catalog) ResolveDataObjWithShard(_ Expression, shard ShardInfo, _, _ time.Time) ([]DataObjLocation, [][]int64, [][]int, error) {
	paths := make([]string, 0, len(c.streamsByObject))
	streams := make([][]int64, 0, len(c.streamsByObject))
	sections := make([]int, 0, len(c.streamsByObject))

	for o, s := range c.streamsByObject {
		paths = append(paths, o)
		streams = append(streams, s.streamIDs)
		sections = append(sections, s.sections)
	}

	// The function needs to return objects and their streamIDs and sections in predictable order
	sort.Slice(streams, func(i, j int) bool {
		return paths[i] < paths[j]
	})
	sort.Slice(sections, func(i, j int) bool {
		return paths[i] < paths[j]
	})
	sort.Slice(paths, func(i, j int) bool {
		return paths[i] < paths[j]
	})

	return filterForShard(shard, paths, streams, sections)
}

var _ Catalog = (*catalog)(nil)

func TestMockCatalog(t *testing.T) {
	catalog := &catalog{
		streamsByObject: map[string]objectMeta{
			"obj1": {streamIDs: []int64{1, 2}, sections: 3},
			"obj2": {streamIDs: []int64{3, 4}, sections: 2},
		},
	}

	for _, tt := range []struct {
		shard       ShardInfo
		expPaths    []DataObjLocation
		expStreams  [][]int64
		expSections [][]int
	}{
		{
			shard:       ShardInfo{0, 1},
			expPaths:    []DataObjLocation{"obj1", "obj2"},
			expStreams:  [][]int64{{1, 2}, {3, 4}},
			expSections: [][]int{{0, 1, 2}, {0, 1}},
		},
		{
			shard:       ShardInfo{0, 4},
			expPaths:    []DataObjLocation{"obj1", "obj2"},
			expStreams:  [][]int64{{1, 2}, {3, 4}},
			expSections: [][]int{{0}, {1}},
		},
		{
			shard:       ShardInfo{1, 4},
			expPaths:    []DataObjLocation{"obj1"},
			expStreams:  [][]int64{{1, 2}},
			expSections: [][]int{{1}},
		},
		{
			shard:       ShardInfo{2, 4},
			expPaths:    []DataObjLocation{"obj1"},
			expStreams:  [][]int64{{1, 2}},
			expSections: [][]int{{2}},
		},
		{
			shard:       ShardInfo{3, 4},
			expPaths:    []DataObjLocation{"obj2"},
			expStreams:  [][]int64{{3, 4}},
			expSections: [][]int{{0}},
		},
	} {
		t.Run("shard "+tt.shard.String(), func(t *testing.T) {
			paths, streams, sections, _ := catalog.ResolveDataObjWithShard(nil, tt.shard, time.Now(), time.Now())
			require.Equal(t, tt.expPaths, paths)
			require.Equal(t, tt.expStreams, streams)
			require.Equal(t, tt.expSections, sections)
		})
	}

}

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

func sections(t *testing.T, nodes []Node) [][]int {
	res := make([][]int, 0, len(nodes))
	for _, n := range nodes {
		obj, ok := n.(*DataObjScan)
		if !ok {
			t.Fatalf("failed to cast Node to DataObjScan, got %T", n)
		}
		res = append(res, obj.Sections)
	}
	return res
}

func TestPlanner_ConvertMaketable(t *testing.T) {
	catalog := &catalog{
		streamsByObject: map[string]objectMeta{
			"obj1": {streamIDs: []int64{1, 2}, sections: 2},
			"obj2": {streamIDs: []int64{3, 4}, sections: 2},
			"obj3": {streamIDs: []int64{5, 1}, sections: 2},
			"obj4": {streamIDs: []int64{2, 3}, sections: 2},
			"obj5": {streamIDs: []int64{4, 5}, sections: 2},
		},
	}
	planner := NewPlanner(NewContext(time.Now(), time.Now()), catalog)

	streamSelector := &logical.BinOp{
		Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
		Right: logical.NewLiteral("users"),
		Op:    types.BinaryOpEq,
	}

	for _, tt := range []struct {
		shard       *logical.ShardInfo
		expPaths    []string
		expSections [][]int
	}{
		{
			shard:       logical.NewShard(0, 1), // no sharding
			expPaths:    []string{"obj1", "obj2", "obj3", "obj4", "obj5"},
			expSections: [][]int{{0, 1}, {0, 1}, {0, 1}, {0, 1}, {0, 1}},
		},
		{
			shard:       logical.NewShard(0, 2), // shard 1 of 2
			expPaths:    []string{"obj1", "obj2", "obj3", "obj4", "obj5"},
			expSections: [][]int{{0}, {0}, {0}, {0}, {0}},
		},
		{
			shard:       logical.NewShard(1, 2), // shard 2 of 2
			expPaths:    []string{"obj1", "obj2", "obj3", "obj4", "obj5"},
			expSections: [][]int{{1}, {1}, {1}, {1}, {1}},
		},
		{
			shard:       logical.NewShard(0, 4), // shard 1 of 4
			expPaths:    []string{"obj1", "obj3", "obj5"},
			expSections: [][]int{{0}, {0}, {0}},
		},
		{
			shard:       logical.NewShard(1, 4), // shard 2 of 4
			expPaths:    []string{"obj1", "obj3", "obj5"},
			expSections: [][]int{{1}, {1}, {1}},
		},
		{
			shard:       logical.NewShard(2, 4), // shard 3 of 4
			expPaths:    []string{"obj2", "obj4"},
			expSections: [][]int{{0}, {0}},
		},
		{
			shard:       logical.NewShard(3, 4), // shard 4 of 4
			expPaths:    []string{"obj2", "obj4"},
			expSections: [][]int{{1}, {1}},
		},
	} {
		t.Run("shard "+tt.shard.String(), func(t *testing.T) {
			relation := &logical.MakeTable{
				Selector: streamSelector,
				Shard:    tt.shard,
			}
			planner.reset()
			nodes, err := planner.processMakeTable(relation, NewContext(time.Now(), time.Now()))
			require.NoError(t, err)

			require.Equal(t, tt.expPaths, locations(t, nodes))
			require.Equal(t, tt.expSections, sections(t, nodes))
		})
	}
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
			Shard: logical.NewShard(0, 1), // no sharding
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
		streamsByObject: map[string]objectMeta{
			"obj1": {streamIDs: []int64{1, 2}, sections: 3},
			"obj2": {streamIDs: []int64{3, 4}, sections: 1},
		},
	}
	planner := NewPlanner(NewContext(time.Now(), time.Now()), catalog)

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
			Shard: logical.NewShard(0, 1), // no sharding
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
		0,             // Step
		time.Minute*5, // Range
	)

	logicalPlan, err := b.ToPlan()
	require.NoError(t, err)

	catalog := &catalog{
		streamsByObject: map[string]objectMeta{
			"obj1": {streamIDs: []int64{1, 2}, sections: 3},
			"obj2": {streamIDs: []int64{3, 4}, sections: 1},
		},
	}
	planner := NewPlanner(NewContext(time.Now(), time.Now()), catalog)

	physicalPlan, err := planner.Build(logicalPlan)
	require.NoError(t, err)
	t.Logf("Physical plan\n%s\n", PrintAsTree(physicalPlan))

	physicalPlan, err = planner.Optimize(physicalPlan)
	require.NoError(t, err)
	t.Logf("Optimized plan\n%s\n", PrintAsTree(physicalPlan))
}
