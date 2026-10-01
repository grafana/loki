package compactor

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

func TestReadCompactionResult(t *testing.T) {
	readErr := errors.New("worker failed after upload")
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	want := v2.ResultArtifact{Path: "indexes/result"}
	resultRecord := func() arrow.RecordBatch {
		rec, err := want.ToRecordBatch(mem)
		require.NoError(t, err)
		return rec
	}
	emptyRecord := func() arrow.RecordBatch {
		builder := array.NewRecordBuilder(mem, v2.ResultRecordSchema)
		defer builder.Release()
		return builder.NewRecordBatch()
	}
	for _, tc := range []struct {
		name    string
		records []arrow.RecordBatch
		openErr error
		readErr error
		wantErr string
	}{
		{name: "success", records: []arrow.RecordBatch{resultRecord()}},
		{name: "missing", wantErr: "produced no result record"},
		{name: "nil record", records: []arrow.RecordBatch{nil}, wantErr: "missing record"},
		{name: "empty batch", records: []arrow.RecordBatch{emptyRecord()}, wantErr: "got 0 rows, want 1"},
		{name: "multiple records", records: []arrow.RecordBatch{resultRecord(), resultRecord()}, wantErr: "more than one result record"},
		{name: "open failure", openErr: readErr, wantErr: "pipeline.Open"},
		{name: "read failure", readErr: readErr, wantErr: "pipeline.Read"},
		{name: "result then failure", records: []arrow.RecordBatch{resultRecord()}, readErr: readErr, wantErr: "pipeline.Read"},
		{name: "result then cancellation", records: []arrow.RecordBatch{resultRecord()}, readErr: context.Canceled, wantErr: "context canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pipeline := &resultTestPipeline{records: tc.records, openErr: tc.openErr, readErr: tc.readErr}
			got, err := readCompactionResult(context.Background(), pipeline)
			require.True(t, pipeline.closed)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.Nil(t, got, "failed tasks must not return a usable artifact")
				if tc.readErr != nil {
					require.ErrorIs(t, err, tc.readErr)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, &want, got)
			require.Equal(t, 2, pipeline.reads, "a result alone is not successful completion")
		})
	}
}

// resultTestPipeline transfers ownership of each returned record to the reader.
type resultTestPipeline struct {
	records []arrow.RecordBatch
	openErr error
	readErr error
	reads   int
	closed  bool
}

func (p *resultTestPipeline) Open(context.Context) error { return p.openErr }

func (p *resultTestPipeline) Read(context.Context) (arrow.RecordBatch, error) {
	p.reads++
	if len(p.records) > 0 {
		rec := p.records[0]
		p.records = p.records[1:]
		return rec, nil
	}
	if p.readErr != nil {
		return nil, p.readErr
	}
	return nil, executor.EOF
}

func (p *resultTestPipeline) Close() {
	p.closed = true
	for _, rec := range p.records {
		if rec != nil {
			rec.Release()
		}
	}
	p.records = nil
}

func TestReadCompactionResultCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pipeline := &resultTestPipeline{readErr: io.EOF}
	got, err := readCompactionResult(ctx, pipeline)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
	require.True(t, pipeline.closed)
}

func TestBuildIndexMergePlan_SingleRoot(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	task := &compactionv2pb.TaskSpec{
		Tenant: "t1",
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "i0", SectionIndex: 0}}},
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "i1", SectionIndex: 0}}},
		},
	}

	plan := buildIndexMergePlan("t1", window, task)

	root, err := plan.Root()
	require.NoError(t, err, "plan must have exactly one root node")

	node, ok := root.(*physical.IndexMerge)
	require.True(t, ok, "root is %T, want *physical.IndexMerge", root)
	require.Equal(t, "t1", node.Tenant)
	require.Equal(t, window.UnixNano(), node.ToCWindowStart)
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
		Runs:   []*compactionv2pb.RunRef{{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "i0", SectionIndex: 0}}}},
	}

	p1 := buildIndexMergePlan("t1", window, task)
	p2 := buildIndexMergePlan("t1", window, task)

	n1, err := p1.Root()
	require.NoError(t, err)
	n2, err := p2.Root()
	require.NoError(t, err)

	require.NotEqual(t, n1.ID(), n2.ID(), "every build must mint a fresh NodeID")
}

func TestBuildLogMergePlan(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	task := &compactionv2pb.TaskSpec{
		Tenant:     "t1",
		SortSchema: []string{"label:service_name"},
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "logs/log-0", SectionIndex: 0, MinKey: []string{"auth"}}}},
		},
	}

	plan := buildLogMergePlan("t1", window, task)

	root, err := plan.Root()
	require.NoError(t, err, "plan must have exactly one root node")

	node, ok := root.(*physical.LogMerge)
	require.True(t, ok, "root is %T, want *physical.LogMerge", root)
	require.Equal(t, "t1", node.Tenant)
	require.Equal(t, window.UnixNano(), node.ToCWindowStart)
	require.Equal(t, task.Runs, node.Runs)
	require.Equal(t, task.SortSchema, node.SortSchema)

	task2 := &compactionv2pb.TaskSpec{
		Tenant: "t1",
		Runs:   []*compactionv2pb.RunRef{{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "obj2"}}}},
	}
	p2 := buildLogMergePlan("t1", window, task2)
	p2Root, err := p2.Root()
	require.NoError(t, err, "plan must have exactly one root node")

	require.NotEqual(t, root.ID(), p2Root.ID(), "every build must mint a fresh NodeID")
}

func TestBuildSortObjectPlan(t *testing.T) {
	plan := buildSortObjectPlan("objects/source", []string{"label:app"})
	root, err := plan.Root()
	require.NoError(t, err)

	node, ok := root.(*physical.SortObject)
	require.True(t, ok, "root is %T, want *physical.SortObject", root)
	require.Equal(t, "objects/source", node.SourceObjectPath)
	require.Equal(t, []string{"label:app"}, node.SortSchema)

	second, err := buildSortObjectPlan("objects/source", []string{"label:app"}).Root()
	require.NoError(t, err)
	require.NotEqual(t, root.ID(), second.ID(), "every build must mint a fresh NodeID")
}
