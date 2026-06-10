package compactor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

func TestBuildIndexMergePlan_SingleRoot(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	task := &compactionv2pb.TaskSpec{
		Tenant: "t1",
		Runs: []compactionv2pb.RunRef{
			{Sections: []compactionv2pb.SectionRef{{ObjectPath: "i0", SectionIndex: 0}}},
			{Sections: []compactionv2pb.SectionRef{{ObjectPath: "i1", SectionIndex: 0}}},
		},
	}
	output := "indexes/tenants/t1/aa/aaaa"

	plan := buildIndexMergePlan("t1", window, task, output, 10*time.Minute)

	root, err := plan.Root()
	require.NoError(t, err, "plan must have exactly one root node")

	node, ok := root.(*physical.IndexMerge)
	require.True(t, ok, "root is %T, want *physical.IndexMerge", root)
	require.Equal(t, "t1", node.Tenant)
	require.Equal(t, window.UnixNano(), node.ToCWindowStart)
	require.Equal(t, output, node.OutputIndexPath)
	require.Equal(t, 10*time.Minute, node.TaskTTL)
	require.Equal(t, task.Runs, node.Runs)
}

// TestBuildIndexMergePlan_AssignsFreshNodeID guards against a stale-ID
// bug where two builds of the same task accidentally share a NodeID. The
// workflow framework keys task tracking off the NodeID; collisions would
// cause manifest registration to fail with "stream/task already registered".
func TestBuildIndexMergePlan_AssignsFreshNodeID(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	task := &compactionv2pb.TaskSpec{
		Tenant: "t1",
		Runs:   []compactionv2pb.RunRef{{Sections: []compactionv2pb.SectionRef{{ObjectPath: "i0", SectionIndex: 0}}}},
	}

	p1 := buildIndexMergePlan("t1", window, task, "out1", time.Minute)
	p2 := buildIndexMergePlan("t1", window, task, "out2", time.Minute)

	n1, err := p1.Root()
	require.NoError(t, err)
	n2, err := p2.Root()
	require.NoError(t, err)

	require.NotEqual(t, n1.ID(), n2.ID(), "every build must mint a fresh NodeID")
}
