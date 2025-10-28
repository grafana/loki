package workflow

import (
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// TODO(rfratto): Once physical plans can be serializable, we should be able to
// write these tests using serialized physical plans rather than constructing
// them with the DAG.
//
// That would also allow for writing tests using txtar[^1], which may be easier
// to write and maintain.
//
// [^1]: https://research.swtch.com/testing

func Test_planWorkflow(t *testing.T) {
	t.Run("no pipeline breakers", func(t *testing.T) {
		ulidGen := ulidGenerator{}

		var physicalGraph dag.Graph[physical.Node]
		physicalGraph.Add(&physical.DataObjScan{})

		physicalPlan := physical.FromGraph(physicalGraph)

		graph, err := planWorkflow("", physicalPlan)
		require.NoError(t, err)
		require.Equal(t, 1, graph.Len())
		requireUniqueStreams(t, graph)
		generateConsistentULIDs(&ulidGen, graph)

		expectOuptut := strings.TrimSpace(`
Task 00000000000000000000000001
-------------------------------
DataObjScan location= streams=0 section_id=0 projections=()
`)

		actualOutput := Sprint(&Workflow{graph: graph})
		require.Equal(t, strings.TrimSpace(expectOuptut), strings.TrimSpace(actualOutput))
	})

	t.Run("ends with one pipeline breaker", func(t *testing.T) {
		ulidGen := ulidGenerator{}

		var physicalGraph dag.Graph[physical.Node]

		var (
			scan     = physicalGraph.Add(&physical.DataObjScan{})
			rangeAgg = physicalGraph.Add(&physical.RangeAggregation{})
		)

		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: rangeAgg, Child: scan})

		physicalPlan := physical.FromGraph(physicalGraph)

		graph, err := planWorkflow("", physicalPlan)
		require.NoError(t, err)
		require.Equal(t, 1, graph.Len())
		requireUniqueStreams(t, graph)
		generateConsistentULIDs(&ulidGen, graph)

		expectOuptut := strings.TrimSpace(`
Task 00000000000000000000000001
-------------------------------
RangeAggregation operation=invalid start=0001-01-01T00:00:00Z end=0001-01-01T00:00:00Z step=0s range=0s
└── DataObjScan location= streams=0 section_id=0 projections=()
`)

		actualOutput := Sprint(&Workflow{graph: graph})
		require.Equal(t, strings.TrimSpace(expectOuptut), strings.TrimSpace(actualOutput))
	})

	t.Run("split on pipeline breaker", func(t *testing.T) {
		ulidGen := ulidGenerator{}

		var physicalGraph dag.Graph[physical.Node]

		var (
			scan      = physicalGraph.Add(&physical.DataObjScan{})
			rangeAgg  = physicalGraph.Add(&physical.RangeAggregation{})
			vectorAgg = physicalGraph.Add(&physical.VectorAggregation{})
		)

		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: rangeAgg, Child: scan})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: vectorAgg, Child: rangeAgg})

		physicalPlan := physical.FromGraph(physicalGraph)

		graph, err := planWorkflow("", physicalPlan)
		require.NoError(t, err)
		require.Equal(t, 2, graph.Len())
		requireUniqueStreams(t, graph)
		generateConsistentULIDs(&ulidGen, graph)

		expectOuptut := strings.TrimSpace(`
Task 00000000000000000000000001
-------------------------------
VectorAggregation operation=invalid
    └── @source stream=00000000000000000000000003

Task 00000000000000000000000002
-------------------------------
RangeAggregation operation=invalid start=0001-01-01T00:00:00Z end=0001-01-01T00:00:00Z step=0s range=0s
│   └── @sink stream=00000000000000000000000003
└── DataObjScan location= streams=0 section_id=0 projections=()
`)

		actualOutput := Sprint(&Workflow{graph: graph})
		require.Equal(t, strings.TrimSpace(expectOuptut), strings.TrimSpace(actualOutput))
	})

	t.Run("split on parallelize", func(t *testing.T) {
		ulidGen := ulidGenerator{}

		var physicalGraph dag.Graph[physical.Node]

		var (
			vectorAgg = physicalGraph.Add(&physical.VectorAggregation{})
			rangeAgg  = physicalGraph.Add(&physical.RangeAggregation{})

			parallelize = physicalGraph.Add(&physical.Parallelize{})

			filter  = physicalGraph.Add(&physical.Filter{})
			parse   = physicalGraph.Add(&physical.ParseNode{Kind: physical.ParserLogfmt})
			scanSet = physicalGraph.Add(&physical.ScanSet{
				Targets: []*physical.ScanTarget{
					{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{Location: "a"}},
					{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{Location: "b"}},
					{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{Location: "c"}},
				},
			})
		)

		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: vectorAgg, Child: rangeAgg})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: rangeAgg, Child: parallelize})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: filter})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: filter, Child: parse})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: parse, Child: scanSet})

		physicalPlan := physical.FromGraph(physicalGraph)

		graph, err := planWorkflow("", physicalPlan)
		require.NoError(t, err)
		require.Equal(t, 5, graph.Len())
		requireUniqueStreams(t, graph)
		generateConsistentULIDs(&ulidGen, graph)

		expectOuptut := strings.TrimSpace(`
Task 00000000000000000000000001
-------------------------------
VectorAggregation operation=invalid
    └── @source stream=00000000000000000000000006

Task 00000000000000000000000002
-------------------------------
RangeAggregation operation=invalid start=0001-01-01T00:00:00Z end=0001-01-01T00:00:00Z step=0s range=0s
    ├── @source stream=00000000000000000000000007
    ├── @source stream=00000000000000000000000008
    ├── @source stream=00000000000000000000000009
    └── @sink stream=00000000000000000000000006

Task 00000000000000000000000003
-------------------------------
Filter
│   └── @sink stream=00000000000000000000000007
└── Parse kind=logfmt
    └── DataObjScan location=a streams=0 section_id=0 projections=()

Task 00000000000000000000000004
-------------------------------
Filter
│   └── @sink stream=00000000000000000000000008
└── Parse kind=logfmt
    └── DataObjScan location=b streams=0 section_id=0 projections=()

Task 00000000000000000000000005
-------------------------------
Filter
│   └── @sink stream=00000000000000000000000009
└── Parse kind=logfmt
    └── DataObjScan location=c streams=0 section_id=0 projections=()
`)

		actualOutput := Sprint(&Workflow{graph: graph})
		require.Equal(t, strings.TrimSpace(expectOuptut), strings.TrimSpace(actualOutput))
	})
}

// requireUniqueStreams asserts that for each stream found in g, that stream has
// exactly one reader (from Task.Sources) and exactly one writer (from
// Task.Sinks).
func requireUniqueStreams(t testing.TB, g dag.Graph[*Task]) {
	t.Helper()

	type streamInfo struct {
		Reader physical.Node
		Writer physical.Node
	}

	streamInfos := make(map[*Stream]*streamInfo)

	for _, root := range g.Roots() {
		_ = g.Walk(root, func(task *Task) error {
			for node, streams := range task.Sources {
				for _, stream := range streams {
					info, ok := streamInfos[stream]
					if !ok {
						info = new(streamInfo)
						streamInfos[stream] = info
					}

					require.Nil(t, info.Reader, "each stream must have exactly one reader")
					info.Reader = node
				}
			}

			for node, streams := range task.Sinks {
				for _, stream := range streams {
					info, ok := streamInfos[stream]
					if !ok {
						info = new(streamInfo)
						streamInfos[stream] = info
					}

					require.Nil(t, info.Writer, "each stream must have exactly one writer")
					info.Writer = node
				}
			}

			return nil
		}, dag.PreOrderWalk)
	}

	for _, info := range streamInfos {
		require.NotNil(t, info.Reader, "each stream must have exactly one reader")
		require.NotNil(t, info.Writer, "each stream must have exactly one writer")
	}
}

// generateConsistentULIDs reassigns ULIDs to all tasks and streams in the graph
// using the ULID generator for consistent output.
func generateConsistentULIDs(gen *ulidGenerator, g dag.Graph[*Task]) {
	for _, root := range g.Roots() {
		_ = g.Walk(root, func(task *Task) error {
			task.ULID = gen.Make()
			return nil
		}, dag.PreOrderWalk)
	}

	// For easier debugging, we do a second pass for all the streams. That way
	// we know stream ULIDs start after task ULIDs.
	for _, root := range g.Roots() {
		_ = g.Walk(root, func(task *Task) error {
			for _, streams := range task.Sources {
				for _, stream := range streams {
					stream.ULID = gen.Make()
				}
			}
			return nil
		}, dag.PreOrderWalk)
	}
}

type ulidGenerator struct {
	lastCounter uint64
}

func (g *ulidGenerator) Make() ulid.ULID {
	g.lastCounter++
	value := g.lastCounter

	// Manually set ULID bytes - counter in the last 8 bytes, zeros in first 8
	var ulidBytes [16]byte
	// Put the counter value in the last 8 bytes (big-endian)
	for i := range 8 {
		ulidBytes[15-i] = byte(value >> (8 * i))
	}
	return ulid.ULID(ulidBytes)
}
