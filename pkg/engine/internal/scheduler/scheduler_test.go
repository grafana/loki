package scheduler_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/server"
	"github.com/grafana/dskit/services"
	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func BenchmarkScheduler_TaskDistribution(b *testing.B) {
	// sched := newTestScheduler(b)

	bkt := getPopulatedBucket(b)
	sched, schedulerAddress := startRemoteScheduler(b, log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr)), 10900)
	for i := 0; i < 1; i++ {
		startRemoteWorker(b, log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr)), bkt, 10901+i, 128, schedulerAddress)
	}

	ctx, capture := xcap.NewCapture(context.Background(), nil)
	defer capture.End()

	count := 100
	for b.Loop() {
		records := int64(0)
		wf := buildFakeWorkflow(b, sched.Runner(), count)
		pipe, err := wf.Run(ctx)
		require.NoError(b, err)

		pipe.Open(ctx)
		for {
			rec, err := pipe.Read(ctx)
			if err != nil {
				break
			}
			if rec != nil {
				records += rec.NumRows()
			}
			rec.Release()
		}
		pipe.Close()
		fmt.Println("records", records)
	}
}

func getPopulatedBucket(b *testing.B) objstore.Bucket {
	bkt := objstore.NewInMemBucket()
	reader1 := buildDataobj(b)
	require.NoError(b, bkt.Upload(b.Context(), "obj1", reader1))
	reader2 := buildDataobj(b)
	require.NoError(b, bkt.Upload(b.Context(), "obj2", reader2))
	reader3 := buildDataobj(b)
	require.NoError(b, bkt.Upload(b.Context(), "obj3", reader3))
	require.NoError(b, reader1.Close())
	require.NoError(b, reader2.Close())
	require.NoError(b, reader3.Close())
	return bkt
}

func buildDataobj(t testing.TB) io.ReadCloser {
	lb, err := logsobj.NewBuilder(logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:          2 * 1024 * 1024,
			TargetObjectSize:        4 * 1024 * 1024,
			TargetSectionSize:       2 * 1024 * 1024,
			SectionStripeMergeLimit: 2,
			BufferSize:              256,
		},
	}, nil)
	require.NoError(t, err)
	for i := range 100 {
		require.NoError(t, lb.Append("test", push.Stream{
			Labels:  `{app="test"}`,
			Entries: []push.Entry{{Timestamp: time.Unix(10, 0), Line: fmt.Sprintf("test %d", i)}},
		}))
	}

	obj, _, err := lb.Flush()
	require.NoError(t, err)
	reader, err := obj.Reader(t.Context())
	require.NoError(t, err)
	return reader
}

func buildFakeWorkflow(t testing.TB, runner workflow.Runner, count int) *workflow.Workflow {
	t.Helper()

	objs := []string{"obj1", "obj2", "obj3"}
	if count <= 0 {
		count = 1
	}

	now := time.Now()
	expectedPlan := &physical.Plan{}

	// Build a globally-aggregating node over a parallelized branch so workflow
	// planning can split one task per ScanSet target.
	globalTopK := expectedPlan.Graph().Add(&physical.TopK{
		SortBy:    &physical.ColumnExpr{Ref: semconv.ColumnIdentTimestamp.ColumnRef()},
		Ascending: false,
		K:         100,
	})
	parallelize := expectedPlan.Graph().Add(&physical.Parallelize{})
	localTopK := expectedPlan.Graph().Add(&physical.TopK{
		SortBy:    &physical.ColumnExpr{Ref: semconv.ColumnIdentTimestamp.ColumnRef()},
		Ascending: false,
		K:         100,
	})

	targets := make([]*physical.ScanTarget, 0, count)
	for i := range count {
		targets = append(targets, &physical.ScanTarget{
			Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{
				Location: physical.DataObjLocation(objs[i%len(objs)]), Section: 0, StreamIDs: []int64{1}, MaxTimeRange: physical.TimeRange{Start: now, End: now.Add(time.Second * 10)},
			},
		})
	}
	scanSet := expectedPlan.Graph().Add(&physical.ScanSet{Targets: targets})
	_ = expectedPlan.Graph().AddEdge(dag.Edge[physical.Node]{Parent: globalTopK, Child: parallelize})
	_ = expectedPlan.Graph().AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: localTopK})
	_ = expectedPlan.Graph().AddEdge(dag.Edge[physical.Node]{Parent: localTopK, Child: scanSet})

	wf, err := workflow.New(workflow.Options{
		Tenant:               "test",
		Actor:                []string{"test"},
		MaxRunningScanTasks:  0, // unlimited
		MaxRunningOtherTasks: 0, // unlimited
	}, log.NewNopLogger(), runner, expectedPlan)
	require.NoError(t, err)

	return wf
}

func startRemoteScheduler(b *testing.B, logger log.Logger, port int) (*engine.Scheduler, string) {
	schedSrv, schedSvc, err := newServerService("scheduler", port, logger, prometheus.NewRegistry())
	if err != nil {
		return nil, ""
	} else if err := services.StartAndAwaitRunning(context.Background(), schedSvc); err != nil {
		return nil, ""
	}
	sched, err := engine.NewScheduler(engine.SchedulerParams{
		Logger:        log.With(logger, "component", "scheduler"),
		AdvertiseAddr: schedSrv.HTTPListenAddr(),
	})
	if err != nil {
		return nil, ""
	} else if err := services.StartAndAwaitRunning(context.Background(), sched.Service()); err != nil {
		return nil, ""
	}
	sched.RegisterSchedulerServer(schedSrv.HTTP)

	return sched, schedSrv.HTTPListenAddr().String()
}

func startRemoteWorker(b *testing.B, logger log.Logger, bucket objstore.Bucket, port int, threads int, schedulerAddress string) *engine.Worker {
	workerSrv, workerSvc, err := newServerService("worker", port, logger, prometheus.NewRegistry())
	if err != nil {
		return nil
	} else if err := services.StartAndAwaitRunning(context.Background(), workerSvc); err != nil {
		return nil
	}

	worker, err := engine.NewWorker(engine.WorkerParams{
		Logger:         logger,
		AdvertiseAddr:  workerSrv.HTTPListenAddr(),
		Bucket:         bucket,
		LocalScheduler: nil,
		Config: engine.WorkerConfig{
			SchedulerLookupAddress:  schedulerAddress,
			SchedulerLookupInterval: time.Minute,
			WorkerThreads:           threads,
		},
		Executor: engine.ExecutorConfig{
			BatchSize: 8192,
		},
	})
	if err != nil {
		return nil
	} else if err := services.StartAndAwaitRunning(context.Background(), worker.Service()); err != nil {
		return nil
	}
	worker.RegisterWorkerServer(workerSrv.HTTP)

	return worker
}

func newServerService(name string, httpPort int, logger log.Logger, registerer prometheus.Registerer) (*server.Server, services.Service, error) {
	logger = log.With(logger, "component", "server", "server", name)
	serv, err := server.New(server.Config{
		Log:               logger,
		Registerer:        registerer,
		HTTPListenNetwork: "tcp",
		HTTPListenAddress: "localhost",
		HTTPListenPort:    httpPort,
	})
	if err != nil {
		return nil, nil, err
	}
	done := make(chan error, 1)
	svc := services.NewBasicService(
		nil,
		func(ctx context.Context) error {
			level.Info(logger).Log("msg", "server starting up")
			go func() {
				defer close(done)
				done <- serv.Run()
			}()
			select {
			case <-ctx.Done():
				return nil
			case err := <-done:
				return err
			}
		},
		func(err error) error {
			level.Info(logger).Log("msg", "server shutting down", "err", err)
			serv.Shutdown()
			<-done
			return nil
		},
	)

	// Enable HTTP/2
	serv.HTTPServer.Handler = h2c.NewHandler(serv.HTTPServer.Handler, &http2.Server{})

	return serv, svc, nil
}

func TestScheduler_RegisterManifest(t *testing.T) {
	t.Run("Succeeds with new streams", func(t *testing.T) {
		sched := newTestScheduler(t)

		manifest := &workflow.Manifest{
			Streams:            []*workflow.Stream{{ULID: ulid.Make()}},
			StreamEventHandler: nopStreamHandler,
		}
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
	})

	t.Run("Fails with existing stream", func(t *testing.T) {
		sched := newTestScheduler(t)

		manifest := &workflow.Manifest{
			Streams:            []*workflow.Stream{{ULID: ulid.Make()}},
			StreamEventHandler: nopStreamHandler,
		}
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.Error(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should reject existing stream")
	})

	t.Run("Fails with zero ULID", func(t *testing.T) {
		sched := newTestScheduler(t)

		manifest := &workflow.Manifest{
			Streams:            []*workflow.Stream{{ULID: ulid.Zero}},
			StreamEventHandler: nopStreamHandler,
		}
		require.Error(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should reject zero-value ULID")
	})

	t.Run("Fails with unrecognized source stream", func(t *testing.T) {
		sched := newTestScheduler(t)

		// Create a stream but don't add it to any manifest.
		stream := &workflow.Stream{ULID: ulid.Make()}

		manifest := &workflow.Manifest{
			Tasks: []*workflow.Task{{
				ULID: ulid.Make(),
				Sources: map[physical.Node][]*workflow.Stream{
					nil: {stream},
				},
			}},
			StreamEventHandler: nopStreamHandler,
			TaskEventHandler:   nopTaskHandler,
		}

		require.Error(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should not accept manifest with unrecognized source stream")
	})

	t.Run("Fails with unrecognized sink stream", func(t *testing.T) {
		sched := newTestScheduler(t)

		// Create a stream but don't add it to any manifest.
		stream := &workflow.Stream{ULID: ulid.Make()}

		manifest := &workflow.Manifest{
			Tasks: []*workflow.Task{{
				ULID: ulid.Make(),
				Sinks: map[physical.Node][]*workflow.Stream{
					nil: {stream},
				},
			}},
			StreamEventHandler: nopStreamHandler,
			TaskEventHandler:   nopTaskHandler,
		}

		require.Error(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should not accept manifest with unrecognized sink stream")
	})

	t.Run("Fails with bound stream (sink)", func(t *testing.T) {
		sched := newTestScheduler(t)

		var (
			stream = &workflow.Stream{ULID: ulid.Make()}

			manifest = &workflow.Manifest{
				Streams: []*workflow.Stream{stream},

				Tasks: []*workflow.Task{
					{
						ULID:  ulid.Make(),
						Sinks: map[physical.Node][]*workflow.Stream{nil: {stream}},
					},

					// Create a second task which is reusing the same stream; this should cause an error.
					{
						ULID:  ulid.Make(),
						Sinks: map[physical.Node][]*workflow.Stream{nil: {stream}},
					},
				},
			}
		)

		require.Error(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should reject task with bound sink")
	})

	t.Run("Fails with bound stream (source)", func(t *testing.T) {
		sched := newTestScheduler(t)

		var (
			stream = &workflow.Stream{ULID: ulid.Make()}

			manifest = &workflow.Manifest{
				Streams: []*workflow.Stream{stream},

				Tasks: []*workflow.Task{
					{
						ULID:    ulid.Make(),
						Sources: map[physical.Node][]*workflow.Stream{nil: {stream}},
					},

					// Create a second task which is reusing the same stream; this should cause an error.
					{
						ULID:    ulid.Make(),
						Sources: map[physical.Node][]*workflow.Stream{nil: {stream}},
					},
				},
			}
		)

		require.Error(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should reject task with bound source")
	})
}

func TestScheduler_UnregisterManifest(t *testing.T) {
	t.Run("Fails with unrecognized stream", func(t *testing.T) {
		sched := newTestScheduler(t)

		manifest := &workflow.Manifest{
			Streams:            []*workflow.Stream{{ULID: ulid.Make()}},
			StreamEventHandler: nopStreamHandler,
		}
		require.Error(t, sched.UnregisterManifest(t.Context(), manifest), "Scheduler should reject unrecognized stream")
	})

	t.Run("Succeeds with recognized stream", func(t *testing.T) {
		sched := newTestScheduler(t)

		manifest := &workflow.Manifest{
			Streams:            []*workflow.Stream{{ULID: ulid.Make()}},
			StreamEventHandler: nopStreamHandler,
		}
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.UnregisterManifest(t.Context(), manifest), "Scheduler should unregister valid manifest")
	})

	t.Run("Fails with removed stream", func(t *testing.T) {
		sched := newTestScheduler(t)

		manifest := &workflow.Manifest{
			Streams:            []*workflow.Stream{{ULID: ulid.Make()}},
			StreamEventHandler: nopStreamHandler,
		}
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.UnregisterManifest(t.Context(), manifest), "Scheduler should unregister valid manifest")
		require.Error(t, sched.UnregisterManifest(t.Context(), manifest), "Scheduler should reject already removed manifest")
	})

	t.Run("Streams move to closed state before removal", func(t *testing.T) {
		var streamState workflow.StreamState
		handler := func(_ context.Context, _ *workflow.Stream, newState workflow.StreamState) {
			streamState = newState
		}

		sched := newTestScheduler(t)

		manifest := &workflow.Manifest{
			Streams:            []*workflow.Stream{{ULID: ulid.Make()}},
			StreamEventHandler: handler,
		}
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.UnregisterManifest(t.Context(), manifest), "Scheduler should allow removing manifest")
		require.Equal(t, workflow.StreamStateClosed, streamState)
	})
}

func TestScheduler_Listen(t *testing.T) {
	t.Run("Succeeds on stream with no receiver or listener", func(t *testing.T) {
		sched := newTestScheduler(t)

		manifest := &workflow.Manifest{
			Streams:            []*workflow.Stream{{ULID: ulid.Make()}},
			StreamEventHandler: nopStreamHandler,
		}
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")

		var mockWriter mockRecordWriter
		err := sched.Listen(t.Context(), &mockWriter, manifest.Streams[0])
		require.NoError(t, err, "Listen should succeed on stream with no receiver or listener")
	})

	t.Run("Fails on stream with receiver", func(t *testing.T) {
		sched := newTestScheduler(t)

		var (
			stream   = &workflow.Stream{ULID: ulid.Make()}
			manifest = &workflow.Manifest{
				Streams: []*workflow.Stream{stream},

				Tasks: []*workflow.Task{{
					ULID: ulid.Make(),
					Sources: map[physical.Node][]*workflow.Stream{
						nil: {stream},
					},
				}},

				StreamEventHandler: nopStreamHandler,
				TaskEventHandler:   nopTaskHandler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")

		var mockWriter mockRecordWriter
		err := sched.Listen(t.Context(), &mockWriter, stream)
		require.Error(t, err, "Listen should fail on stream with receiver")
	})

	t.Run("Fails on stream with listener", func(t *testing.T) {
		sched := newTestScheduler(t)

		var (
			stream   = &workflow.Stream{ULID: ulid.Make()}
			manifest = &workflow.Manifest{
				Streams:            []*workflow.Stream{stream},
				StreamEventHandler: nopStreamHandler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")

		var mockWriterA mockRecordWriter
		err := sched.Listen(t.Context(), &mockWriterA, stream)
		require.NoError(t, err, "Listen should succeed on stream with no listener or receiver")

		var mockWriterB mockRecordWriter
		err = sched.Listen(t.Context(), &mockWriterB, stream)
		require.Error(t, err, "Listen should fail on stream with existing listener")
	})

	t.Run("Data is sent to listener", func(t *testing.T) {
		sched := newTestScheduler(t)

		var (
			stream   = &workflow.Stream{ULID: ulid.Make()}
			manifest = &workflow.Manifest{
				Streams:            []*workflow.Stream{stream},
				StreamEventHandler: nopStreamHandler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")

		var writer mockRecordWriter
		err := sched.Listen(t.Context(), &writer, stream)
		require.NoError(t, err, "Listen should succeed on stream with no listener or receiver")

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		var wg sync.WaitGroup
		wg.Go(func() {
			// Create a fake worker to send a message to the scheduler.
			conn, err := sched.DialFrom(ctx, wire.LocalWorker)
			require.NoError(t, err, "Should be able to connect to scheduler")
			defer conn.Close()

			peer := wire.Peer{
				Logger:  log.NewNopLogger(),
				Metrics: wire.NewMetrics(),
				Conn:    conn,
				Handler: nil,
			}
			go func() { _ = peer.Serve(ctx) }()

			msg := wire.StreamDataMessage{
				StreamID: stream.ULID,
				Data:     nil, // (No need to create an actual message for this test)
			}
			err = peer.SendMessage(ctx, msg)
			require.NoError(t, err, "Scheduler should accept message")
		})

		// Wait for data to be written to the capture writer
		require.Eventually(t, func() bool {
			return writer.writes.Load() == 1
		}, time.Second, 10*time.Millisecond, "Data should be forwarded to listener")

		wg.Wait()
	})

	t.Run("Stream is automatically closed with terminated sender", func(t *testing.T) {
		var streamState workflow.StreamState
		handler := func(_ context.Context, _ *workflow.Stream, newState workflow.StreamState) {
			streamState = newState
		}

		sched := newTestScheduler(t)

		var (
			stream   = &workflow.Stream{ULID: ulid.Make()}
			manifest = &workflow.Manifest{
				Streams: []*workflow.Stream{stream},
				Tasks: []*workflow.Task{{
					ULID: ulid.Make(),
					Sinks: map[physical.Node][]*workflow.Stream{
						nil: {stream},
					},
				}},
				StreamEventHandler: handler,
				TaskEventHandler:   nopTaskHandler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")

		// Cancel the tasks; this should cause the stream the close.
		require.NoError(t, sched.Cancel(t.Context(), manifest.Tasks...), "Scheduler should allow cancelling tasks")
		require.Equal(t, workflow.StreamStateClosed, streamState)
	})
}

func TestScheduler_Start(t *testing.T) {
	t.Run("New tasks are moved to pending state", func(t *testing.T) {
		var taskStatus workflow.TaskStatus
		handler := func(_ context.Context, _ *workflow.Task, newStatus workflow.TaskStatus) {
			taskStatus = newStatus
		}

		sched := newTestScheduler(t)

		var (
			exampleTask = &workflow.Task{ULID: ulid.Make(), Fragment: nil}
			manifest    = &workflow.Manifest{
				Tasks:              []*workflow.Task{exampleTask},
				StreamEventHandler: nopStreamHandler,
				TaskEventHandler:   handler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.Start(t.Context(), exampleTask), "Scheduler should start registered task")

		require.Equal(t, workflow.TaskStatePending, taskStatus.State, "Started tasks should move to pending state")
	})

	t.Run("Ignores already started tasks", func(t *testing.T) {
		sched := newTestScheduler(t)

		var (
			exampleTask = &workflow.Task{ULID: ulid.Make(), Fragment: nil}
			manifest    = &workflow.Manifest{
				Tasks:              []*workflow.Task{exampleTask},
				StreamEventHandler: nopStreamHandler,
				TaskEventHandler:   nopTaskHandler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.Start(t.Context(), exampleTask), "Scheduler should start registered task")
		require.NoError(t, sched.Start(t.Context(), exampleTask), "Scheduler should ignore already started tasks")
	})
}

func TestScheduler_Cancel(t *testing.T) {
	t.Run("Fails with unrecognized task", func(t *testing.T) {
		sched := newTestScheduler(t)

		exampleTask := workflow.Task{ULID: ulid.Make(), Fragment: nil}
		require.Error(t, sched.Cancel(t.Context(), &exampleTask), "Unrecognized tasks should be rejected")
	})

	t.Run("Handler is notified of canceled tasks", func(t *testing.T) {
		var taskStatus workflow.TaskStatus
		handler := func(_ context.Context, _ *workflow.Task, newStatus workflow.TaskStatus) {
			taskStatus = newStatus
		}

		sched := newTestScheduler(t)

		var (
			exampleTask = &workflow.Task{ULID: ulid.Make(), Fragment: nil}
			manifest    = &workflow.Manifest{
				Tasks:              []*workflow.Task{exampleTask},
				StreamEventHandler: nopStreamHandler,
				TaskEventHandler:   handler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.Start(t.Context(), exampleTask), "Scheduler should start registered task")

		require.NoError(t, sched.Cancel(t.Context(), exampleTask), "Scheduler should permit canceling tasks")
		require.Equal(t, workflow.TaskStateCancelled, taskStatus.State, "Canceled tasks should be in the canceled state")
	})
}

func TestScheduler_worker(t *testing.T) {
	t.Run("Worker must send WorkerHello before WorkerReady", func(t *testing.T) {
		sched := newTestScheduler(t)

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		conn, err := sched.DialFrom(ctx, wire.LocalWorker)
		require.NoError(t, err)
		defer conn.Close()

		messages := make(chan wire.Message, 10)

		peer := wire.Peer{
			Logger:  log.NewNopLogger(),
			Metrics: wire.NewMetrics(),
			Conn:    conn,
			Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
				select {
				case <-ctx.Done():
				case messages <- message:
				}
				return nil
			},
		}
		go func() { _ = peer.Serve(ctx) }()

		// Send a ready message so we get a task as soon as we create one.
		require.Error(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should not accept ready message without hello")
	})

	t.Run("Worker must send WorkerHello with at least one thread", func(t *testing.T) {
		sched := newTestScheduler(t)

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		conn, err := sched.DialFrom(ctx, wire.LocalWorker)
		require.NoError(t, err)
		defer conn.Close()

		messages := make(chan wire.Message, 10)

		peer := wire.Peer{
			Logger:  log.NewNopLogger(),
			Metrics: wire.NewMetrics(),
			Conn:    conn,
			Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
				select {
				case <-ctx.Done():
				case messages <- message:
				}
				return nil
			},
		}
		go func() { _ = peer.Serve(ctx) }()

		// Send a ready message so we get a task as soon as we create one.
		require.Error(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{Threads: 0}), "Scheduler should reject WorkerHello with <= 0 threads")
	})

	t.Run("Tasks are assigned to ready worker", func(t *testing.T) {
		sched := newTestScheduler(t)

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		conn, err := sched.DialFrom(ctx, wire.LocalWorker)
		require.NoError(t, err)
		defer conn.Close()

		messages := make(chan wire.Message, 10)

		peer := wire.Peer{
			Logger:  log.NewNopLogger(),
			Metrics: wire.NewMetrics(),
			Conn:    conn,
			Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
				select {
				case <-ctx.Done():
				case messages <- message:
				}
				return nil
			},
		}
		go func() { _ = peer.Serve(ctx) }()

		// Send a ready message so we get a task as soon as we create one.
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{Threads: 1}), "Scheduler should accept hello message")
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		var (
			exampleTask = &workflow.Task{ULID: ulid.Make()}
			manifest    = &workflow.Manifest{
				Tasks:              []*workflow.Task{exampleTask},
				StreamEventHandler: nopStreamHandler,
				TaskEventHandler:   nopTaskHandler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.Start(t.Context(), exampleTask), "Scheduler should start registered task")

		var assignedTask *workflow.Task

	WaitAssign:
		for {
			select {
			case <-ctx.Done():
				require.Fail(t, "time out before receiving task")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.WorkerSubscribeMessage:
					continue // Ignore; we already sent WorkerReady
				case wire.TaskAssignMessage:
					assignedTask = msg.Task
					break WaitAssign
				default:
					require.Fail(t, "Unexpected message type", "Unexpected message type %T", msg)
				}
			}
		}

		require.Equal(t, exampleTask.ULID, assignedTask.ULID, "Should have been assigned expected task")

		// Validate that the scheduler is not tracking thread counts with
		// WorkerReady.
		terminalStatus := workflow.TaskStatus{State: workflow.TaskStateFailed}
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should not reject ReadyMessage beyond total count")
		require.NoError(t, peer.SendMessage(ctx, wire.TaskStatusMessage{ID: assignedTask.ULID, Status: terminalStatus}), "Scheduler should accept TaskStatusMessage")
	})

	t.Run("Owner is notified of canceled tasks", func(t *testing.T) {
		sched := newTestScheduler(t)

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		conn, err := sched.DialFrom(ctx, wire.LocalWorker)
		require.NoError(t, err)
		defer conn.Close()

		messages := make(chan wire.Message, 10)

		peer := wire.Peer{
			Logger:  log.NewNopLogger(),
			Metrics: wire.NewMetrics(),
			Conn:    conn,
			Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
				select {
				case <-ctx.Done():
				case messages <- message:
				}
				return nil
			},
		}
		go func() { _ = peer.Serve(ctx) }()

		// Send a ready message so we get a task as soon as we create one.
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{Threads: 1}), "Scheduler should accept hello message")
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		var (
			exampleTask = &workflow.Task{ULID: ulid.Make()}
			manifest    = &workflow.Manifest{
				Tasks:              []*workflow.Task{exampleTask},
				StreamEventHandler: nopStreamHandler,
				TaskEventHandler:   nopTaskHandler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.Start(t.Context(), exampleTask), "Scheduler should start registered task")

		// Wait for assignment.
	WaitAssign:
		for {
			select {
			case <-ctx.Done():
				require.Fail(t, "time out before receiving expected message")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.WorkerSubscribeMessage:
					continue // Ignore; we already sent WorkerReady
				case wire.TaskAssignMessage:
					break WaitAssign
				default:
					require.Fail(t, "Unexpected message type", "Unexpected message type %T", msg)
				}
			}
		}

		require.NoError(t, sched.Cancel(t.Context(), exampleTask), "Scheduler should permit cancellation of registered task")

		// Wait for cancellation.
		select {
		case <-ctx.Done():
			require.Fail(t, "time out before receiving expected message")
		case msg := <-messages:
			switch msg := msg.(type) {
			case wire.TaskCancelMessage:
				require.Equal(t, exampleTask.ULID, msg.ID, "Expected task should have been canceled")
				return
			default:
				require.Fail(t, "Unexpected message type %T", msg)
			}
		}
	})

	t.Run("Owned tasks are canceled upon connection loss", func(t *testing.T) {
		var taskStatus atomic.Pointer[workflow.TaskStatus]
		handler := func(_ context.Context, _ *workflow.Task, newStatus workflow.TaskStatus) {
			taskStatus.Store(&newStatus)
		}

		sched := newTestScheduler(t)

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		conn, err := sched.DialFrom(ctx, wire.LocalWorker)
		require.NoError(t, err)

		messages := make(chan wire.Message, 10)

		peer := wire.Peer{
			Logger:  log.NewNopLogger(),
			Metrics: wire.NewMetrics(),
			Conn:    conn,
			Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
				select {
				case <-ctx.Done():
				case messages <- message:
				}
				return nil
			},
		}
		go func() { _ = peer.Serve(ctx) }()

		// Send a ready message so we get a task as soon as we create one.
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{Threads: 1}), "Scheduler should accept hello message")
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		var (
			exampleTask = &workflow.Task{ULID: ulid.Make()}
			manifest    = &workflow.Manifest{
				Tasks:              []*workflow.Task{exampleTask},
				StreamEventHandler: nopStreamHandler,
				TaskEventHandler:   handler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.Start(t.Context(), exampleTask), "Scheduler should start registered task")

		// Wait for assignment.
	WaitAssign:
		for {
			select {
			case <-ctx.Done():
				require.Fail(t, "time out before receiving expected message")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.WorkerSubscribeMessage:
					continue // Ignore; we already sent WorkerReady
				case wire.TaskAssignMessage:
					break WaitAssign
				default:
					require.Fail(t, "Unexpected message type", "Unexpected message type %T", msg)
				}
			}
		}

		// We can't close the connection immediately, because the scheduler may
		// be waiting for an ACK. To hack around this, we'll send a status
		// update, which will guarantee that the scheduler has received the ACK
		// since it can't process this message until it finished task assignment.
		require.NoError(t, peer.SendMessage(ctx, wire.TaskStatusMessage{
			ID:     exampleTask.ULID,
			Status: workflow.TaskStatus{State: workflow.TaskStateRunning},
		}), "Sending status message should succeed")

		// Close the connection, then wait for the task to be canceled.
		require.NoError(t, conn.Close(), "Closing connection should succeed")

		maxWait, _ := ctx.Deadline()
		require.Eventually(t, func() bool {
			return taskStatus.Load().State.Terminal()
		}, time.Until(maxWait), 25*time.Millisecond, "Owned task should have been canceled")
	})

	t.Run("Sender receives bind once listener is available", func(t *testing.T) {
		t.Run("Listen before assignment", func(t *testing.T) {
			sched := newTestScheduler(t)

			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()

			conn, err := sched.DialFrom(ctx, wire.LocalWorker)
			require.NoError(t, err)
			defer conn.Close()

			var (
				stream = workflow.Stream{ULID: ulid.Make()}
				task   = workflow.Task{
					ULID: ulid.Make(),

					Sinks: map[physical.Node][]*workflow.Stream{
						nil: {&stream},
					},
				}

				manifest = &workflow.Manifest{
					Streams:            []*workflow.Stream{&stream},
					Tasks:              []*workflow.Task{&task},
					StreamEventHandler: nopStreamHandler,
					TaskEventHandler:   nopTaskHandler,
				}
			)
			require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
			require.NoError(t, sched.Start(t.Context(), &task), "Scheduler should start registered task")

			var writer mockRecordWriter
			err = sched.Listen(t.Context(), &writer, &stream)
			require.NoError(t, err, "Scheduler should permit listening to stream")

			messages := make(chan wire.Message, 10)

			peer := wire.Peer{
				Logger:  log.NewNopLogger(),
				Metrics: wire.NewMetrics(),
				Conn:    conn,
				Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
					select {
					case <-ctx.Done():
					case messages <- message:
					}
					return nil
				},
			}
			go func() { _ = peer.Serve(ctx) }()

			// Send a ready message so we get the task.
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{Threads: 1}), "Scheduler should accept hello message")
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

			for {
				select {
				case <-ctx.Done():
					require.Fail(t, "time out before receiving task")
				case msg := <-messages:
					switch msg := msg.(type) {
					case wire.StreamBindMessage:
						require.Equal(t, stream.ULID, msg.StreamID, "Should have seen expected stream")
						require.Equal(t, wire.LocalScheduler, msg.Receiver, "Should have seen expected receiver")
						return
					case wire.WorkerSubscribeMessage:
						// Ignore; we already sent WorkerReady
					case wire.TaskAssignMessage:
					default:
						require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
					}
				}
			}
		})

		t.Run("Listen after assignment", func(t *testing.T) {
			sched := newTestScheduler(t)

			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()

			conn, err := sched.DialFrom(ctx, wire.LocalWorker)
			require.NoError(t, err)
			defer conn.Close()

			var (
				stream = workflow.Stream{ULID: ulid.Make()}
				task   = workflow.Task{
					ULID: ulid.Make(),

					Sinks: map[physical.Node][]*workflow.Stream{
						nil: {&stream},
					},
				}

				manifest = &workflow.Manifest{
					Streams:            []*workflow.Stream{&stream},
					Tasks:              []*workflow.Task{&task},
					StreamEventHandler: nopStreamHandler,
					TaskEventHandler:   nopTaskHandler,
				}
			)
			require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
			require.NoError(t, sched.Start(t.Context(), &task), "Scheduler should start registered task")

			messages := make(chan wire.Message, 10)

			peer := wire.Peer{
				Logger:  log.NewNopLogger(),
				Metrics: wire.NewMetrics(),
				Conn:    conn,
				Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
					select {
					case <-ctx.Done():
					case messages <- message:
					}
					return nil
				},
			}
			go func() { _ = peer.Serve(ctx) }()

			// Send a ready message so we get the task.
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{Threads: 1}), "Scheduler should accept hello message")
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

			// Wait for assignment.
		WaitAssign:
			for {
				select {
				case <-ctx.Done():
					require.Fail(t, "time out before receiving task")
				case msg := <-messages:
					switch msg := msg.(type) {
					case wire.WorkerSubscribeMessage:
						continue // Ignore; we already sent WorkerReady
					case wire.TaskAssignMessage:
						break WaitAssign
					default:
						require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
					}
				}
			}

			// Listen to the stream, then wait for the binding.
			var writer mockRecordWriter
			err = sched.Listen(t.Context(), &writer, &stream)
			require.NoError(t, err, "Scheduler should permit listening to stream")

			select {
			case <-ctx.Done():
				require.Fail(t, "time out before receiving task")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.StreamBindMessage:
					require.Equal(t, stream.ULID, msg.StreamID, "Should have seen expected stream")
					require.Equal(t, wire.LocalScheduler, msg.Receiver, "Should have seen expected receiver")
					return
				default:
					require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
				}
			}
		})
	})

	t.Run("Sender receives bind once receiver is available", func(t *testing.T) {
		t.Run("Receiver assigned before sender", func(t *testing.T) {
			sched := newTestScheduler(t)

			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()

			conn, err := sched.DialFrom(ctx, wire.LocalWorker)
			require.NoError(t, err)
			defer conn.Close()

			var (
				stream = workflow.Stream{ULID: ulid.Make()}

				receiver = workflow.Task{
					ULID:    ulid.Make(),
					Sources: map[physical.Node][]*workflow.Stream{nil: {&stream}},
				}
				sender = workflow.Task{
					ULID:  ulid.Make(),
					Sinks: map[physical.Node][]*workflow.Stream{nil: {&stream}},
				}

				manifest = &workflow.Manifest{
					Streams:            []*workflow.Stream{&stream},
					Tasks:              []*workflow.Task{&receiver, &sender},
					StreamEventHandler: nopStreamHandler,
					TaskEventHandler:   nopTaskHandler,
				}
			)
			require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")

			messages := make(chan wire.Message, 10)

			peer := wire.Peer{
				Logger:  log.NewNopLogger(),
				Metrics: wire.NewMetrics(),
				Conn:    conn,
				Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
					select {
					case <-ctx.Done():
					case messages <- message:
					}
					return nil
				},
			}
			go func() { _ = peer.Serve(ctx) }()

			require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{Threads: 2}), "Scheduler should accept hello message")
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

			// Scheduler the receiver first; we'll schedule the sender once the task has been assigned.
			require.NoError(t, sched.Start(t.Context(), &receiver), "Scheduler should start registered task")

			for {
				select {
				case <-ctx.Done():
					require.Fail(t, "time out before receiving task")
				case msg := <-messages:
					switch msg := msg.(type) {
					case wire.WorkerSubscribeMessage:
						continue // Ignore; we already sent WorkerReady
					case wire.StreamBindMessage:
						require.Equal(t, stream.ULID, msg.StreamID, "Should have seen expected stream")
						require.Equal(t, wire.LocalWorker, msg.Receiver, "Should have seen expected receiver")
						return
					case wire.TaskAssignMessage:
						if msg.Task.ULID == receiver.ULID {
							require.NoError(t, sched.Start(t.Context(), &sender), "Scheduler should start registered task")
						}
					default:
						require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
					}
				}
			}
		})

		t.Run("Receiver assigned after sender", func(t *testing.T) {
			sched := newTestScheduler(t)

			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()

			conn, err := sched.DialFrom(ctx, wire.LocalWorker)
			require.NoError(t, err)
			defer conn.Close()

			var (
				stream = workflow.Stream{ULID: ulid.Make()}

				receiver = workflow.Task{
					ULID:    ulid.Make(),
					Sources: map[physical.Node][]*workflow.Stream{nil: {&stream}},
				}
				sender = workflow.Task{
					ULID:  ulid.Make(),
					Sinks: map[physical.Node][]*workflow.Stream{nil: {&stream}},
				}

				manifest = &workflow.Manifest{
					Streams:            []*workflow.Stream{&stream},
					Tasks:              []*workflow.Task{&receiver, &sender},
					StreamEventHandler: nopStreamHandler,
					TaskEventHandler:   nopTaskHandler,
				}
			)
			require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")

			messages := make(chan wire.Message, 10)

			peer := wire.Peer{
				Logger:  log.NewNopLogger(),
				Metrics: wire.NewMetrics(),
				Conn:    conn,
				Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
					select {
					case <-ctx.Done():
					case messages <- message:
					}
					return nil
				},
			}
			go func() { _ = peer.Serve(ctx) }()

			require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{Threads: 2}), "Scheduler should accept hello message")
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

			// Schedule the sender first; we'll schedule the receiver once the task has been assigned.
			require.NoError(t, sched.Start(t.Context(), &sender), "Scheduler should start registered task")

			for {
				select {
				case <-ctx.Done():
					require.Fail(t, "time out before receiving task")
				case msg := <-messages:
					switch msg := msg.(type) {
					case wire.WorkerSubscribeMessage:
						continue // Ignore; we already sent WorkerReady
					case wire.StreamBindMessage:
						require.Equal(t, stream.ULID, msg.StreamID, "Should have seen expected stream")
						require.Equal(t, wire.LocalWorker, msg.Receiver, "Should have seen expected receiver")
						return
					case wire.TaskAssignMessage:
						if msg.Task.ULID == sender.ULID {
							require.NoError(t, sched.Start(t.Context(), &receiver), "Scheduler should start registered task")
						}
					default:
						require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
					}
				}
			}
		})
	})

	t.Run("Task state change propagates to handler", func(t *testing.T) {
		var taskStatus atomic.Pointer[workflow.TaskStatus]
		handler := func(_ context.Context, _ *workflow.Task, newStatus workflow.TaskStatus) {
			taskStatus.Store(&newStatus)
		}

		sched := newTestScheduler(t)

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		conn, err := sched.DialFrom(ctx, wire.LocalWorker)
		require.NoError(t, err)
		defer conn.Close()

		var (
			exampleTask = &workflow.Task{ULID: ulid.Make()}
			manifest    = &workflow.Manifest{
				Tasks:              []*workflow.Task{exampleTask},
				StreamEventHandler: nopStreamHandler,
				TaskEventHandler:   handler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.Start(t.Context(), exampleTask), "Scheduler should start registered task")

		messages := make(chan wire.Message, 10)

		peer := wire.Peer{
			Logger:  log.NewNopLogger(),
			Metrics: wire.NewMetrics(),
			Conn:    conn,
			Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
				select {
				case <-ctx.Done():
				case messages <- message:
				}
				return nil
			},
		}
		go func() { _ = peer.Serve(ctx) }()

		// Send a ready message so we get the task.
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{Threads: 1}), "Scheduler should accept hello message")
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		// Wait for assignment.
	WaitAssign:
		for {
			select {
			case <-ctx.Done():
				require.Fail(t, "time out before receiving task")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.WorkerSubscribeMessage:
					continue // Ignore; we already sent WorkerReady
				case wire.TaskAssignMessage:
					break WaitAssign
				default:
					require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
				}
			}
		}

		require.NoError(t, peer.SendMessage(ctx, wire.TaskStatusMessage{
			ID:     exampleTask.ULID,
			Status: workflow.TaskStatus{State: workflow.TaskStateRunning},
		}), "Scheduler should accept status message")

		// Wait for the handler to receive the state change.
		maxWait, _ := ctx.Deadline()
		require.Eventually(t, func() bool {
			return taskStatus.Load().State == workflow.TaskStateRunning
		}, time.Until(maxWait), 25*time.Millisecond, "Handler should be notified of state change")
	})

	t.Run("Stream state change propagates to handler", func(t *testing.T) {
		var streamState atomic.Pointer[workflow.StreamState]
		handler := func(_ context.Context, _ *workflow.Stream, newState workflow.StreamState) {
			streamState.Store(&newState)
		}

		sched := newTestScheduler(t)

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		conn, err := sched.DialFrom(ctx, wire.LocalWorker)
		require.NoError(t, err)
		defer conn.Close()

		var (
			stream = workflow.Stream{ULID: ulid.Make()}
			task   = workflow.Task{
				ULID: ulid.Make(),

				Sinks: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}

			manifest = &workflow.Manifest{
				Streams:            []*workflow.Stream{&stream},
				Tasks:              []*workflow.Task{&task},
				StreamEventHandler: handler,
				TaskEventHandler:   nopTaskHandler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.Start(t.Context(), &task), "Scheduler should start registered task")

		messages := make(chan wire.Message, 10)

		peer := wire.Peer{
			Logger:  log.NewNopLogger(),
			Metrics: wire.NewMetrics(),
			Conn:    conn,
			Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
				select {
				case <-ctx.Done():
				case messages <- message:
				}
				return nil
			},
		}
		go func() { _ = peer.Serve(ctx) }()

		// Send a ready message so we get the task.
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{Threads: 1}), "Scheduler should accept hello message")
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		// Wait for assignment.
	WaitAssign:
		for {
			select {
			case <-ctx.Done():
				require.Fail(t, "time out before receiving task")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.WorkerSubscribeMessage:
					continue // Ignore; we already sent WorkerReady
				case wire.TaskAssignMessage:
					break WaitAssign
				default:
					require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
				}
			}
		}

		require.NoError(t, peer.SendMessage(ctx, wire.StreamStatusMessage{
			StreamID: stream.ULID,
			State:    workflow.StreamStateBlocked,
		}), "Scheduler should accept status message")

		// Wait for the handler to receive the state change.
		maxWait, _ := ctx.Deadline()
		require.Eventually(t, func() bool {
			return *streamState.Load() == workflow.StreamStateBlocked
		}, time.Until(maxWait), 25*time.Millisecond, "Handler should be notified of state change")
	})

	t.Run("Stream state change propagates to receiver", func(t *testing.T) {
		sched := newTestScheduler(t)

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		conn, err := sched.DialFrom(ctx, wire.LocalWorker)
		require.NoError(t, err)
		defer conn.Close()

		var (
			stream = workflow.Stream{ULID: ulid.Make()}

			receiver = workflow.Task{
				ULID:    ulid.Make(),
				Sources: map[physical.Node][]*workflow.Stream{nil: {&stream}},
			}
			sender = workflow.Task{
				ULID:  ulid.Make(),
				Sinks: map[physical.Node][]*workflow.Stream{nil: {&stream}},
			}

			manifest = &workflow.Manifest{
				Streams:            []*workflow.Stream{&stream},
				Tasks:              []*workflow.Task{&receiver, &sender},
				StreamEventHandler: nopStreamHandler,
				TaskEventHandler:   nopTaskHandler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.Start(t.Context(), &receiver, &sender), "Scheduler should start registered tasks")

		messages := make(chan wire.Message, 10)

		peer := wire.Peer{
			Logger:  log.NewNopLogger(),
			Metrics: wire.NewMetrics(),
			Conn:    conn,
			Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
				select {
				case <-ctx.Done():
				case messages <- message:
				}
				return nil
			},
		}
		go func() { _ = peer.Serve(ctx) }()

		// Send a ready message so we get the tasks.
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{Threads: 2}), "Scheduler should accept hello message")
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		// Wait for assignment of both tasks.
		for assigned := 0; assigned < 2; {
			select {
			case <-ctx.Done():
				require.Fail(t, "time out before receiving task")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.StreamBindMessage: // Ignore bindings
				case wire.WorkerSubscribeMessage: // Ignore subscriptions
				case wire.TaskAssignMessage:
					assigned++
				default:
					require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
				}
			}
		}

		require.NoError(t, peer.SendMessage(ctx, wire.StreamStatusMessage{
			StreamID: stream.ULID,
			State:    workflow.StreamStateBlocked,
		}), "Scheduler should accept status message")

		// Wait for get the status change.
		for {
			select {
			case <-ctx.Done():
				require.Fail(t, "time out before receiving task")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.StreamBindMessage: // Ignore bindings
				case wire.StreamStatusMessage:
					require.Equal(t, stream.ULID, msg.StreamID, "Unexpected stream ID")
					require.Equal(t, workflow.StreamStateBlocked, msg.State, "Unexpected stream state")
					return
				default:
					require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
				}
			}
		}
	})

	t.Run("Receiver is provided most recent stream state", func(t *testing.T) {
		sched := newTestScheduler(t)

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		conn, err := sched.DialFrom(ctx, wire.LocalWorker)
		require.NoError(t, err)
		defer conn.Close()

		var (
			stream = workflow.Stream{ULID: ulid.Make()}

			receiver = workflow.Task{
				ULID:    ulid.Make(),
				Sources: map[physical.Node][]*workflow.Stream{nil: {&stream}},
			}
			sender = workflow.Task{
				ULID:  ulid.Make(),
				Sinks: map[physical.Node][]*workflow.Stream{nil: {&stream}},
			}

			manifest = &workflow.Manifest{
				Streams:            []*workflow.Stream{&stream},
				Tasks:              []*workflow.Task{&receiver, &sender},
				StreamEventHandler: nopStreamHandler,
				TaskEventHandler:   nopTaskHandler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.Start(t.Context(), &sender), "Scheduler should start registered task")

		messages := make(chan wire.Message, 10)

		peer := wire.Peer{
			Logger:  log.NewNopLogger(),
			Metrics: wire.NewMetrics(),
			Conn:    conn,
			Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
				select {
				case <-ctx.Done():
				case messages <- message:
				}
				return nil
			},
		}
		go func() { _ = peer.Serve(ctx) }()

		// Send a ready message so we get the tasks.
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{Threads: 2}), "Scheduler should accept hello message")
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		// Wait for task assignment.
	WaitAssign:
		for {
			select {
			case <-ctx.Done():
				require.Fail(t, "time out before receiving task")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.WorkerSubscribeMessage:
					continue // Ignore; we already sent WorkerReady
				case wire.TaskAssignMessage:
					break WaitAssign
				default:
					require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
				}
			}
		}

		require.NoError(t, peer.SendMessage(ctx, wire.StreamStatusMessage{
			StreamID: stream.ULID,
			State:    workflow.StreamStateOpen,
		}), "Scheduler should accept state change")

		// Start the receiver. It should be assigned with a message indicating
		// the existing state of the stream it uses.
		require.NoError(t, sched.Start(t.Context(), &receiver), "Scheduler should start registered task")
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		// Wait for assignment.
		for {
			select {
			case <-ctx.Done():
				require.Fail(t, "time out before receiving task")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.StreamBindMessage: // Ignore bindings
				case wire.TaskAssignMessage:
					require.NotNil(t, msg.StreamStates, "Stream states should exist")
					value, ok := msg.StreamStates[stream.ULID]
					require.True(t, ok, "Stream state should exist for source")
					require.Equal(t, workflow.StreamStateOpen, value, "Current stream state should be sent with assignment")
					return

				default:
					require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
				}
			}
		}
	})
}

func newTestScheduler(t testing.TB) *scheduler.Scheduler {
	t.Helper()

	sched, err := scheduler.New(scheduler.Config{
		Logger:   log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr)),
		Listener: &wire.Local{Address: wire.LocalScheduler},
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

func nopStreamHandler(_ context.Context, _ *workflow.Stream, _ workflow.StreamState) {}
func nopTaskHandler(context.Context, *workflow.Task, workflow.TaskStatus)            {}

type mockRecordWriter struct {
	writes atomic.Int64
}

func (m *mockRecordWriter) Write(_ context.Context, _ arrow.RecordBatch) error {
	m.writes.Inc()
	return nil
}
