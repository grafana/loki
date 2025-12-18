package worker_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/objtest"
	"github.com/grafana/loki/v3/pkg/engine/internal/worker"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// Test runs an end-to-end test of the worker.
func Test(t *testing.T) {
	builder := objtest.NewBuilder(t)

	logger := log.NewNopLogger()
	if testing.Verbose() {
		logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	}

	net := newTestNetwork()
	sched := newTestScheduler(t, logger, net)
	_ = newTestWorker(t, logger, builder.Location(), net)

	ctx := user.InjectOrgID(t.Context(), objtest.Tenant)

	builder.Append(ctx, logproto.Stream{
		Labels: `{app="loki", env="dev"}`,
		Entries: []logproto.Entry{{
			Timestamp: time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			Line:      "Hello, world!",
		}, {
			Timestamp: time.Date(2025, time.January, 1, 0, 0, 1, 0, time.UTC),
			Line:      "Goodbye, world!",
		}},
	})
	builder.Close()

	params, err := logql.NewLiteralParams(
		`{app="loki"}`,
		time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, time.January, 2, 0, 0, 0, 0, time.UTC),
		0,
		0,
		logproto.BACKWARD,
		1000,
		[]string{"0_of_1"},
		nil,
	)
	require.NoError(t, err, "expected to be able to create literal LogQL params")

	wf := buildWorkflow(ctx, t, logger, builder.Location(), sched, params)
	pipeline, err := wf.Run(ctx)
	require.NoError(t, err)

	expected := arrowtest.Rows{
		{
			"timestamp_ns.builtin.timestamp": time.Date(2025, time.January, 1, 0, 0, 1, 0, time.UTC),
			"utf8.label.app":                 "loki",
			"utf8.label.env":                 "dev",
			"utf8.builtin.message":           "Goodbye, world!",
		},
		{
			"timestamp_ns.builtin.timestamp": time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			"utf8.label.app":                 "loki",
			"utf8.label.env":                 "dev",
			"utf8.builtin.message":           "Hello, world!",
		},
	}

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, readTable(ctx, t, pipeline))
	require.NoError(t, err, "failed to get rows from table")
	require.Equal(t, expected, actual)
}

// TestWorkerGracefulShutdown tests that the worker gracefully shuts down by
// finishing execution of a task even after its context is canceled. The test
// creates a TopK node job that accepts a Stream, waits for the worker to start
// processing, cancels the worker's context, then sends data to the stream and
// verifies the job completes.
func TestWorkerGracefulShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := log.NewNopLogger()
		if testing.Verbose() {
			logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
		}

		net := newTestNetwork()
		sched := newTestScheduler(t, logger, net)

		// Create a cancelable context for the worker's run() method
		runCtx, cancel := context.WithCancel(context.Background())
		defer cancel()

		_ = newTestWorkerWithContext(t, logger, objtest.Location{}, net, runCtx)

		ctx := user.InjectOrgID(context.Background(), objtest.Tenant)

		// Create a physical plan with a TopK node
		topkNode := &physical.TopK{
			NodeID:    ulid.Make(),
			SortBy:    &physical.ColumnExpr{Ref: semconv.ColumnIdentTimestamp.ColumnRef()},
			Ascending: false,
			K:         10,
		}

		planGraph := &dag.Graph[physical.Node]{}
		planGraph.Add(topkNode)
		plan := physical.FromGraph(*planGraph)

		// Create a stream that will feed data to the TopK node
		inputStream := &workflow.Stream{
			ULID:     ulid.Make(),
			TenantID: objtest.Tenant,
		}

		// Create a workflow task manually with the TopK node and stream source
		task := &workflow.Task{
			ULID:     ulid.Make(),
			TenantID: objtest.Tenant,
			Fragment: plan,
			Sources: map[physical.Node][]*workflow.Stream{
				topkNode: {inputStream},
			},
			Sinks: make(map[physical.Node][]*workflow.Stream),
		}

		// Create a results stream for the workflow output
		resultsStream := &workflow.Stream{
			ULID:     ulid.Make(),
			TenantID: objtest.Tenant,
		}
		task.Sinks[topkNode] = []*workflow.Stream{resultsStream}

		// Create a workflow with the task
		manifest := &workflow.Manifest{
			Streams: []*workflow.Stream{inputStream, resultsStream},
			Tasks:   []*workflow.Task{task},
			TaskEventHandler: func(_ context.Context, _ *workflow.Task, _ workflow.TaskStatus) {
				// Empty
			},
			StreamEventHandler: func(_ context.Context, _ *workflow.Stream, _ workflow.StreamState) {
				// Empty
			},
		}
		require.NoError(t, sched.RegisterManifest(ctx, manifest))

		// Create a simple record writer to receive results
		resultsWriter := &testRecordWriter{records: make(chan arrow.RecordBatch, 10)}
		require.NoError(t, sched.Listen(ctx, resultsWriter, resultsStream))

		// Start the task - this will assign it to the worker
		require.NoError(t, sched.Start(ctx, task))

		// Wait for the task to be assigned to the worker and start waiting for input
		synctest.Wait()

		// Now send data to the input stream
		// The worker should process this data even though its context will be canceled
		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, false),
			semconv.FieldFromIdent(semconv.ColumnIdentMessage, false),
		}, nil)

		timestampBuilder := array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))
		messageBuilder := array.NewStringBuilder(memory.DefaultAllocator)

		// Create test data
		for i := 0; i < 5; i++ {
			timestampBuilder.Append(arrow.Timestamp(time.Date(2025, time.January, 1, 0, 0, i, 0, time.UTC).UnixNano()))
			messageBuilder.Append(fmt.Sprintf("Message %d", i))
		}

		timestampArr := timestampBuilder.NewArray()
		messageArr := messageBuilder.NewArray()
		record := array.NewRecordBatch(schema, []arrow.Array{timestampArr, messageArr}, 5)

		// Send data for the stream to the worker
		// We need to connect to the worker 1 on behalf of worker 2 and send a StreamDataMessage
		workerConn, err := net.workerListener.DialFrom(ctx, wire.LocalWorker)
		require.NoError(t, err)
		defer workerConn.Close()

		workerPeer := &wire.Peer{
			Logger: logger,
			Conn:   workerConn,
			Handler: func(_ context.Context, _ *wire.Peer, _ wire.Message) error {
				return nil
			},
		}
		go func() { _ = workerPeer.Serve(ctx) }()

		// Cancel the worker's context to trigger graceful shutdown
		// This simulates the worker receiving a shutdown signal in Worker.run().
		// The earliest we can cancel is after worker1Listener is dialed to, otherwise
		// the connection will not be accepted.
		cancel()

		// Connect to the scheduler on behalf of worker 2
		schedulerConn, err := net.schedulerListener.DialFrom(ctx, testAddr("worker2"))
		require.NoError(t, err)
		defer schedulerConn.Close()

		schedulerPeer := &wire.Peer{
			Logger: logger,
			Conn:   schedulerConn,
			Handler: func(_ context.Context, _ *wire.Peer, _ wire.Message) error {
				return nil
			},
		}
		go func() { _ = schedulerPeer.Serve(ctx) }()

		// Say hello to the scheduler on behalf of worker 2
		err = schedulerPeer.SendMessage(ctx, wire.WorkerHelloMessage{
			Threads: 1,
		})
		require.NoError(t, err)

		synctest.Wait()

		// Send the data message
		err = workerPeer.SendMessage(ctx, wire.StreamDataMessage{
			StreamID: inputStream.ULID,
			Data:     record,
		})
		require.NoError(t, err)

		// Close the stream to signal EOF by sending a StreamStatusMessage
		err = schedulerPeer.SendMessage(ctx, wire.StreamStatusMessage{
			StreamID: inputStream.ULID,
			State:    workflow.StreamStateClosed,
		})
		require.NoError(t, err)

		// Wait for results - the worker should have processed the data
		// even though its context was canceled
		resultCtx, resultCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer resultCancel()

		select {
		case resultRecord := <-resultsWriter.records:
			require.Greater(t, resultRecord.NumRows(), int64(0), "should have at least some rows")
		case <-resultCtx.Done():
			t.Fatal("did not receive result within timeout")
		}

		synctest.Wait()
	})
}

// testRecordWriter implements workflow.RecordWriter for testing
type testRecordWriter struct {
	records chan arrow.RecordBatch
}

func (w *testRecordWriter) Write(ctx context.Context, record arrow.RecordBatch) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case w.records <- record:
		return nil
	}
}

type testNetwork struct {
	schedulerListener *wire.Local
	workerListener    *wire.Local
	dialer            wire.Dialer
}

func newTestNetwork() *testNetwork {
	schedulerListener := &wire.Local{Address: wire.LocalScheduler}
	workerListener := &wire.Local{Address: wire.LocalWorker}

	return &testNetwork{
		schedulerListener: schedulerListener,
		workerListener:    workerListener,
		dialer:            wire.NewLocalDialer(schedulerListener, workerListener),
	}
}

func newTestScheduler(t *testing.T, logger log.Logger, net *testNetwork) *scheduler.Scheduler {
	t.Helper()

	sched, err := scheduler.New(scheduler.Config{
		Logger:   logger,
		Listener: net.schedulerListener,
	})
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(t.Context(), sched.Service()))

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		require.NoError(t, services.StopAndAwaitTerminated(ctx, sched.Service()))
	})

	return sched
}

func newTestWorker(t *testing.T, logger log.Logger, loc objtest.Location, net *testNetwork) *worker.Worker {
	return newTestWorkerWithContext(t, logger, loc, net, t.Context())
}

//nolint:revive
func newTestWorkerWithContext(t *testing.T, logger log.Logger, loc objtest.Location, net *testNetwork, runCtx context.Context) *worker.Worker {
	t.Helper()

	w, err := worker.New(worker.Config{
		Logger:    logger,
		Bucket:    loc.Bucket,
		BatchSize: 2048,

		Dialer:           net.dialer,
		Listener:         net.workerListener,
		SchedulerAddress: wire.LocalScheduler,

		// Create enough threads to guarantee all tasks can be scheduled without
		// blocking.
		NumThreads: 2,
	})
	require.NoError(t, err, "expected to create worker")
	require.NoError(t, services.StartAndAwaitRunning(runCtx, w.Service()))

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		require.NoError(t, services.StopAndAwaitTerminated(ctx, w.Service()))
	})

	return w
}

func buildWorkflow(ctx context.Context, t *testing.T, logger log.Logger, loc objtest.Location, sched *scheduler.Scheduler, params logql.Params) *workflow.Workflow {
	logicalPlan, err := logical.BuildPlan(params)
	require.NoError(t, err, "expected to create logical plan")

	ms := metastore.NewObjectMetastore(
		loc.Bucket,
		metastore.Config{IndexStoragePrefix: loc.IndexPrefix},
		logger,
		metastore.NewObjectMetastoreMetrics(prometheus.NewRegistry()),
	)
	catalog := physical.NewMetastoreCatalog(func(start time.Time, end time.Time, selectors []*labels.Matcher, predicates []*labels.Matcher) ([]*metastore.DataobjSectionDescriptor, error) {
		resp, err := ms.Sections(ctx, metastore.SectionsRequest{
			Start:      start,
			End:        end,
			Matchers:   selectors,
			Predicates: predicates,
		})
		return resp.Sections, err
	})
	planner := physical.NewPlanner(physical.NewContext(params.Start(), params.End()), catalog)
	plan, err := planner.Build(logicalPlan)
	require.NoError(t, err, "expected to create physical plan")

	plan, err = planner.Optimize(plan)
	require.NoError(t, err, "expected to optimize physical plan")

	if testing.Verbose() {
		fmt.Fprintln(os.Stderr, physical.PrintAsTree(plan))
	}

	opts := workflow.Options{
		MaxRunningScanTasks:  32,
		MaxRunningOtherTasks: 0, // unlimited
	}
	wf, err := workflow.New(opts, logger, objtest.Tenant, sched, plan)
	require.NoError(t, err)

	if testing.Verbose() {
		workflow.Fprint(os.Stderr, wf)
	}
	return wf
}

func readTable(ctx context.Context, t *testing.T, p executor.Pipeline) arrow.Table {
	var recs []arrow.RecordBatch

	for {
		rec, err := p.Read(ctx)
		if rec != nil {
			if rec.NumRows() > 0 {
				recs = append(recs, rec)
			}
		}

		if err != nil && errors.Is(err, executor.EOF) {
			break
		}
		require.NoError(t, err)
	}

	return array.NewTableFromRecords(recs[0].Schema(), recs)
}

type testAddr string

var _ net.Addr = testAddr("")

func (addr testAddr) Network() string { return "local" }
func (addr testAddr) String() string  { return string(addr) }
