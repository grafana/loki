package scheduler

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

func TestLogAssignment(t *testing.T) {
	window := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	var g dag.Graph[physical.Node]
	g.Add(&physical.LogMerge{
		NodeID:         ulid.Make(),
		Tenant:         "tenant-a",
		ToCWindowStart: window.UnixNano(),
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "a"}, {ObjectPath: "b"}}},
		},
		InputBytes: 2048,
	})
	task := &workflow.Task{ULID: ulid.Make(), TenantID: "tenant-a", Fragment: physical.FromGraph(g)}
	addr := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 9095}

	var buf bytes.Buffer
	logAssignment(log.NewLogfmtLogger(&buf), task, addr)

	require.Equal(t,
		"level=info msg=\"assigning compaction task\" task_id="+task.ULID.String()+
			" tenant=tenant-a worker=10.0.0.1:9095 node_type=LogMerge window=2024-05-01T12:00:00Z input_objects=2 input_bytes=2048\n",
		buf.String(),
	)
}

func TestLogAssignment_NilFragment(t *testing.T) {
	task := &workflow.Task{ULID: ulid.Make(), TenantID: "tenant-a"}

	var buf bytes.Buffer
	logAssignment(log.NewLogfmtLogger(&buf), task, &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 9095})

	require.Equal(t,
		"level=info msg=\"assigning compaction task\" task_id="+task.ULID.String()+" tenant=tenant-a worker=10.0.0.1:9095\n",
		buf.String(),
	)
}
