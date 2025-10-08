package physical

import (
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

type catalog struct {
	sectionDescriptors []*metastore.DataobjSectionDescriptor
}

// ResolveShardDescriptors implements Catalog.
func (c *catalog) ResolveShardDescriptors(e Expression, from, through time.Time) ([]FilteredShardDescriptor, error) {
	return c.ResolveShardDescriptorsWithShard(e, nil, noShard, from, through)
}

// ResolveDataObjForShard implements Catalog.
func (c *catalog) ResolveShardDescriptorsWithShard(_ Expression, _ []Expression, shard ShardInfo, _, _ time.Time) ([]FilteredShardDescriptor, error) {
	return filterDescriptorsForShard(shard, c.sectionDescriptors)
}

var _ Catalog = (*catalog)(nil)

func TestMockCatalog(t *testing.T) {
	timeStart := time.Now()
	timeEnd := timeStart.Add(time.Second * 10)
	catalog := &catalog{
		sectionDescriptors: []*metastore.DataobjSectionDescriptor{
			{SectionKey: metastore.SectionKey{ObjectPath: "obj1", SectionIdx: 0}, StreamIDs: []int64{1, 2}, Start: timeStart, End: timeEnd},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj1", SectionIdx: 1}, StreamIDs: []int64{1, 2}, Start: timeStart, End: timeEnd},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj1", SectionIdx: 2}, StreamIDs: []int64{1, 2}, Start: timeStart, End: timeEnd},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj2", SectionIdx: 0}, StreamIDs: []int64{3, 4}, Start: timeStart, End: timeEnd},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj2", SectionIdx: 1}, StreamIDs: []int64{3, 4}, Start: timeStart, End: timeEnd},
		},
	}
	for _, tt := range []struct {
		shard          ShardInfo
		expDescriptors []FilteredShardDescriptor
	}{
		{
			shard: ShardInfo{0, 1},
			expDescriptors: []FilteredShardDescriptor{
				{Location: "obj1", Streams: []int64{1, 2}, Sections: []int{0}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
				{Location: "obj1", Streams: []int64{1, 2}, Sections: []int{1}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
				{Location: "obj1", Streams: []int64{1, 2}, Sections: []int{2}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
				{Location: "obj2", Streams: []int64{3, 4}, Sections: []int{0}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
				{Location: "obj2", Streams: []int64{3, 4}, Sections: []int{1}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
			},
		},
		{
			shard: ShardInfo{0, 4},
			expDescriptors: []FilteredShardDescriptor{
				{Location: "obj1", Streams: []int64{1, 2}, Sections: []int{0}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
				{Location: "obj2", Streams: []int64{3, 4}, Sections: []int{0}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
			},
		},
		{
			shard: ShardInfo{1, 4},
			expDescriptors: []FilteredShardDescriptor{
				{Location: "obj1", Streams: []int64{1, 2}, Sections: []int{1}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
				{Location: "obj2", Streams: []int64{3, 4}, Sections: []int{1}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
			},
		},
		{
			shard:          ShardInfo{2, 4},
			expDescriptors: []FilteredShardDescriptor{{Location: "obj1", Streams: []int64{1, 2}, Sections: []int{2}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}}},
		},
		{
			shard:          ShardInfo{3, 4},
			expDescriptors: []FilteredShardDescriptor{},
		},
	} {
		t.Run("shard "+tt.shard.String(), func(t *testing.T) {
			filteredShardDescriptors, err := catalog.ResolveShardDescriptorsWithShard(nil, nil, tt.shard, timeStart, timeEnd)
			require.Nil(t, err)
			require.ElementsMatch(t, tt.expDescriptors, filteredShardDescriptors)
		})
	}
}

func locations(t *testing.T, plan *Plan, nodes []Node) []string {
	res := make([]string, 0, len(nodes))

	visitor := &nodeCollectVisitor{
		onVisitDataObjScan: func(scan *DataObjScan) error {
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
	timeStart := time.Now()
	timeEnd := timeStart.Add(time.Second * 10)
	catalog := &catalog{
		sectionDescriptors: []*metastore.DataobjSectionDescriptor{
			{SectionKey: metastore.SectionKey{ObjectPath: "obj1", SectionIdx: 0}, StreamIDs: []int64{1, 2}, Start: timeStart, End: timeEnd},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj1", SectionIdx: 1}, StreamIDs: []int64{1, 2}, Start: timeStart, End: timeEnd},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj2", SectionIdx: 0}, StreamIDs: []int64{3, 4}, Start: timeStart, End: timeEnd},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj2", SectionIdx: 1}, StreamIDs: []int64{3, 4}, Start: timeStart, End: timeEnd},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj3", SectionIdx: 0}, StreamIDs: []int64{5, 1}, Start: timeStart, End: timeEnd},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj3", SectionIdx: 1}, StreamIDs: []int64{5, 1}, Start: timeStart, End: timeEnd},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj4", SectionIdx: 0}, StreamIDs: []int64{2, 3}, Start: timeStart, End: timeEnd},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj4", SectionIdx: 1}, StreamIDs: []int64{2, 3}, Start: timeStart, End: timeEnd},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj5", SectionIdx: 0}, StreamIDs: []int64{4, 5}, Start: timeStart, End: timeEnd},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj5", SectionIdx: 1}, StreamIDs: []int64{4, 5}, Start: timeStart, End: timeEnd},
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
			expPaths:    []string{"obj1", "obj2", "obj3", "obj4", "obj5"},
			expSections: [][]int{{0}, {0}, {0}, {0}, {0}},
		},
		{
			shard:       logical.NewShard(1, 4), // shard 2 of 4
			expPaths:    []string{"obj1", "obj2", "obj3", "obj4", "obj5"},
			expSections: [][]int{{1}, {1}, {1}, {1}, {1}},
		},
		{
			shard:       logical.NewShard(2, 4), // shard 3 of 4
			expPaths:    []string{},
			expSections: [][]int{},
		},
		{
			shard:       logical.NewShard(3, 4), // shard 4 of 4
			expPaths:    []string{},
			expSections: [][]int{},
		},
	} {
		t.Run("shard "+tt.shard.String(), func(t *testing.T) {
			relation := &logical.MakeTable{
				Selector: streamSelector,
				Shard:    tt.shard,
			}
			planner.reset()
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
			Right: logical.NewLiteral(types.Timestamp(1742826126000000000)),
			Op:    types.BinaryOpLt,
		},
	).Limit(0, 1000)

	logicalPlan, err := b.ToPlan()
	require.NoError(t, err)

	timeStart := time.Now()
	timeEnd := timeStart.Add(time.Second * 10)
	catalog := &catalog{
		sectionDescriptors: []*metastore.DataobjSectionDescriptor{
			{SectionKey: metastore.SectionKey{ObjectPath: "obj1", SectionIdx: 0}, StreamIDs: []int64{1, 2}, Start: timeStart, End: timeEnd},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj2", SectionIdx: 0}, StreamIDs: []int64{3, 4}, Start: timeStart, End: timeEnd},
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

func TestPlanner_Convert_WithParse(t *testing.T) {
	t.Run("Build a query plan for a log query with Parse", func(t *testing.T) {
		// Build a query plan with Parse:
		// { app="users" } | logfmt | level="error"
		b := logical.NewBuilder(
			&logical.MakeTable{
				Selector: &logical.BinOp{
					Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
					Right: logical.NewLiteral("users"),
					Op:    types.BinaryOpEq,
				},
				Shard: logical.NewShard(0, 1),
			},
		).Parse(
			logical.ParserLogfmt,
		).Select(
			&logical.BinOp{
				Left:  logical.NewColumnRef("level", types.ColumnTypeAmbiguous),
				Right: logical.NewLiteral("error"),
				Op:    types.BinaryOpEq,
			},
		)

		logicalPlan, err := b.ToPlan()
		require.NoError(t, err)

		catalog := &catalog{
			sectionDescriptors: []*metastore.DataobjSectionDescriptor{
				{SectionKey: metastore.SectionKey{ObjectPath: "obj1", SectionIdx: 0}, StreamIDs: []int64{1, 2}, Start: time.Now(), End: time.Now().Add(time.Second * 10)},
			},
		}
		planner := NewPlanner(NewContext(time.Now(), time.Now()), catalog)

		physicalPlan, err := planner.Build(logicalPlan)
		t.Logf("Physical plan\n%s\n", PrintAsTree(physicalPlan))
		require.NoError(t, err)

		// Verify ParseNode exists in correct position
		root, err := physicalPlan.Root()
		require.NoError(t, err)

		// Physical plan is built bottom up, so it should be Filter -> ParseNode -> ...
		filterNode, ok := root.(*Filter)
		require.True(t, ok, "Root should be Filter")

		children := physicalPlan.Children(filterNode)
		require.Len(t, children, 1)

		parseNode, ok := children[0].(*ParseNode)
		require.True(t, ok, "Filter's child should be ParseNode")
		require.Equal(t, ParserLogfmt, parseNode.Kind)
		require.Empty(t, parseNode.RequestedKeys)

		physicalPlan, err = planner.Optimize(physicalPlan)
		t.Logf("Optimized plan\n%s\n", PrintAsTree(physicalPlan))
		require.NoError(t, err)

		// For log queries, parse nodes should request all keys (nil)
		require.Nil(t, parseNode.RequestedKeys)
	})

	t.Run("Build a query plan for a metric query with Parse", func(t *testing.T) {
		// Build a metric query plan with Parse:
		// count_over_time({ app="users" } | logfmt | level="error" [5m])
		start := time.Date(2023, 10, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2023, 10, 1, 1, 0, 0, 0, time.UTC)

		b := logical.NewBuilder(
			&logical.MakeTable{
				Selector: &logical.BinOp{
					Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
					Right: logical.NewLiteral("users"),
					Op:    types.BinaryOpEq,
				},
				Shard: logical.NewShard(0, 1),
			},
		).Parse(
			logical.ParserLogfmt,
		).Select(
			&logical.BinOp{
				Left:  logical.NewColumnRef("level", types.ColumnTypeAmbiguous),
				Right: logical.NewLiteral("error"),
				Op:    types.BinaryOpEq,
			},
		).RangeAggregation(
			[]logical.ColumnRef{*logical.NewColumnRef("level", types.ColumnTypeAmbiguous)},
			types.RangeAggregationTypeCount,
			start,         // Start time
			end,           // End time
			time.Minute,   // Step
			5*time.Minute, // Range interval
		)

		logicalPlan, err := b.ToPlan()
		require.NoError(t, err)

		catalog := &catalog{
			sectionDescriptors: []*metastore.DataobjSectionDescriptor{
				{SectionKey: metastore.SectionKey{ObjectPath: "obj1", SectionIdx: 0}, StreamIDs: []int64{1, 2}, Start: start, End: end},
			},
		}
		planner := NewPlanner(NewContext(start, end), catalog)

		physicalPlan, err := planner.Build(logicalPlan)
		t.Logf("Physical plan\n%s\n", PrintAsTree(physicalPlan))
		require.NoError(t, err)

		// Find ParseNode in the plan
		var parseNode *ParseNode
		visitor := &nodeCollectVisitor{
			onVisitParse: func(node *ParseNode) error {
				parseNode = node
				return nil
			},
		}
		root, err := physicalPlan.Root()
		require.NoError(t, err)
		err = physicalPlan.DFSWalk(root, visitor, PreOrderWalk)
		require.NoError(t, err)
		require.NotNil(t, parseNode, "ParseNode should exist in the plan")

		require.Equal(t, ParserLogfmt, parseNode.Kind)
		require.Empty(t, parseNode.RequestedKeys) // Before optimization

		physicalPlan, err = planner.Optimize(physicalPlan)
		t.Logf("Optimized plan\n%s\n", PrintAsTree(physicalPlan))
		require.NoError(t, err)

		// For metric queries, parse nodes should request specific keys used in aggregations
		require.Equal(t, []string{"level"}, parseNode.RequestedKeys)
	})
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
			Right: logical.NewLiteral(types.Timestamp(1742826126000000000)),
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

	timeStart := time.Now()
	timeEnd := timeStart.Add(time.Second * 10)
	catalog := &catalog{
		sectionDescriptors: []*metastore.DataobjSectionDescriptor{
			{SectionKey: metastore.SectionKey{ObjectPath: "obj1", SectionIdx: 3}, StreamIDs: []int64{1, 2}, Start: timeStart, End: timeEnd},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj2", SectionIdx: 1}, StreamIDs: []int64{3, 4}, Start: timeStart, End: timeEnd},
		},
	}
	planner := NewPlanner(NewContext(timeStart, timeEnd), catalog)

	physicalPlan, err := planner.Build(logicalPlan)
	require.NoError(t, err)
	t.Logf("Physical plan\n%s\n", PrintAsTree(physicalPlan))

	physicalPlan, err = planner.Optimize(physicalPlan)
	require.NoError(t, err)
	t.Logf("Optimized plan\n%s\n", PrintAsTree(physicalPlan))
}

func TestPlanner_MakeTable_Ordering(t *testing.T) {
	// Two separate groups with different timestamps in each group
	now := time.Now()
	catalog := &catalog{
		sectionDescriptors: []*metastore.DataobjSectionDescriptor{
			{SectionKey: metastore.SectionKey{ObjectPath: "obj1", SectionIdx: 3}, StreamIDs: []int64{1, 2}, Start: now, End: now.Add(time.Second * 10)},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj2", SectionIdx: 1}, StreamIDs: []int64{3, 4}, Start: now, End: now.Add(time.Second * 10)},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj3", SectionIdx: 2}, StreamIDs: []int64{5, 1}, Start: now.Add(-time.Minute), End: now.Add(-30 * time.Second)},
			{SectionKey: metastore.SectionKey{ObjectPath: "obj3", SectionIdx: 3}, StreamIDs: []int64{5, 1}, Start: now.Add(-2 * time.Minute), End: now.Add(-45 * time.Second)},
		},
	}

	// simple logical plan for { app="users" }
	b := logical.NewBuilder(
		&logical.MakeTable{
			Selector: &logical.BinOp{
				Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
				Right: logical.NewLiteral("users"),
				Op:    types.BinaryOpEq,
			},
			Shard: logical.NewShard(0, 1), // no sharding
		},
	)

	logicalPlan, err := b.ToPlan()
	require.NoError(t, err)

	t.Run("ascending", func(t *testing.T) {
		planner := NewPlanner(NewContext(time.Now(), time.Now()).WithDirection(ASC), catalog)
		plan, err := planner.Build(logicalPlan)
		require.NoError(t, err)

		expectedPlan := &Plan{}
		merge := expectedPlan.addNode(&Merge{id: "merge"})
		sortMerge1 := expectedPlan.addNode(&SortMerge{id: "sortmerge1", Order: ASC, Column: &ColumnExpr{Ref: types.ColumnRef{Column: "timestamp", Type: types.ColumnTypeBuiltin}}})
		sortMerge2 := expectedPlan.addNode(&SortMerge{id: "sortmerge2", Order: ASC, Column: &ColumnExpr{Ref: types.ColumnRef{Column: "timestamp", Type: types.ColumnTypeBuiltin}}})
		scan1 := expectedPlan.addNode(&DataObjScan{id: "scan1", Location: "obj1", Section: 3, StreamIDs: []int64{1, 2}, Direction: ASC})
		scan2 := expectedPlan.addNode(&DataObjScan{id: "scan2", Location: "obj2", Section: 1, StreamIDs: []int64{3, 4}, Direction: ASC})
		scan3 := expectedPlan.addNode(&DataObjScan{id: "scan3", Location: "obj3", Section: 2, StreamIDs: []int64{5, 1}, Direction: ASC})
		scan4 := expectedPlan.addNode(&DataObjScan{id: "scan4", Location: "obj3", Section: 3, StreamIDs: []int64{5, 1}, Direction: ASC})

		_ = expectedPlan.addEdge(Edge{Parent: merge, Child: sortMerge1})
		_ = expectedPlan.addEdge(Edge{Parent: merge, Child: sortMerge2})

		// Sort merges should be added in the order of the scan timestamps
		// ASC => oldest to newest
		_ = expectedPlan.addEdge(Edge{Parent: sortMerge1, Child: scan3})
		_ = expectedPlan.addEdge(Edge{Parent: sortMerge1, Child: scan4})
		_ = expectedPlan.addEdge(Edge{Parent: sortMerge2, Child: scan1})
		_ = expectedPlan.addEdge(Edge{Parent: sortMerge2, Child: scan2})

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)

		pat := regexp.MustCompile("<.+?>")
		actual = pat.ReplaceAllString(actual, "")
		expected = pat.ReplaceAllString(expected, "")

		require.Equal(t, actual, expected)
	})

	t.Run("descending", func(t *testing.T) {
		planner := NewPlanner(NewContext(time.Now(), time.Now()).WithDirection(DESC), catalog)
		plan, err := planner.Build(logicalPlan)
		require.NoError(t, err)

		expectedPlan := &Plan{}
		merge := expectedPlan.addNode(&Merge{id: "merge"})
		sortMerge1 := expectedPlan.addNode(&SortMerge{id: "sortmerge1", Order: DESC, Column: &ColumnExpr{Ref: types.ColumnRef{Column: "timestamp", Type: types.ColumnTypeBuiltin}}})
		sortMerge2 := expectedPlan.addNode(&SortMerge{id: "sortmerge2", Order: DESC, Column: &ColumnExpr{Ref: types.ColumnRef{Column: "timestamp", Type: types.ColumnTypeBuiltin}}})
		scan1 := expectedPlan.addNode(&DataObjScan{id: "scan1", Location: "obj1", Section: 3, StreamIDs: []int64{1, 2}, Direction: DESC})
		scan2 := expectedPlan.addNode(&DataObjScan{id: "scan2", Location: "obj2", Section: 1, StreamIDs: []int64{3, 4}, Direction: DESC})
		scan3 := expectedPlan.addNode(&DataObjScan{id: "scan3", Location: "obj3", Section: 2, StreamIDs: []int64{5, 1}, Direction: DESC})
		scan4 := expectedPlan.addNode(&DataObjScan{id: "scan4", Location: "obj3", Section: 3, StreamIDs: []int64{5, 1}, Direction: DESC})

		_ = expectedPlan.addEdge(Edge{Parent: merge, Child: sortMerge1})
		_ = expectedPlan.addEdge(Edge{Parent: merge, Child: sortMerge2})

		// Sort merges should be added in the order of the scan timestamps
		_ = expectedPlan.addEdge(Edge{Parent: sortMerge1, Child: scan1})
		_ = expectedPlan.addEdge(Edge{Parent: sortMerge1, Child: scan2})
		_ = expectedPlan.addEdge(Edge{Parent: sortMerge2, Child: scan3})
		_ = expectedPlan.addEdge(Edge{Parent: sortMerge2, Child: scan4})

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)

		pat := regexp.MustCompile("<.+?>")
		actual = pat.ReplaceAllString(actual, "")
		expected = pat.ReplaceAllString(expected, "")

		require.Equal(t, actual, expected)
	})
}

func TestPlanner_OverlappingShardDescriptors(t *testing.T) {
	tests := []struct {
		name   string
		ranges []TimeRange
		groups int
	}{
		{
			name: "Isolated groups",
			ranges: []TimeRange{
				{Start: time.UnixMilli(1), End: time.UnixMilli(2)},
				{Start: time.UnixMilli(3), End: time.UnixMilli(4)},
				{Start: time.UnixMilli(5), End: time.UnixMilli(6)},
			},
			groups: 3,
		},
		{
			name: "Equal start and end are one group",
			ranges: []TimeRange{
				{Start: time.UnixMilli(1), End: time.UnixMilli(2)},
				{Start: time.UnixMilli(2), End: time.UnixMilli(4)},
			},
			groups: 1,
		},
		{
			name: "One range contains two isolated groups",
			ranges: []TimeRange{
				{Start: time.UnixMilli(1), End: time.UnixMilli(2)},
				{Start: time.UnixMilli(3), End: time.UnixMilli(4)},
				{Start: time.UnixMilli(0), End: time.UnixMilli(5)},
			},
			groups: 1,
		},
		{
			name: "One range spans two isolated groups",
			ranges: []TimeRange{
				{Start: time.UnixMilli(0), End: time.UnixMilli(2)},
				{Start: time.UnixMilli(4), End: time.UnixMilli(5)},
				{Start: time.UnixMilli(2), End: time.UnixMilli(4)},
			},
			groups: 1,
		},
		{
			name: "Real world example",
			ranges: []TimeRange{
				{Start: time.Date(2025, time.September, 16, 15, 0, 31, 361695211, time.UTC), End: time.Date(2025, time.September, 16, 15, 0, 46, 800186241, time.UTC)},
				{Start: time.Date(2025, time.September, 16, 15, 0, 31, 350398040, time.UTC), End: time.Date(2025, time.September, 16, 15, 0, 31, 350398040, time.UTC)},
				{Start: time.Date(2025, time.September, 16, 15, 0, 31, 330227014, time.UTC), End: time.Date(2025, time.September, 16, 15, 1, 3, 337407239, time.UTC)},
			},
			groups: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			descriptors := []FilteredShardDescriptor{}
			for _, tr := range tt.ranges {
				descriptors = append(descriptors, FilteredShardDescriptor{TimeRange: tr})
			}

			groups := overlappingShardDescriptors(descriptors)
			require.Equal(t, tt.groups, len(groups))
		})
	}
}
