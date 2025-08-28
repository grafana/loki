package physical

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
)

func TestInsertCastNodeAfterParse(t *testing.T) {
	t.Run("planner inserts cast node for numeric unwrap", func(t *testing.T) {
		// Create a logical plan with Parse that has numeric hints
		makeTable := &logical.MakeTable{
			Selector: &logical.BinOp{
				Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
				Right: logical.NewLiteral("test"),
				Op:    types.BinaryOpEq,
			},
			Shard: logical.NewShard(0, 1),
		}

		builder := logical.NewBuilder(makeTable).
			Parse(logical.ParserLogfmt, []string{"bytes"}, map[string]logical.NumericType{"bytes": logical.NumericInt64})

		logicalPlan, err := builder.ToPlan()
		require.NoError(t, err)

		// Convert to physical plan
		ctx := NewContext(time.Now().Add(-time.Hour), time.Now())
		catalog := &mockCatalog{
			streamsByObject: map[string]objMetadata{
				"obj1": {streamIDs: []int64{1}, sections: 1},
			},
		}
		planner := NewPlanner(ctx, catalog)
		physicalPlan, err := planner.Build(logicalPlan)
		require.NoError(t, err)

		// Assert physical plan structure:
		// Should have: MakeTable -> ParseNode -> CastNode
		require.NotNil(t, physicalPlan)

		// Find the ParseNode
		var parseNode *ParseNode
		visitor := &castTestVisitor{
			onParse: func(node *ParseNode) {
				parseNode = node
			},
		}
		root, err := physicalPlan.Root()
		require.NoError(t, err)
		err = physicalPlan.DFSWalk(root, visitor, PreOrderWalk)
		require.NoError(t, err)
		require.NotNil(t, parseNode, "Should have a ParseNode")

		// Check that ParseNode has a CastNode as a parent in the plan
		parent := physicalPlan.Parent(parseNode)
		require.NotNil(t, parent, "ParseNode should have a parent")

		castNode, ok := parent.(*CastNode)
		require.True(t, ok, "ParseNode's parent should be a CastNode")

		// Verify the cast configuration
		require.Equal(t, 1, len(castNode.Casts))
		require.Equal(t, "bytes", castNode.Casts[0].Column)
		require.Equal(t, CastTypeInt64, castNode.Casts[0].Type)
	})
}

type castTestVisitor struct {
	onParse func(*ParseNode)
}

func (v *castTestVisitor) VisitDataObjScan(*DataObjScan) error             { return nil }
func (v *castTestVisitor) VisitSortMerge(*SortMerge) error                 { return nil }
func (v *castTestVisitor) VisitProjection(*Projection) error               { return nil }
func (v *castTestVisitor) VisitRangeAggregation(*RangeAggregation) error   { return nil }
func (v *castTestVisitor) VisitFilter(*Filter) error                       { return nil }
func (v *castTestVisitor) VisitMerge(*Merge) error                         { return nil }
func (v *castTestVisitor) VisitLimit(*Limit) error                         { return nil }
func (v *castTestVisitor) VisitVectorAggregation(*VectorAggregation) error { return nil }
func (v *castTestVisitor) VisitParse(node *ParseNode) error {
	if v.onParse != nil {
		v.onParse(node)
	}
	return nil
}
func (v *castTestVisitor) VisitCast(*CastNode) error { return nil }
