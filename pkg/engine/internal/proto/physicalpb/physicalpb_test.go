package physicalpb_test

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// Test performs a basic end-to-end test ensuring that we can convert from a
// [physical.Plan] into a [Plan] and back.
func Test(t *testing.T) {
	var (
		parent = &physical.IndexFilter{NodeID: ulid.Make(), Tenant: "tenant-a", SourceIndexPaths: []string{"index/0"}, ObjectPaths: []string{"objects/0"}}
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
	keySpan, err := postings.NewKeySpan(
		&postings.Key{Kind: postings.KindLabel, ColumnName: "app", LabelValue: "api"},
		&postings.Key{Kind: postings.KindLabel, ColumnName: "env"},
	)
	require.NoError(t, err)
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
		KeySpan: keySpan,
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
	require.Equal(t, want.KeySpan, got.KeySpan)
}

func TestIndexMerge_BoundedRuns(t *testing.T) {
	runs := []*compactionv2pb.RunRef{{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "index/0"}}}}
	end := postings.Key{Kind: postings.KindLabel, ColumnName: "env"}
	keySpan, err := postings.NewKeySpan(nil, &end)
	require.NoError(t, err)
	encodedStart, encodedEnd := keySpan.Encode()

	t.Run("holds the runs of a merge with a key span in bounded_runs so an older worker finds no runs", func(t *testing.T) {
		var pb physicalpb.IndexMerge
		require.NoError(t, pb.UnmarshalPhysical(&physical.IndexMerge{NodeID: ulid.Make(), Tenant: "acme", Runs: runs, KeySpan: keySpan}))
		require.Empty(t, pb.Runs)
		require.Equal(t, runs, pb.BoundedRuns)
	})

	t.Run("holds the runs of a merge over every key in runs", func(t *testing.T) {
		var pb physicalpb.IndexMerge
		require.NoError(t, pb.UnmarshalPhysical(&physical.IndexMerge{NodeID: ulid.Make(), Tenant: "acme", Runs: runs}))
		require.Equal(t, runs, pb.Runs)
		require.Empty(t, pb.BoundedRuns)
	})

	t.Run("returns an error when a merge with a key span holds runs in runs", func(t *testing.T) {
		pb := physicalpb.IndexMerge{Tenant: "acme", Runs: runs, StartPostingKey: encodedStart, EndPostingKey: encodedEnd}
		_, err := pb.MarshalPhysical(ulid.Make())
		require.ErrorContains(t, err, "holds runs in runs instead of bounded_runs")
	})

	t.Run("returns an error when a merge without a key span holds runs in bounded_runs", func(t *testing.T) {
		pb := physicalpb.IndexMerge{Tenant: "acme", BoundedRuns: runs}
		_, err := pb.MarshalPhysical(ulid.Make())
		require.ErrorContains(t, err, "holds runs in bounded_runs")
	})

	t.Run("keeps the key span without an end and the stats sources of the last task of a split", func(t *testing.T) {
		start := postings.Key{Kind: postings.KindLabel, ColumnName: "env"}
		fromEnv, err := postings.NewKeySpan(&start, nil)
		require.NoError(t, err)
		var pb physicalpb.IndexMerge
		require.NoError(t, pb.UnmarshalPhysical(&physical.IndexMerge{NodeID: ulid.Make(), Tenant: "acme", Runs: runs, KeySpan: fromEnv, StatsSources: []string{"index/0"}}))
		node, err := pb.MarshalPhysical(ulid.Make())
		require.NoError(t, err)
		got, ok := node.(*physical.IndexMerge)
		require.True(t, ok, "node is %T", node)
		require.Equal(t, fromEnv, got.KeySpan)
		require.Equal(t, runs, got.Runs)
		require.Equal(t, []string{"index/0"}, got.StatsSources)
	})

	t.Run("returns an error when a merge without a key span has stats sources", func(t *testing.T) {
		var pb physicalpb.IndexMerge
		require.ErrorContains(t, pb.UnmarshalPhysical(&physical.IndexMerge{NodeID: ulid.Make(), Tenant: "acme", Runs: runs, StatsSources: []string{"index/0"}}), "has stats sources")

		_, err := (&physicalpb.IndexMerge{Tenant: "acme", Runs: runs, StatsSourcePaths: []string{"index/0"}}).MarshalPhysical(ulid.Make())
		require.ErrorContains(t, err, "has stats source paths")
	})

	t.Run("returns an error when a posting key bound does not decode", func(t *testing.T) {
		pb := physicalpb.IndexMerge{Tenant: "acme", StartPostingKey: []byte{0xff}, BoundedRuns: runs}
		_, err := pb.MarshalPhysical(ulid.Make())
		require.ErrorContains(t, err, "IndexMerge key span")
	})
}

func TestIndexFilter_RoundTrip(t *testing.T) {
	t.Run("keeps every source path of an index run in order", func(t *testing.T) {
		want := &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme",
			SourceIndexPaths: []string{"index/b", "index/a"}, ObjectPaths: []string{"logs/a"},
		}
		var pb physicalpb.IndexFilter
		require.NoError(t, pb.UnmarshalPhysical(want))

		got, err := pb.MarshalPhysical(want.NodeID)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
}
