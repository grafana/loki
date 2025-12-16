package physical

import (
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

type catalog struct {
	sectionDescriptors []*metastore.DataobjSectionDescriptor
}

// ResolveShardDescriptors implements Catalog.
func (c *catalog) ResolveDataObjSections(_ Expression, _ []Expression, shard ShardInfo, _, _ time.Time) ([]DataObjSections, error) {
	return filterForShard(shard, c.sectionDescriptors)
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
		expDescriptors []DataObjSections
	}{
		{
			shard: ShardInfo{0, 1},
			expDescriptors: []DataObjSections{
				{Location: "obj1", Streams: []int64{1, 2}, Sections: []int{0}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
				{Location: "obj1", Streams: []int64{1, 2}, Sections: []int{1}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
				{Location: "obj1", Streams: []int64{1, 2}, Sections: []int{2}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
				{Location: "obj2", Streams: []int64{3, 4}, Sections: []int{0}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
				{Location: "obj2", Streams: []int64{3, 4}, Sections: []int{1}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
			},
		},
		{
			shard: ShardInfo{0, 4},
			expDescriptors: []DataObjSections{
				{Location: "obj1", Streams: []int64{1, 2}, Sections: []int{0}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
				{Location: "obj2", Streams: []int64{3, 4}, Sections: []int{0}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
			},
		},
		{
			shard: ShardInfo{1, 4},
			expDescriptors: []DataObjSections{
				{Location: "obj1", Streams: []int64{1, 2}, Sections: []int{1}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
				{Location: "obj2", Streams: []int64{3, 4}, Sections: []int{1}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}},
			},
		},
		{
			shard:          ShardInfo{2, 4},
			expDescriptors: []DataObjSections{{Location: "obj1", Streams: []int64{1, 2}, Sections: []int{2}, TimeRange: TimeRange{Start: timeStart, End: timeEnd}}},
		},
		{
			shard:          ShardInfo{3, 4},
			expDescriptors: []DataObjSections{},
		},
	} {
		t.Run("shard "+tt.shard.String(), func(t *testing.T) {
			filteredShardDescriptors, err := catalog.ResolveDataObjSections(nil, nil, tt.shard, timeStart, timeEnd)
			require.Nil(t, err)
			require.ElementsMatch(t, tt.expDescriptors, filteredShardDescriptors)
		})
	}
}

func locations(plan *Plan, node Node) []string {
	res := make([]string, 0)
	_ = plan.DFSWalk(node, func(n Node) error {
		switch node := n.(type) {
		case *ScanSet:
			for _, target := range node.Targets {
				switch target.Type {
				case ScanTypeDataObject:
					res = append(res, string(target.DataObject.Location))
				}
			}
		}
		return nil
	}, dag.PreOrderWalk)
	return res
}

func sections(plan *Plan, node Node) [][]int {
	res := make([][]int, 0)
	_ = plan.DFSWalk(node, func(n Node) error {
		switch node := n.(type) {
		case *ScanSet:
			for _, target := range node.Targets {
				switch target.Type {
				case ScanTypeDataObject:
					res = append(res, []int{target.DataObject.Section})
				}
			}
		}
		return nil
	}, dag.PreOrderWalk)
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
			node, err := planner.processMakeTable(relation, NewContext(timeStart, timeEnd))
			require.NoError(t, err)
			require.ElementsMatch(t, tt.expPaths, locations(planner.plan, node))
			require.ElementsMatch(t, tt.expSections, sections(planner.plan, node))
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
			types.VariadicOpParseLogfmt, false, false,
		).Select(
			&logical.BinOp{
				Left:  logical.NewColumnRef("level", types.ColumnTypeAmbiguous),
				Right: logical.NewLiteral("error"),
				Op:    types.BinaryOpEq,
			},
		).Compat(true)

		logicalPlan, err := b.ToPlan()
		require.NoError(t, err)

		catalog := &catalog{
			sectionDescriptors: []*metastore.DataobjSectionDescriptor{
				{SectionKey: metastore.SectionKey{ObjectPath: "obj1", SectionIdx: 0}, StreamIDs: []int64{1, 2}, Start: time.Now(), End: time.Now().Add(time.Second * 10)},
			},
		}
		planner := NewPlanner(NewContext(time.Now(), time.Now()), catalog)

		physicalPlan, err := planner.Build(logicalPlan)
		t.Logf("\nPhysical plan\n%s\n", PrintAsTree(physicalPlan))
		require.NoError(t, err)

		// Verify ParseNode exists in correct position
		root, err := physicalPlan.Root()
		require.NoError(t, err)

		// Physical plan is built bottom up, so it should be Filter -> ParseNode -> ...
		filterNode, ok := root.(*Filter)
		require.True(t, ok, "Root should be Filter")

		children := physicalPlan.Children(filterNode)
		require.Len(t, children, 1)

		compatNode, ok := children[0].(*ColumnCompat)
		require.True(t, ok, "Filter's child should be ColumnCompat")
		children = physicalPlan.Children(compatNode)

		projectionNode, ok := children[0].(*Projection)
		require.True(t, ok, "ColumnCompat's child should be Projection")
		require.Len(t, projectionNode.Expressions, 1)

		expr, ok := projectionNode.Expressions[0].(*VariadicExpr)
		require.True(t, ok)
		require.Equal(t, types.VariadicOpParseLogfmt, expr.Op)

		funcArgs := expr.Expressions
		require.Len(t, funcArgs, 4)

		sourcCol, ok := funcArgs[0].(*ColumnExpr)
		require.True(t, ok)
		require.Equal(t, types.ColumnNameBuiltinMessage, sourcCol.Ref.Column)
		require.Equal(t, types.ColumnTypeBuiltin, sourcCol.Ref.Type)

		physicalPlan, err = planner.Optimize(physicalPlan)
		t.Logf("\nOptimized plan\n%s\n", PrintAsTree(physicalPlan))
		require.NoError(t, err)

		funcArgs = expr.Expressions
		require.Len(t, funcArgs, 4)

		sourcCol, ok = funcArgs[0].(*ColumnExpr)
		require.True(t, ok)
		require.Equal(t, types.ColumnNameBuiltinMessage, sourcCol.Ref.Column)
		require.Equal(t, types.ColumnTypeBuiltin, sourcCol.Ref.Type)
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
			types.VariadicOpParseLogfmt, false, false,
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
		).Compat(true)

		logicalPlan, err := b.ToPlan()
		require.NoError(t, err)

		catalog := &catalog{
			sectionDescriptors: []*metastore.DataobjSectionDescriptor{
				{SectionKey: metastore.SectionKey{ObjectPath: "obj1", SectionIdx: 0}, StreamIDs: []int64{1, 2}, Start: start, End: end},
			},
		}
		planner := NewPlanner(NewContext(start, end), catalog)

		physicalPlan, err := planner.Build(logicalPlan)
		t.Logf("\nPhysical plan\n%s\n", PrintAsTree(physicalPlan))
		require.NoError(t, err)

		// Verify ParseNode exists in correct position
		root, err := physicalPlan.Root()
		require.NoError(t, err)

		// Physical plan is built bottom up, so it should be RangeAggregation -> Filter -> Projection -> ...
		rangeAgg, ok := root.(*RangeAggregation)
		require.True(t, ok, "Root should be RangeAggregation")

		children := physicalPlan.Children(rangeAgg)
		require.Len(t, children, 1)

		filterNode, ok := children[0].(*Filter)
		require.True(t, ok, "RangeAggregation's child should be Filter")

		children = physicalPlan.Children(filterNode)
		require.Len(t, children, 1)

		compatNode, ok := children[0].(*ColumnCompat)
		require.True(t, ok, "Filter's child should be ColumnCompat")
		children = physicalPlan.Children(compatNode)

		projectionNode, ok := children[0].(*Projection)
		require.True(t, ok, "ColumnCompat's child should be Projection")
		require.Len(t, projectionNode.Expressions, 1)

		expr, ok := projectionNode.Expressions[0].(*VariadicExpr)
		require.True(t, ok)
		require.Equal(t, types.VariadicOpParseLogfmt, expr.Op)

		funcArgs := expr.Expressions
		require.Len(t, funcArgs, 4)

		sourcCol, ok := funcArgs[0].(*ColumnExpr)
		require.True(t, ok)
		require.Equal(t, types.ColumnNameBuiltinMessage, sourcCol.Ref.Column)
		require.Equal(t, types.ColumnTypeBuiltin, sourcCol.Ref.Type)

		physicalPlan, err = planner.Optimize(physicalPlan)
		t.Logf("\nOptimized plan\n%s\n", PrintAsTree(physicalPlan))
		require.NoError(t, err)

		funcArgs = expr.Expressions
		require.Len(t, funcArgs, 4)

		sourcCol, ok = funcArgs[0].(*ColumnExpr)
		require.True(t, ok)
		require.Equal(t, types.ColumnNameBuiltinMessage, sourcCol.Ref.Column)
		require.Equal(t, types.ColumnTypeBuiltin, sourcCol.Ref.Type)

		reqKeys, ok := funcArgs[1].(*LiteralExpr)
		require.True(t, ok)

		keys, ok := reqKeys.Literal().(types.StringListLiteral)
		require.True(t, ok)
		require.Equal(t, []string{"level"}, keys.Value())
	})
}

func TestPlanner_Convert_WithCastProjection(t *testing.T) {
	t.Run("Build a query plan for a log query with unwrap", func(t *testing.T) {
		// Build a query plan with unwrap:
		// { app="users" } | unwrap duration(request_duration)
		b := logical.NewBuilder(
			&logical.MakeTable{
				Selector: &logical.BinOp{
					Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
					Right: logical.NewLiteral("users"),
					Op:    types.BinaryOpEq,
				},
				Shard: logical.NewShard(0, 1),
			},
		).Cast(
			"request_duration", types.UnaryOpCastDuration,
		).Compat(true)

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

		// Verify Projection node exists at root (unwrap is now implemented as projection)
		root, err := physicalPlan.Root()
		require.NoError(t, err)

		// Root should be a Projection node with the unwrap cast operation
		projectionNode, ok := root.(*Projection)
		require.True(t, ok, "Root should be Projection")
		require.NotEmpty(t, projectionNode.Expressions, "Projection should have expressions")

		physicalPlan, err = planner.Optimize(physicalPlan)
		t.Logf("Optimized plan\n%s\n", PrintAsTree(physicalPlan))
		require.NoError(t, err)
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
		[]logical.ColumnRef{},
		types.RangeAggregationTypeCount,
		time.Date(2023, 10, 1, 0, 0, 0, 0, time.UTC), // Start Time
		time.Date(2023, 10, 1, 1, 0, 0, 0, time.UTC), // End Time
		0,             // Step
		time.Minute*5, // Range
	).Compat(true)

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

func TestPlanner_Convert_Rate(t *testing.T) {
	// logical plan for rate({ app="users" } | age > 21[5m])
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
		[]logical.ColumnRef{},
		types.RangeAggregationTypeCount,
		time.Date(2023, 10, 1, 0, 0, 0, 0, time.UTC), // Start Time
		time.Date(2023, 10, 1, 1, 0, 0, 0, time.UTC), // End Time
		0,             // Step
		time.Minute*5, // Range
	).BinOpRight(
		types.BinaryOpDiv, logical.NewLiteral(int64(300)),
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

func TestPlanner_BuildMathExpressions(t *testing.T) {
	// logical plan for (rate({ app="users" }[5m]) * 40) ^ 2
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
		[]logical.ColumnRef{},
		types.RangeAggregationTypeCount,
		time.Date(2023, 10, 1, 0, 0, 0, 0, time.UTC), // Start Time
		time.Date(2023, 10, 1, 1, 0, 0, 0, time.UTC), // End Time
		0,             // Step
		time.Minute*5, // Range
	).BinOpRight(
		types.BinaryOpDiv, logical.NewLiteral(int64(300)),
	).BinOpRight(
		types.BinaryOpMul, logical.NewLiteral(int64(40)),
	).BinOpRight(
		types.BinaryOpPow, logical.NewLiteral(int64(2)),
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

func TestPlanner_BuildMathExpressionsWithTwoInputs(t *testing.T) {
	// logical plan for rate({ env="prod", app="users" }[5m])) / rate({ env="prod" }[5m]))
	b1 := logical.NewBuilder(
		&logical.MakeTable{
			Selector: &logical.BinOp{
				Left:  logical.NewColumnRef("env", types.ColumnTypeLabel),
				Right: logical.NewLiteral("prod"),
				Op:    types.BinaryOpEq,
			},
			Shard: logical.NewShard(0, 1), // no sharding
		},
	).Select(
		&logical.BinOp{
			Left:  logical.NewColumnRef("timestamp", types.ColumnTypeBuiltin),
			Right: logical.NewLiteral(types.Timestamp(1742826126000000000)),
			Op:    types.BinaryOpLt,
		},
	).RangeAggregation(
		[]logical.ColumnRef{},
		types.RangeAggregationTypeCount,
		time.Date(2023, 10, 1, 0, 0, 0, 0, time.UTC), // Start Time
		time.Date(2023, 10, 1, 1, 0, 0, 0, time.UTC), // End Time
		0,             // Step
		time.Minute*5, // Range
	).BinOpRight(
		types.BinaryOpDiv, logical.NewLiteral(float64(300)),
	)
	b2 := logical.NewBuilder(
		&logical.MakeTable{
			Selector: &logical.BinOp{
				Op: types.BinaryOpAnd,
				Left: &logical.BinOp{
					Left:  logical.NewColumnRef("env", types.ColumnTypeLabel),
					Right: logical.NewLiteral("prod"),
					Op:    types.BinaryOpEq,
				},
				Right: &logical.BinOp{
					Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
					Right: logical.NewLiteral("users"),
					Op:    types.BinaryOpEq,
				},
			},
			Shard: logical.NewShard(0, 1), // no sharding
		},
	).Select(
		&logical.BinOp{
			Left:  logical.NewColumnRef("timestamp", types.ColumnTypeBuiltin),
			Right: logical.NewLiteral(types.Timestamp(1742826126000000000)),
			Op:    types.BinaryOpLt,
		},
	).RangeAggregation(
		[]logical.ColumnRef{},
		types.RangeAggregationTypeCount,
		time.Date(2023, 10, 1, 0, 0, 0, 0, time.UTC), // Start Time
		time.Date(2023, 10, 1, 1, 0, 0, 0, time.UTC), // End Time
		0,             // Step
		time.Minute*5, // Range
	).BinOpRight(
		types.BinaryOpDiv, logical.NewLiteral(float64(300)),
	).BinOpRight(
		types.BinaryOpDiv, b1.Value(),
	)

	logicalPlan, err := b2.ToPlan()
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
	).Compat(true)

	logicalPlan, err := b.ToPlan()
	require.NoError(t, err)

	t.Run("ascending", func(t *testing.T) {
		planner := NewPlanner(NewContext(now, now.Add(time.Minute)).WithDirection(ASC), catalog)
		plan, err := planner.Build(logicalPlan)
		require.NoError(t, err)

		expectedPlan := &Plan{}
		parallelize := expectedPlan.graph.Add(&Parallelize{})
		compat := expectedPlan.graph.Add(&ColumnCompat{Source: types.ColumnTypeMetadata, Destination: types.ColumnTypeMetadata, Collisions: []types.ColumnType{types.ColumnTypeLabel}})
		scanSet := expectedPlan.graph.Add(&ScanSet{
			// Targets should be added in the order of the scan timestamps
			// ASC => oldest to newest
			Targets: []*ScanTarget{
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{
					Location: "obj3", Section: 3, StreamIDs: []int64{5, 1}, MaxTimeRange: TimeRange{Start: now.Add(-2 * time.Minute), End: now.Add(-45 * time.Second)},
				}},
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{
					Location: "obj3", Section: 2, StreamIDs: []int64{5, 1}, MaxTimeRange: TimeRange{Start: now.Add(-time.Minute), End: now.Add(-30 * time.Second)},
				}},
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{
					Location: "obj2", Section: 1, StreamIDs: []int64{3, 4}, MaxTimeRange: TimeRange{Start: now, End: now.Add(time.Second * 10)},
				}},
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{
					Location: "obj1", Section: 3, StreamIDs: []int64{1, 2}, MaxTimeRange: TimeRange{Start: now, End: now.Add(time.Second * 10)},
				}},
			},
		})

		_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: compat})
		_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: compat, Child: scanSet})

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)

		pat := regexp.MustCompile("<.+?>")
		actual = pat.ReplaceAllString(actual, "")
		expected = pat.ReplaceAllString(expected, "")

		require.Equal(t, expected, actual)
	})

	t.Run("descending", func(t *testing.T) {
		planner := NewPlanner(NewContext(now, now.Add(time.Minute)).WithDirection(DESC), catalog)
		plan, err := planner.Build(logicalPlan)
		require.NoError(t, err)

		expectedPlan := &Plan{}
		parallelize := expectedPlan.graph.Add(&Parallelize{})
		compat := expectedPlan.graph.Add(&ColumnCompat{Source: types.ColumnTypeMetadata, Destination: types.ColumnTypeMetadata, Collisions: []types.ColumnType{types.ColumnTypeLabel}})
		scanSet := expectedPlan.graph.Add(&ScanSet{
			// Targets should be added in the order of the scan timestamps
			Targets: []*ScanTarget{
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{
					Location: "obj1", Section: 3, StreamIDs: []int64{1, 2}, MaxTimeRange: TimeRange{Start: now, End: now.Add(time.Second * 10)},
				}},
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{
					Location: "obj2", Section: 1, StreamIDs: []int64{3, 4}, MaxTimeRange: TimeRange{Start: now, End: now.Add(time.Second * 10)},
				}},
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{
					Location: "obj3", Section: 2, StreamIDs: []int64{5, 1}, MaxTimeRange: TimeRange{Start: now.Add(-time.Minute), End: now.Add(-30 * time.Second)},
				}},
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{
					Location: "obj3", Section: 3, StreamIDs: []int64{5, 1}, MaxTimeRange: TimeRange{Start: now.Add(-2 * time.Minute), End: now.Add(-45 * time.Second)},
				}},
			},
		})

		_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: compat})
		_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: compat, Child: scanSet})

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)

		pat := regexp.MustCompile("<.+?>")
		actual = pat.ReplaceAllString(actual, "")
		expected = pat.ReplaceAllString(expected, "")

		require.Equal(t, expected, actual)
	})
}
