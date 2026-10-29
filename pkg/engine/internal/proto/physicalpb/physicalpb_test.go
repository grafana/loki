package physicalpb_test

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// Test performs a basic end-to-end test ensuring that we can convert from a
// [physical.Plan] into a [Plan] and back.
func Test(t *testing.T) {
	var (
		parent = &physical.IndexFilter{NodeID: ulid.Make(), Tenant: "tenant-a", SourceIndexPath: "index/0", ObjectPaths: []string{"objects/0"}}
		child  = &physical.IndexMerge{NodeID: ulid.Make(), Tenant: "tenant-a", ToCWindowStart: 1_700_000_000_000_000_000}
	)

	var graph dag.Graph[physical.Node]
	graph.Add(parent)
	graph.Add(child)
	require.NoError(t, graph.AddEdge(dag.Edge[physical.Node]{Parent: parent, Child: child}), "Failed to add edge")
	expectedPlan := physical.FromGraph(graph)

	var protoPlan physicalpb.Plan
	require.NoError(t, protoPlan.UnmarshalPhysical(expectedPlan), "Failed to unmarshal physical plan")

	actualPlan, err := protoPlan.MarshalPhysical()
	require.NoError(t, err, "Failed to marshal protobuf plan")

	root, err := actualPlan.Root()
	require.NoError(t, err)
	require.Equal(t, parent, root)
	children := actualPlan.Children(root)
	require.Len(t, children, 1)
	require.Equal(t, child.ID(), children[0].ID())
}

// Test_Node_IndexMerge_RoundTrip asserts on the concrete fields of IndexMerge
// after a protobuf round-trip.
func Test_Node_IndexMerge_RoundTrip(t *testing.T) {
	want := &physical.IndexMerge{
		NodeID: ulid.Make(),

		Tenant:         "tenant-a",
		ToCWindowStart: 1_700_000_000_000_000_000,
		Runs: []*compactionv2pb.RunRef{
			{
				Sections: []*compactionv2pb.SectionRef{
					{
						ObjectPath:   "s3://bucket/index/0",
						SectionIndex: 1,
						MinKey:       []string{"a", "b"},
						MaxKey:       []string{"c", "d"},
						MinTimestamp: 100,
						MaxTimestamp: 200,
					},
					{
						ObjectPath:   "s3://bucket/index/1",
						SectionIndex: 2,
						MinKey:       []string{"e"},
						MaxKey:       []string{"f"},
						MinTimestamp: 300,
						MaxTimestamp: 400,
					},
				},
			},
			{
				Sections: []*compactionv2pb.SectionRef{
					{
						ObjectPath:   "s3://bucket/index/2",
						SectionIndex: 3,
						MinKey:       []string{"g"},
						MaxKey:       []string{"h"},
						MinTimestamp: 500,
						MaxTimestamp: 600,
					},
				},
			},
		},
	}

	var graph dag.Graph[physical.Node]
	graph.Add(want)
	expectedPlan := physical.FromGraph(graph)

	var protoPlan physicalpb.Plan
	require.NoError(t, protoPlan.UnmarshalPhysical(expectedPlan), "Failed to unmarshal physical plan")

	actualPlan, err := protoPlan.MarshalPhysical()
	require.NoError(t, err, "Failed to marshal protobuf plan")

	root, err := actualPlan.Root()
	require.NoError(t, err)

	got, ok := root.(*physical.IndexMerge)
	require.True(t, ok, "expected *physical.IndexMerge, got %T", root)

	require.Equal(t, want.NodeID, got.NodeID)
	require.Equal(t, want.Tenant, got.Tenant)
	require.Equal(t, want.ToCWindowStart, got.ToCWindowStart)
	require.Equal(t, want.Runs, got.Runs)
}
