package physical

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
)

// Helper visitor to collect ParseNode
type parseNodeCollector struct {
	parseNode *ParseNode
}

func (c *parseNodeCollector) VisitDataObjScan(_ *DataObjScan) error             { return nil }
func (c *parseNodeCollector) VisitFilter(_ *Filter) error                       { return nil }
func (c *parseNodeCollector) VisitLimit(_ *Limit) error                         { return nil }
func (c *parseNodeCollector) VisitMerge(_ *Merge) error                         { return nil }
func (c *parseNodeCollector) VisitProjection(_ *Projection) error               { return nil }
func (c *parseNodeCollector) VisitRangeAggregation(_ *RangeAggregation) error   { return nil }
func (c *parseNodeCollector) VisitSortMerge(_ *SortMerge) error                 { return nil }
func (c *parseNodeCollector) VisitVectorAggregation(_ *VectorAggregation) error { return nil }
func (c *parseNodeCollector) VisitParse(n *ParseNode) error {
	c.parseNode = n
	return nil
}

// Mock catalog for testing
type mockCatalog struct {
	streamsByObject map[string]objMetadata
}

type objMetadata struct {
	streamIDs []int64
	sections  int
}

func (c *mockCatalog) ResolveDataObj(e Expression, from, through time.Time) ([]DataObjLocation, [][]int64, [][]int, error) {
	return c.ResolveDataObjWithShard(e, nil, noShard, from, through)
}

func (c *mockCatalog) ResolveDataObjWithShard(_ Expression, _ []Expression, _ ShardInfo, _, _ time.Time) ([]DataObjLocation, [][]int64, [][]int, error) {
	paths := make([]DataObjLocation, 0, len(c.streamsByObject))
	streams := make([][]int64, 0, len(c.streamsByObject))
	sections := make([][]int, 0, len(c.streamsByObject))

	for o, s := range c.streamsByObject {
		paths = append(paths, DataObjLocation(o))
		streams = append(streams, s.streamIDs)
		// Create sections array
		sectionList := make([]int, s.sections)
		for i := range sectionList {
			sectionList[i] = i
		}
		sections = append(sections, sectionList)
	}

	return paths, streams, sections, nil
}

func TestVisitorCanVisitParseNode(t *testing.T) {
	// Create a ParseNode
	parseNode := &ParseNode{
		Kind: logical.ParserLogfmt,
	}

	// Create a plan and add the node
	plan := &Plan{}
	plan.init()
	plan.addNode(parseNode)

	// Create a visitor that counts ParseNodes
	parseNodeCount := 0
	visitor := &nodeCollectVisitor{
		onVisitParse: func(_ *ParseNode) error {
			parseNodeCount++
			return nil
		},
	}

	// Visit the ParseNode
	err := plan.DFSWalk(parseNode, visitor, PreOrderWalk)
	require.NoError(t, err)

	// Assert count = 1
	require.Equal(t, 1, parseNodeCount, "Visitor should have visited exactly one ParseNode")
}
