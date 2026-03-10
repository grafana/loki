package worker

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// mockRecordSink records each Send call and total rows for testing.
// RecordedFieldNames is the list of field names (in order) for each received record.
type mockRecordSink struct {
	mu                 sync.Mutex
	SendCount          int
	TotalRows          int64
	RecordedFieldNames [][]string
}

func (m *mockRecordSink) Send(_ context.Context, rec arrow.RecordBatch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SendCount++
	m.TotalRows += rec.NumRows()
	if rec != nil && rec.Schema() != nil {
		n := rec.Schema().NumFields()
		names := make([]string, n)
		for i := 0; i < n; i++ {
			names[i] = rec.Schema().Field(i).Name
		}
		m.RecordedFieldNames = append(m.RecordedFieldNames, names)
	}
	return nil
}

// recordInput pairs a schema with rows to build one record for pipeline tests.
type recordInput struct {
	Schema *arrow.Schema
	Rows   arrowtest.Rows
}

func TestThread_drainPipeline(t *testing.T) {
	schemaAB := arrow.NewSchema([]arrow.Field{
		{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "b", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	tests := []struct {
		name             string
		batchSizeRecords int64
		inputs           []recordInput
		wantTotalRows    int
		checkSends       func(t *testing.T, sendCount int, totalRows int64)
		checkFieldNames  func(t *testing.T, fieldNames [][]string)
	}{
		{
			name:             "batchSizeRecords=0 sink receives one send per record",
			batchSizeRecords: 0,
			inputs: []recordInput{
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(1), "b": "x"}}},
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(2), "b": "x"}}},
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(3), "b": "x"}}},
			},
			wantTotalRows: 3,
			checkSends: func(t *testing.T, sendCount int, totalRows int64) {
				require.Equal(t, 3, sendCount, "sink should receive one send per record when batching is disabled")
				require.Equal(t, int64(3), totalRows)
			},
		},
		{
			name:             "batchSizeRecords=2 fits exactly two records per batch",
			batchSizeRecords: 2,
			inputs: []recordInput{
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(1), "b": "x"}}},
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(2), "b": "x"}}},
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(3), "b": "x"}}},
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(4), "b": "x"}}},
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(5), "b": "x"}}},
			},
			wantTotalRows: 5,
			checkSends: func(t *testing.T, sendCount int, totalRows int64) {
				require.Equal(t, 3, sendCount, "5 records with 2 per batch => 2+2+1 sends")
				require.Equal(t, int64(5), totalRows, "all rows must be received regardless of batching")
			},
		},
		{
			name:             "batchSizeRecords=1 each record sent alone",
			batchSizeRecords: 1,
			inputs: []recordInput{
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(1), "b": "x"}}},
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(2), "b": "x"}}},
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(3), "b": "x"}}},
			},
			wantTotalRows: 3,
			checkSends: func(t *testing.T, sendCount int, totalRows int64) {
				require.Equal(t, 3, sendCount, "each record in its own batch so each is sent alone")
				require.Equal(t, int64(3), totalRows)
			},
		},
		{
			name:             "records with different schemas are batched with union schema",
			batchSizeRecords: 10, // large enough to batch both records
			inputs: []recordInput{
				{
					arrow.NewSchema([]arrow.Field{
						{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
						{Name: "b", Type: arrow.BinaryTypes.String, Nullable: true},
					}, nil),
					arrowtest.Rows{map[string]any{"a": int64(1), "b": "x"}},
				},
				{
					arrow.NewSchema([]arrow.Field{
						{Name: "b", Type: arrow.BinaryTypes.String, Nullable: true},
						{Name: "c", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
					}, nil),
					arrowtest.Rows{map[string]any{"b": "y", "c": int64(2)}},
				},
			},
			wantTotalRows: 2,
			checkSends: func(t *testing.T, sendCount int, totalRows int64) {
				require.Equal(t, 1, sendCount, "one batched send with schema reconciliation")
				require.Equal(t, int64(2), totalRows)
			},
			checkFieldNames: func(t *testing.T, fieldNames [][]string) {
				require.Len(t, fieldNames, 1, "one record received")
				require.Equal(t, []string{"a", "b", "c"}, fieldNames[0], "union schema order first seen: a,b from first record then c from second")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			records := make([]arrow.RecordBatch, len(tt.inputs))
			for i, in := range tt.inputs {
				records[i] = in.Rows.Record(memory.DefaultAllocator, in.Schema)
			}
			pipeline := executor.NewBufferedPipeline(records...)
			defer pipeline.Close()

			sink := &mockRecordSink{}
			th := &thread{Logger: log.NewNopLogger()}
			totalRows, err := th.drainPipeline(ctx, pipeline, []recordSink{sink}, tt.batchSizeRecords, log.NewNopLogger())
			require.NoError(t, err)
			require.Equal(t, tt.wantTotalRows, totalRows)

			sink.mu.Lock()
			sendCount := sink.SendCount
			totalRowsReceived := sink.TotalRows
			fieldNames := append([][]string(nil), sink.RecordedFieldNames...)
			sink.mu.Unlock()

			tt.checkSends(t, sendCount, totalRowsReceived)
			if tt.checkFieldNames != nil {
				tt.checkFieldNames(t, fieldNames)
			}
		})
	}
}

func TestThread_runJob_IgnoresClosedSourceBindErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		peers := newTestPeerPair(t)

		limitNode := &physical.Limit{NodeID: ulid.Make(), Fetch: 1}

		var graph dag.Graph[physical.Node]
		graph.Add(limitNode)

		var (
			closedStream = &workflow.Stream{ULID: ulid.Make(), TenantID: "test-tenant"}
			openStream   = &workflow.Stream{ULID: ulid.Make(), TenantID: "test-tenant"}

			closedSource = &streamSource{}
			openSource   = &streamSource{}
		)

		closedSource.Close()

		go func() {
			rows := arrowtest.Rows{map[string]any{"a": int64(1), "b": "x"}}
			record := rows.Record(memory.DefaultAllocator, rows.Schema())
			_ = openSource.Write(t.Context(), record)
			openSource.Close()
		}()

		job := &threadJob{
			Context:   t.Context(),
			Scheduler: peers.workerPeer,
			Task: &workflow.Task{
				ULID:     ulid.Make(),
				TenantID: "test-tenant",
				Fragment: physical.FromGraph(graph),
				Sources: map[physical.Node][]*workflow.Stream{
					limitNode: {closedStream, openStream},
				},
				Sinks: map[physical.Node][]*workflow.Stream{},
			},
			Sources: map[ulid.ULID]*streamSource{
				closedStream.ULID: closedSource,
				openStream.ULID:   openSource,
			},
			Sinks: map[ulid.ULID]*streamSink{},
			Close: func() {},
		}

		th := &thread{
			Logger:  log.NewNopLogger(),
			Metrics: newMetrics(),
		}
		th.runJob(t.Context(), job)

		finalState := peers.waitForTerminalTaskState(t, 2*time.Second)
		require.Equal(t, workflow.TaskStateCompleted, finalState, "expected task to complete even with one closed source")
	})
}

type testPeerPair struct {
	workerPeer *wire.Peer
	taskStates chan workflow.TaskState
}

func newTestPeerPair(t *testing.T) *testPeerPair {
	t.Helper()

	listener := &wire.Local{Address: wire.LocalScheduler}

	var (
		acceptedConn = make(chan wire.Conn, 1)
		acceptErr    = make(chan error, 1)
	)
	go func() {
		conn, err := listener.Accept(t.Context())
		if err != nil {
			acceptErr <- err
			return
		}
		acceptedConn <- conn
	}()

	workerConn, err := listener.DialFrom(t.Context(), wire.LocalWorker)
	require.NoError(t, err)

	var schedulerConn wire.Conn
	select {
	case err := <-acceptErr:
		require.NoError(t, err)
	case schedulerConn = <-acceptedConn:
	}

	pair := &testPeerPair{taskStates: make(chan workflow.TaskState, 10)}

	schedulerPeer := &wire.Peer{
		Logger:  log.NewNopLogger(),
		Metrics: wire.NewMetrics(),
		Conn:    schedulerConn,
		Handler: func(_ context.Context, _ *wire.Peer, message wire.Message) error {
			if status, ok := message.(wire.TaskStatusMessage); ok {
				pair.taskStates <- status.Status.State
			}
			return nil
		},
	}
	pair.workerPeer = &wire.Peer{
		Logger:  log.NewNopLogger(),
		Metrics: wire.NewMetrics(),
		Conn:    workerConn,
		Handler: func(_ context.Context, _ *wire.Peer, _ wire.Message) error { return nil },
	}

	serveCtx, cancelServe := context.WithCancel(context.Background())
	t.Cleanup(cancelServe)
	t.Cleanup(func() { _ = listener.Close(context.Background()) })

	go func() { _ = schedulerPeer.Serve(serveCtx) }()
	go func() { _ = pair.workerPeer.Serve(serveCtx) }()

	return pair
}

func (p *testPeerPair) waitForTerminalTaskState(t *testing.T, timeout time.Duration) workflow.TaskState {
	t.Helper()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case state := <-p.taskStates:
			if state == workflow.TaskStateCompleted || state == workflow.TaskStateFailed {
				return state
			}
		case <-timer.C:
			t.Fatal("timed out waiting for terminal task state")
		}
	}
}
