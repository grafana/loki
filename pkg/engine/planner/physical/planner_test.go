package physical

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
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
func (c *catalog) ResolveDataObj(e Expression, from, through time.Time) ([]FilteredShardDescriptor, error) {
	return c.ResolveDataObjWithShard(e, nil, noShard, from, through)
}

// ResolveDataObjForShard implements Catalog.
func (c *catalog) ResolveDataObjWithShard(_ Expression, _ []Expression, shard ShardInfo, from, through time.Time) ([]FilteredShardDescriptor, error) {
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

	return filterForShard(shard, paths, streams, sections, from, through)
}

var _ Catalog = (*catalog)(nil)

func TestMockCatalog(t *testing.T) {
	catalog := &catalog{
		streamsByObject: map[string]objectMeta{
			"obj1": {streamIDs: []int64{1, 2}, sections: 3},
			"obj2": {streamIDs: []int64{3, 4}, sections: 2},
		},
	}
	timeStart := time.Now()
	timeEnd := timeStart.Add(time.Second * 10)
	for _, tt := range []struct {
		shard          ShardInfo
		expDescriptors []FilteredShardDescriptor
	}{
		{
			shard: ShardInfo{0, 1},
			expDescriptors: []FilteredShardDescriptor{{Location: "obj1", Streams: []int64{1, 2}, Sections: []int{0, 1, 2}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
				{Location: "obj2", Streams: []int64{3, 4}, Sections: []int{0, 1}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
			},
		},
		{
			shard: ShardInfo{0, 4},
			expDescriptors: []FilteredShardDescriptor{{Location: "obj1", Streams: []int64{1, 2}, Sections: []int{0}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
				{Location: "obj2", Streams: []int64{3, 4}, Sections: []int{1}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
			},
		},
		{
			shard:          ShardInfo{1, 4},
			expDescriptors: []FilteredShardDescriptor{{Location: "obj1", Streams: []int64{1, 2}, Sections: []int{1}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}}},
		},
		{
			shard:          ShardInfo{2, 4},
			expDescriptors: []FilteredShardDescriptor{{Location: "obj1", Streams: []int64{1, 2}, Sections: []int{2}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}}},
		},
		{
			shard:          ShardInfo{3, 4},
			expDescriptors: []FilteredShardDescriptor{{Location: "obj2", Streams: []int64{3, 4}, Sections: []int{0}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}}},
		},
	} {
		t.Run("shard "+tt.shard.String(), func(t *testing.T) {
			filteredShardDescriptors, err := catalog.ResolveDataObjWithShard(nil, nil, tt.shard, timeStart, timeEnd)
			require.Nil(t, err)
			require.ElementsMatch(t, tt.expDescriptors, filteredShardDescriptors)
		})
	}
}

func locations(t *testing.T, plan *Plan, nodes []Node) []string {
	res := make([]string, 0, len(nodes))

	visitor := &nodeCollectVisitor{
		onVisitDataObjScan: func(scan *DataObjScan) error {
			print("VISITED NODE", scan.Location)
			res = append(res, string(scan.Location))
			return nil
		},
	}

	for _, n := range nodes {
		require.NoError(t, plan.DFSWalk(n, visitor, PreOrderWalk))
	}
	return res
}

func sections(t *testing.T, plan *Plan, nodes []Node) [][]int {
	res := make([][]int, 0, len(nodes))

	visitor := &nodeCollectVisitor{
		onVisitDataObjScan: func(scan *DataObjScan) error {
			res = append(res, []int{scan.Section})
			return nil
		},
	}

	for _, n := range nodes {
		require.NoError(t, plan.DFSWalk(n, visitor, PreOrderWalk))
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
			shard: logical.NewShard(0, 1), // no sharding
			expPaths: []string{
				// Each section gets its own DataObjScan node, so objects here are
				// repeated once per section to scan.
				"obj1", "obj1", "obj2", "obj2", "obj3", "obj3", "obj4", "obj4", "obj5", "obj5",
			},
			expSections: [][]int{{0}, {1}, {0}, {1}, {0}, {1}, {0}, {1}, {0}, {1}},
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
			timeStart := time.Now()
			timeEnd := timeStart.Add(time.Second * 10)
			nodes, err := planner.processMakeTable(relation, NewContext(timeStart, timeEnd))
			require.NoError(t, err)
			require.ElementsMatch(t, tt.expPaths, locations(t, planner.plan, nodes))
			require.ElementsMatch(t, tt.expSections, sections(t, planner.plan, nodes))
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
			Right: logical.NewLiteral(datatype.Timestamp(1742826126000000000)),
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
			Right: logical.NewLiteral(datatype.Timestamp(1742826126000000000)),
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
