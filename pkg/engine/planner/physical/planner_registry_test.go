package physical

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
)

func TestPlanner_ParseNodeRegistersColumns(t *testing.T) {
	t.Run("Parse node registers columns and filter resolves them", func(t *testing.T) {
		// Create a logical plan with Parse followed by Select with filter
		builder := logical.NewBuilder(&logical.MakeTable{
			Selector: &logical.BinOp{
				Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
				Right: logical.NewLiteral("test"),
				Op:    types.BinaryOpEq,
			},
			Shard: logical.NewShard(0, 1), // noShard
		})

		// Add Parse instruction that will extract "level" and "status"
		builder = builder.Parse(logical.ParserLogfmt, []string{"level", "status"})

		// Add Select with filter on parsed column
		builder = builder.Select(&logical.BinOp{
			Left:  logical.NewColumnRef("level", types.ColumnTypeAmbiguous), // Ambiguous at logical level
			Right: logical.NewLiteral("error"),
			Op:    types.BinaryOpEq,
		})

		logicalPlan, err := builder.ToPlan()
		require.NoError(t, err)

		// Create physical planner with registry support
		ctx := NewContext(time.Now().Add(-1*time.Hour), time.Now())
		catalog := &catalog{
			streamsByObject: map[string]objectMeta{
				"obj1": {streamIDs: []int64{1}, sections: 1},
			},
		}

		planner := NewPlannerWithRegistry(ctx, catalog)
		physicalPlan, err := planner.Build(logicalPlan)
		require.NoError(t, err)

		// Find the Filter node in the physical plan
		var filterNode *Filter
		visitor := &testVisitor{
			visitFilter: func(n *Filter) {
				filterNode = n
			},
		}
		root, err := physicalPlan.Root()
		require.NoError(t, err)
		err = physicalPlan.DFSWalk(root, visitor, PreOrderWalk)
		require.NoError(t, err)
		require.NotNil(t, filterNode, "Should have a Filter node")

		// The filter expression should have resolved "level" to ColumnTypeParsed
		require.Len(t, filterNode.Predicates, 1, "Should have one predicate")
		binaryExpr, ok := filterNode.Predicates[0].(*BinaryExpr)
		require.True(t, ok, "Filter should have BinaryExpr")

		columnExpr, ok := binaryExpr.Left.(*ColumnExpr)
		require.True(t, ok, "Left side should be ColumnExpr")

		// This is the key assertion: "level" should be resolved as Parsed, not Ambiguous
		require.Equal(t, types.ColumnTypeParsed, columnExpr.Ref.Type,
			"Column 'level' should be resolved as ColumnTypeParsed after Parse node registered it")
		require.Equal(t, "level", columnExpr.Ref.Column)
	})

	t.Run("Multiple Parse nodes accumulate columns", func(t *testing.T) {
		// Create a plan with two Parse nodes
		builder := logical.NewBuilder(&logical.MakeTable{
			Selector: &logical.BinOp{
				Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
				Right: logical.NewLiteral("test"),
				Op:    types.BinaryOpEq,
			},
			Shard: logical.NewShard(0, 1), // noShard
		})

		// First parse extracts "level"
		builder = builder.Parse(logical.ParserLogfmt, []string{"level"})

		// Second parse extracts "status"
		builder = builder.Parse(logical.ParserLogfmt, []string{"status"})

		// Filter uses both parsed columns
		builder = builder.Select(&logical.BinOp{
			Left: &logical.BinOp{
				Left:  logical.NewColumnRef("level", types.ColumnTypeAmbiguous),
				Right: logical.NewLiteral("error"),
				Op:    types.BinaryOpEq,
			},
			Right: &logical.BinOp{
				Left:  logical.NewColumnRef("status", types.ColumnTypeAmbiguous),
				Right: logical.NewLiteral(int64(500)),
				Op:    types.BinaryOpEq,
			},
			Op: types.BinaryOpAnd,
		})

		logicalPlan, err := builder.ToPlan()
		require.NoError(t, err)

		// Create physical planner with registry support
		ctx := NewContext(time.Now().Add(-1*time.Hour), time.Now())
		catalog := &catalog{
			streamsByObject: map[string]objectMeta{
				"obj1": {streamIDs: []int64{1}, sections: 1},
			},
		}

		planner := NewPlannerWithRegistry(ctx, catalog)
		physicalPlan, err := planner.Build(logicalPlan)
		require.NoError(t, err)

		// Find the Filter node
		var filterNode *Filter
		visitor := &testVisitor{
			visitFilter: func(n *Filter) {
				filterNode = n
			},
		}
		root, err := physicalPlan.Root()
		require.NoError(t, err)
		err = physicalPlan.DFSWalk(root, visitor, PreOrderWalk)
		require.NoError(t, err)
		require.NotNil(t, filterNode)

		// Check that both columns are resolved as parsed
		require.Len(t, filterNode.Predicates, 1, "Should have one compound predicate")
		andExpr, ok := filterNode.Predicates[0].(*BinaryExpr)
		require.True(t, ok)
		require.Equal(t, types.BinaryOpAnd, andExpr.Op)

		// Check left side (level="error")
		leftBinary, ok := andExpr.Left.(*BinaryExpr)
		require.True(t, ok)
		leftColumn, ok := leftBinary.Left.(*ColumnExpr)
		require.True(t, ok)
		require.Equal(t, types.ColumnTypeParsed, leftColumn.Ref.Type, "level should be parsed")

		// Check right side (status=500)
		rightBinary, ok := andExpr.Right.(*BinaryExpr)
		require.True(t, ok)
		rightColumn, ok := rightBinary.Left.(*ColumnExpr)
		require.True(t, ok)
		require.Equal(t, types.ColumnTypeParsed, rightColumn.Ref.Type, "status should be parsed")
	})

	t.Run("Stream labels are registered and have correct precedence", func(t *testing.T) {
		// Create a plan where "app" is both a stream label and parsed field
		builder := logical.NewBuilder(&logical.MakeTable{
			Selector: &logical.BinOp{
				Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
				Right: logical.NewLiteral("test"),
				Op:    types.BinaryOpEq,
			},
			Shard: logical.NewShard(0, 1), // noShard
		})

		// Parse also extracts "app" (same name as label)
		builder = builder.Parse(logical.ParserLogfmt, []string{"app", "level"})

		// Filter on "app" - should resolve to parsed (higher precedence)
		builder = builder.Select(&logical.BinOp{
			Left:  logical.NewColumnRef("app", types.ColumnTypeAmbiguous),
			Right: logical.NewLiteral("backend"),
			Op:    types.BinaryOpEq,
		})

		logicalPlan, err := builder.ToPlan()
		require.NoError(t, err)

		ctx := NewContext(time.Now().Add(-1*time.Hour), time.Now())
		catalog := &catalog{
			streamsByObject: map[string]objectMeta{
				"obj1": {streamIDs: []int64{1}, sections: 1},
			},
		}

		planner := NewPlannerWithRegistry(ctx, catalog)
		physicalPlan, err := planner.Build(logicalPlan)
		require.NoError(t, err)

		// Find the Filter node
		var filterNode *Filter
		visitor := &testVisitor{
			visitFilter: func(n *Filter) {
				filterNode = n
			},
		}
		root, err := physicalPlan.Root()
		require.NoError(t, err)
		err = physicalPlan.DFSWalk(root, visitor, PreOrderWalk)
		require.NoError(t, err)
		require.NotNil(t, filterNode)

		// "app" should resolve to parsed (higher precedence than label)
		require.Len(t, filterNode.Predicates, 1, "Should have one predicate")
		binaryExpr, ok := filterNode.Predicates[0].(*BinaryExpr)
		require.True(t, ok)
		columnExpr, ok := binaryExpr.Left.(*ColumnExpr)
		require.True(t, ok)
		require.Equal(t, types.ColumnTypeParsed, columnExpr.Ref.Type,
			"Column 'app' should be ColumnTypeParsed due to precedence")
	})
}

// testVisitor is a test helper for traversing physical plans
type testVisitor struct {
	visitFilter func(*Filter)
	visitParse  func(*ParseNode)
}

func (v *testVisitor) VisitDataObjScan(*DataObjScan) error {
	return nil
}

func (v *testVisitor) VisitFilter(n *Filter) error {
	if v.visitFilter != nil {
		v.visitFilter(n)
	}
	return nil
}

func (v *testVisitor) VisitLimit(*Limit) error {
	return nil
}

func (v *testVisitor) VisitProjection(*Projection) error {
	return nil
}

func (v *testVisitor) VisitSortMerge(*SortMerge) error {
	return nil
}

func (v *testVisitor) VisitParse(n *ParseNode) error {
	if v.visitParse != nil {
		v.visitParse(n)
	}
	return nil
}

func (v *testVisitor) VisitRangeAggregation(*RangeAggregation) error {
	return nil
}

func (v *testVisitor) VisitVectorAggregation(*VectorAggregation) error {
	return nil
}

func (v *testVisitor) VisitMerge(*Merge) error {
	return nil
}
