package scheduler

import (
	"context"
	"net/http"
	"os"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

func TestScheduler_RegisterManifest(t *testing.T) {
	t.Run("Succeeds with new streams", func(t *testing.T) {
		sched := newTestScheduler(t)

		manifest := &workflow.Manifest{
			Streams:             []*workflow.Stream{{ULID: ulid.Make()}},
			StreamClosedHandler: nopStreamHandler,
		}
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
	})

	t.Run("Fails with existing stream", func(t *testing.T) {
		sched := newTestScheduler(t)

		manifest := &workflow.Manifest{
			Streams:             []*workflow.Stream{{ULID: ulid.Make()}},
			StreamClosedHandler: nopStreamHandler,
		}
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.Error(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should reject existing stream")
	})

	t.Run("Fails with zero ULID", func(t *testing.T) {
		sched := newTestScheduler(t)

		manifest := &workflow.Manifest{
			Streams:             []*workflow.Stream{{ULID: ulid.Zero}},
			StreamClosedHandler: nopStreamHandler,
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
			StreamClosedHandler: nopStreamHandler,
			TaskResultHandler:   nopTaskHandler,
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
			StreamClosedHandler: nopStreamHandler,
			TaskResultHandler:   nopTaskHandler,
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

	t.Run("Counts resources only after successful manifest validation", func(t *testing.T) {
		sched := newTestScheduler(t)

		valid := &workflow.Manifest{
			ID:                  ulid.Make(),
			Streams:             []*workflow.Stream{{ULID: ulid.Make()}},
			Tasks:               []*workflow.Task{{ULID: ulid.Make()}},
			StreamClosedHandler: nopStreamHandler,
			TaskResultHandler:   nopTaskHandler,
		}
		require.NoError(t, sched.RegisterManifest(t.Context(), valid))
		require.Equal(t, float64(1), testutil.ToFloat64(sched.metrics.tasksRegisteredTotal))
		require.Equal(t, float64(1), testutil.ToFloat64(sched.metrics.streamsRegisteredTotal))

		duplicateID := ulid.Make()
		invalid := &workflow.Manifest{
			ID:                  ulid.Make(),
			Streams:             []*workflow.Stream{{ULID: ulid.Make()}},
			StreamClosedHandler: nopStreamHandler,
			Tasks: []*workflow.Task{
				{ULID: duplicateID},
				{ULID: duplicateID},
			},
			TaskResultHandler: nopTaskHandler,
		}
		require.Error(t, sched.RegisterManifest(t.Context(), invalid))
		require.Equal(t, float64(1), testutil.ToFloat64(sched.metrics.tasksRegisteredTotal))
		require.Equal(t, float64(1), testutil.ToFloat64(sched.metrics.streamsRegisteredTotal))
	})
}

func TestScheduler_UnregisterManifest(t *testing.T) {
	t.Run("Fails with unrecognized stream", func(t *testing.T) {
		sched := newTestScheduler(t)

		manifest := &workflow.Manifest{
			Streams:             []*workflow.Stream{{ULID: ulid.Make()}},
			StreamClosedHandler: nopStreamHandler,
		}
		require.Error(t, sched.UnregisterManifest(t.Context(), manifest), "Scheduler should reject unrecognized stream")
	})

	t.Run("Succeeds with recognized stream", func(t *testing.T) {
		sched := newTestScheduler(t)

		manifest := &workflow.Manifest{
			Streams:             []*workflow.Stream{{ULID: ulid.Make()}},
			StreamClosedHandler: nopStreamHandler,
		}
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.UnregisterManifest(t.Context(), manifest), "Scheduler should unregister valid manifest")
	})

	t.Run("Fails with removed stream", func(t *testing.T) {
		sched := newTestScheduler(t)

		manifest := &workflow.Manifest{
			Streams:             []*workflow.Stream{{ULID: ulid.Make()}},
			StreamClosedHandler: nopStreamHandler,
		}
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.UnregisterManifest(t.Context(), manifest), "Scheduler should unregister valid manifest")
		require.Error(t, sched.UnregisterManifest(t.Context(), manifest), "Scheduler should reject already removed manifest")
	})

	t.Run("Streams move to closed state before removal", func(t *testing.T) {
		var streamClosed bool
		handler := func(_ context.Context, _ *workflow.Stream) {
			streamClosed = true
		}

		sched := newTestScheduler(t)

		manifest := &workflow.Manifest{
			Streams:             []*workflow.Stream{{ULID: ulid.Make()}},
			StreamClosedHandler: handler,
		}
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.UnregisterManifest(t.Context(), manifest), "Scheduler should allow removing manifest")
		require.True(t, streamClosed)
	})
}

func TestScheduler_Listen(t *testing.T) {
	t.Run("Succeeds on stream with no receiver or listener", func(t *testing.T) {
		sched := newTestScheduler(t)

		manifest := &workflow.Manifest{
			Streams:             []*workflow.Stream{{ULID: ulid.Make()}},
			StreamClosedHandler: nopStreamHandler,
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

				StreamClosedHandler: nopStreamHandler,
				TaskResultHandler:   nopTaskHandler,
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
				Streams:             []*workflow.Stream{stream},
				StreamClosedHandler: nopStreamHandler,
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
				Streams:             []*workflow.Stream{stream},
				StreamClosedHandler: nopStreamHandler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")

		writer := mockRecordWriter{wrote: make(chan struct{}, 1)}
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

		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for data to reach listener")
		case <-writer.wrote:
		}
		require.Equal(t, int64(1), writer.writes.Load())
		wg.Wait()
	})

	t.Run("Stream is automatically closed with terminated sender", func(t *testing.T) {
		var streamClosed bool
		handler := func(_ context.Context, _ *workflow.Stream) {
			streamClosed = true
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
				StreamClosedHandler: handler,
				TaskResultHandler:   nopTaskHandler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")

		// Cancel the tasks; this should cause the stream the close.
		require.NoError(t, sched.Cancel(t.Context(), manifest.Tasks...), "Scheduler should allow cancelling tasks")
		require.True(t, streamClosed)
	})
}

func TestScheduler_ReleasesTerminalResources(t *testing.T) {
	// A finished task stays registered until its manifest is unregistered so
	// that deregistration, late worker results, and redundant cancellations
	// stay well-defined. Its (potentially large) capture, however, is released
	// as soon as the task produces a terminal result.
	sched := newTestScheduler(t)

	var notified workflow.TaskResult
	handler := func(_ context.Context, _ *workflow.Task, result workflow.TaskResult) {
		notified = result
	}

	exampleTask := &workflow.Task{ULID: ulid.Make()}
	manifest := &workflow.Manifest{
		Tasks:               []*workflow.Task{exampleTask},
		StreamClosedHandler: nopStreamHandler,
		TaskResultHandler:   handler,
	}
	require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")

	// Before the task finishes, the scheduler holds its capture.
	registered := sched.tasks[exampleTask.ULID]
	require.NotNil(t, registered, "task should be registered")
	require.NotNil(t, registered.capture, "capture should be held while the task is live")

	// Cancel a never-started task, driving it synchronously to a terminal result.
	require.NoError(t, sched.Cancel(t.Context(), exampleTask), "Scheduler should permit cancelling a registered task")

	// The task remains registered so the manifest can still be unregistered.
	require.Contains(t, sched.tasks, exampleTask.ULID, "finished task should remain registered until the manifest is unregistered")
	require.True(t, registered.HasResult(), "task should have a terminal result")

	// But its heavy, no-longer-needed state has been released.
	require.Nil(t, registered.capture, "capture should be released once the task is terminal")
	require.Nil(t, registered.region, "region should be released once the task is terminal")
	result, _ := registered.Result()
	require.Nil(t, result.Capture, "result capture should be released once the task is terminal")

	// The handler still received the capture through the notification.
	require.NotNil(t, notified.Capture, "handler should still receive the task capture via the notification")

	// The manifest can still be cleanly unregistered afterwards.
	require.NoError(t, sched.UnregisterManifest(t.Context(), manifest), "Scheduler should unregister the manifest")
	require.NotContains(t, sched.tasks, exampleTask.ULID, "task should be removed after deregistration")
}

func TestScheduler_Start(t *testing.T) {
	t.Run("Starts new tasks without emitting a task result", func(t *testing.T) {
		var results atomic.Int64
		handler := func(_ context.Context, _ *workflow.Task, _ workflow.TaskResult) {
			results.Inc()
		}

		sched := newTestScheduler(t)

		var (
			exampleTask = &workflow.Task{ULID: ulid.Make(), Fragment: nil}
			manifest    = &workflow.Manifest{
				Tasks:               []*workflow.Task{exampleTask},
				StreamClosedHandler: nopStreamHandler,
				TaskResultHandler:   handler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.Start(t.Context(), exampleTask), "Scheduler should start registered task")

		require.True(t, sched.tasks[exampleTask.ULID].Queued())
		require.Zero(t, results.Load(), "Start should not emit a terminal task result")
	})

	t.Run("Ignores already started tasks", func(t *testing.T) {
		sched := newTestScheduler(t)

		var (
			exampleTask = &workflow.Task{ULID: ulid.Make(), Fragment: nil}
			manifest    = &workflow.Manifest{
				Tasks:               []*workflow.Task{exampleTask},
				StreamClosedHandler: nopStreamHandler,
				TaskResultHandler:   nopTaskHandler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.Start(t.Context(), exampleTask), "Scheduler should start registered task")
		require.NoError(t, sched.Start(t.Context(), exampleTask), "Scheduler should ignore already started tasks")
		require.Equal(t, 1, sched.taskQueue.Len())
	})
}

func TestScheduler_Cancel(t *testing.T) {
	t.Run("Fails with unrecognized task", func(t *testing.T) {
		sched := newTestScheduler(t)

		exampleTask := workflow.Task{ULID: ulid.Make(), Fragment: nil}
		require.Error(t, sched.Cancel(t.Context(), &exampleTask), "Unrecognized tasks should be rejected")
	})

	t.Run("Handler is notified of canceled tasks", func(t *testing.T) {
		var taskResult workflow.TaskResult
		handler := func(_ context.Context, _ *workflow.Task, result workflow.TaskResult) {
			taskResult = result
		}

		sched := newTestScheduler(t)

		var (
			exampleTask = &workflow.Task{ULID: ulid.Make(), Fragment: nil}
			manifest    = &workflow.Manifest{
				Tasks:               []*workflow.Task{exampleTask},
				StreamClosedHandler: nopStreamHandler,
				TaskResultHandler:   handler,
			}
		)
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest), "Scheduler should accept valid manifest")
		require.NoError(t, sched.Start(t.Context(), exampleTask), "Scheduler should start registered task")

		require.NoError(t, sched.Cancel(t.Context(), exampleTask), "Scheduler should permit canceling tasks")
		require.Equal(t, workflow.TaskOutcomeCancelled, taskResult.Outcome, "cancelled tasks should produce a cancelled result")
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
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{}), "Scheduler should accept hello message")
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		var (
			exampleTask = &workflow.Task{ULID: ulid.Make()}
			manifest    = &workflow.Manifest{
				Tasks:               []*workflow.Task{exampleTask},
				StreamClosedHandler: nopStreamHandler,
				TaskResultHandler:   nopTaskHandler,
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
		result := workflow.TaskResult{Outcome: workflow.TaskOutcomeFailed}
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should not reject ReadyMessage beyond total count")
		require.NoError(t, peer.SendMessage(ctx, wire.TaskResultMessage{ID: assignedTask.ULID, Result: result}), "Scheduler should accept TaskResultMessage")
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
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{}), "Scheduler should accept hello message")
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		var (
			exampleTask = &workflow.Task{ULID: ulid.Make()}
			manifest    = &workflow.Manifest{
				Tasks:               []*workflow.Task{exampleTask},
				StreamClosedHandler: nopStreamHandler,
				TaskResultHandler:   nopTaskHandler,
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

	t.Run("Owned tasks and their sinks terminate upon connection loss", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			taskResults := make(chan workflow.TaskResult, 1)
			streamClosed := make(chan struct{}, 1)
			taskHandler := func(_ context.Context, _ *workflow.Task, result workflow.TaskResult) {
				taskResults <- result
			}
			streamHandler := func(_ context.Context, _ *workflow.Stream) {
				streamClosed <- struct{}{}
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

			require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{}))
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}))

			stream := &workflow.Stream{ULID: ulid.Make()}
			exampleTask := &workflow.Task{
				ULID:  ulid.Make(),
				Sinks: map[physical.Node][]*workflow.Stream{nil: {stream}},
			}
			manifest := &workflow.Manifest{
				Streams:             []*workflow.Stream{stream},
				Tasks:               []*workflow.Task{exampleTask},
				StreamClosedHandler: streamHandler,
				TaskResultHandler:   taskHandler,
			}
			require.NoError(t, sched.RegisterManifest(t.Context(), manifest))
			require.NoError(t, sched.Start(t.Context(), exampleTask))

		WaitAssign:
			for {
				select {
				case <-ctx.Done():
					t.Fatal("timed out waiting for assignment")
				case msg := <-messages:
					switch msg.(type) {
					case wire.TaskAssignMessage:
						break WaitAssign
					default:
						t.Fatalf("unexpected message type %T", msg)
					}
				}
			}

			synctest.Wait()
			guard := sched.resourcesMut.RLock("test_wait_assignment_owner")
			require.NotNil(t, sched.tasks[exampleTask.ULID].owner)
			guard.RUnlock()

			require.NoError(t, conn.Close())
			select {
			case <-ctx.Done():
				t.Fatal("timed out waiting for disconnected task result")
			case result := <-taskResults:
				require.True(t, result.Outcome.Valid())
			}
			select {
			case <-ctx.Done():
				t.Fatal("timed out waiting for disconnected task sink closure")
			case <-streamClosed:
			}
		})
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
					Streams:             []*workflow.Stream{&stream},
					Tasks:               []*workflow.Task{&task},
					StreamClosedHandler: nopStreamHandler,
					TaskResultHandler:   nopTaskHandler,
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
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{}), "Scheduler should accept hello message")
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
					Streams:             []*workflow.Stream{&stream},
					Tasks:               []*workflow.Task{&task},
					StreamClosedHandler: nopStreamHandler,
					TaskResultHandler:   nopTaskHandler,
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
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{}), "Scheduler should accept hello message")
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

			// Wait for assignment.
		WaitAssign:
			for {
				select {
				case <-ctx.Done():
					require.Fail(t, "time out before receiving task")
				case msg := <-messages:
					switch msg := msg.(type) {
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
					Streams:             []*workflow.Stream{&stream},
					Tasks:               []*workflow.Task{&receiver, &sender},
					StreamClosedHandler: nopStreamHandler,
					TaskResultHandler:   nopTaskHandler,
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

			require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{}), "Scheduler should accept hello message")
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

			// Scheduler the receiver first; we'll schedule the sender once the task has been assigned.
			require.NoError(t, sched.Start(t.Context(), &receiver), "Scheduler should start registered task")

			for {
				select {
				case <-ctx.Done():
					require.Fail(t, "time out before receiving task")
				case msg := <-messages:
					switch msg := msg.(type) {
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
					Streams:             []*workflow.Stream{&stream},
					Tasks:               []*workflow.Task{&receiver, &sender},
					StreamClosedHandler: nopStreamHandler,
					TaskResultHandler:   nopTaskHandler,
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

			require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{}), "Scheduler should accept hello message")
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

			// Schedule the sender first; we'll schedule the receiver once the task has been assigned.
			require.NoError(t, sched.Start(t.Context(), &sender), "Scheduler should start registered task")

			for {
				select {
				case <-ctx.Done():
					require.Fail(t, "time out before receiving task")
				case msg := <-messages:
					switch msg := msg.(type) {
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

	t.Run("Task result propagates to handler", func(t *testing.T) {
		var taskResult atomic.Pointer[workflow.TaskResult]
		handler := func(_ context.Context, _ *workflow.Task, result workflow.TaskResult) {
			taskResult.Store(&result)
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
				Tasks:               []*workflow.Task{exampleTask},
				StreamClosedHandler: nopStreamHandler,
				TaskResultHandler:   handler,
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
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{}), "Scheduler should accept hello message")
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		// Wait for assignment.
	WaitAssign:
		for {
			select {
			case <-ctx.Done():
				require.Fail(t, "time out before receiving task")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.TaskAssignMessage:
					break WaitAssign
				default:
					require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
				}
			}
		}

		require.NoError(t, peer.SendMessage(ctx, wire.TaskResultMessage{
			ID:     exampleTask.ULID,
			Result: workflow.TaskResult{Outcome: workflow.TaskOutcomeCompleted},
		}), "Scheduler should accept task result")

		require.Equal(t, workflow.TaskOutcomeCompleted, taskResult.Load().Outcome, "handler should be notified before the result is acknowledged")

		// The first result is authoritative; a conflicting duplicate is ignored.
		require.NoError(t, peer.SendMessage(ctx, wire.TaskResultMessage{
			ID:     exampleTask.ULID,
			Result: workflow.TaskResult{Outcome: workflow.TaskOutcomeFailed, Error: context.Canceled},
		}))
		require.Equal(t, workflow.TaskOutcomeCompleted, taskResult.Load().Outcome)
	})

	t.Run("Stream closure propagates to handler", func(t *testing.T) {
		var closeNotifications atomic.Int64
		handler := func(_ context.Context, _ *workflow.Stream) {
			closeNotifications.Inc()
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
				Streams:             []*workflow.Stream{&stream},
				Tasks:               []*workflow.Task{&task},
				StreamClosedHandler: handler,
				TaskResultHandler:   nopTaskHandler,
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
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{}), "Scheduler should accept hello message")
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		// Wait for assignment.
	WaitAssign:
		for {
			select {
			case <-ctx.Done():
				require.Fail(t, "time out before receiving task")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.TaskAssignMessage:
					break WaitAssign
				default:
					require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
				}
			}
		}

		require.NoError(t, peer.SendMessage(ctx, wire.TaskResultMessage{
			ID:     task.ULID,
			Result: workflow.TaskResult{Outcome: workflow.TaskOutcomeCompleted},
		}), "Scheduler should accept producer result")
		require.Equal(t, int64(1), closeNotifications.Load(), "producer result should close its sink")
		require.Equal(t, float64(1), testutil.ToFloat64(sched.metrics.streamClosuresTotal))

		// A conflicting duplicate result must not close or notify twice.
		require.NoError(t, peer.SendMessage(ctx, wire.TaskResultMessage{
			ID:     task.ULID,
			Result: workflow.TaskResult{Outcome: workflow.TaskOutcomeFailed, Error: context.Canceled},
		}))
		require.Equal(t, int64(1), closeNotifications.Load())
		require.Equal(t, float64(1), testutil.ToFloat64(sched.metrics.streamClosuresTotal))

		// A later cancellation follows another close path but remains idempotent.
		require.NoError(t, sched.Cancel(t.Context(), &task))
		require.Equal(t, int64(1), closeNotifications.Load())
		require.Equal(t, float64(1), testutil.ToFloat64(sched.metrics.streamClosuresTotal))
	})

	t.Run("Stream closure propagates to receiver", func(t *testing.T) {
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
				Streams:             []*workflow.Stream{&stream},
				Tasks:               []*workflow.Task{&receiver, &sender},
				StreamClosedHandler: nopStreamHandler,
				TaskResultHandler:   nopTaskHandler,
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
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{}), "Scheduler should accept hello message")
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		// Wait for assignment of both tasks.
		for assigned := 0; assigned < 2; {
			select {
			case <-ctx.Done():
				require.Fail(t, "time out before receiving task")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.StreamBindMessage: // Ignore bindings
				case wire.TaskAssignMessage:
					assigned++
				default:
					require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
				}
			}
		}

		require.NoError(t, peer.SendMessage(ctx, wire.TaskResultMessage{
			ID:     sender.ULID,
			Result: workflow.TaskResult{Outcome: workflow.TaskOutcomeCompleted},
		}), "Scheduler should accept producer result")

		// Wait for the close event.
		for {
			select {
			case <-ctx.Done():
				require.Fail(t, "time out before receiving task")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.StreamBindMessage: // Ignore bindings
				case wire.StreamClosedMessage:
					require.Equal(t, stream.ULID, msg.StreamID, "unexpected stream ID")
					return
				default:
					require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
				}
			}
		}
	})

	t.Run("Receiver is assigned with already-closed sources", func(t *testing.T) {
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
				Streams:             []*workflow.Stream{&stream},
				Tasks:               []*workflow.Task{&receiver, &sender},
				StreamClosedHandler: nopStreamHandler,
				TaskResultHandler:   nopTaskHandler,
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
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{}), "Scheduler should accept hello message")
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		// Wait for task assignment.
	WaitAssign:
		for {
			select {
			case <-ctx.Done():
				require.Fail(t, "time out before receiving task")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.TaskAssignMessage:
					break WaitAssign
				default:
					require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
				}
			}
		}

		require.NoError(t, peer.SendMessage(ctx, wire.TaskResultMessage{
			ID:     sender.ULID,
			Result: workflow.TaskResult{Outcome: workflow.TaskOutcomeCompleted},
		}), "Scheduler should accept producer result")

		// Start the receiver. Its acknowledged assignment must identify the
		// source that was already closed.
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
				case wire.TaskCancelMessage: // Sender result may beat assignment finalization.
				case wire.TaskAssignMessage:
					require.Contains(t, msg.ClosedSourceIDs, stream.ULID, "already-closed source should be sent with assignment")
					return

				default:
					require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
				}
			}
		}
	})
}

func TestScheduler_RejectsWorkerStreamClosedMessages(t *testing.T) {
	sched := newTestScheduler(t)
	worker := &workerConn{ty: connectionTypeControlPlane}

	err := sched.handleMessage(t.Context(), worker, wire.StreamClosedMessage{StreamID: ulid.Make()})
	require.ErrorContains(t, err, "unsupported message kind")
}

func TestScheduler_DisconnectAfterAssignmentAckBeforeFinalize(t *testing.T) {
	var (
		result       workflow.TaskResult
		streamClosed bool
	)
	taskHandler := func(_ context.Context, _ *workflow.Task, taskResult workflow.TaskResult) {
		result = taskResult
	}
	streamHandler := func(_ context.Context, _ *workflow.Stream) {
		streamClosed = true
	}

	sched := newTestScheduler(t)
	stream := &workflow.Stream{ULID: ulid.Make()}
	workflowTask := &workflow.Task{
		ULID:  ulid.Make(),
		Sinks: map[physical.Node][]*workflow.Stream{nil: {stream}},
	}
	manifest := &workflow.Manifest{
		Streams:             []*workflow.Stream{stream},
		Tasks:               []*workflow.Task{workflowTask},
		StreamClosedHandler: streamHandler,
		TaskResultHandler:   taskHandler,
	}
	require.NoError(t, sched.RegisterManifest(t.Context(), manifest))
	require.NoError(t, sched.Start(t.Context(), workflowTask))

	tracked, err := sched.findTasks([]*workflow.Task{workflowTask})
	require.NoError(t, err)
	require.NoError(t, tracked[0].TryAssign(func() error { return nil }), "worker should accept assignment")

	// The connection closes after the assignment ACK records assignTime, but
	// before finalizeAssignment can install ownership.
	var worker workerConn
	worker.MarkClosed(context.Canceled)
	sched.finalizeAssignment(t.Context(), tracked[0], &worker, nil)

	require.Equal(t, workflow.TaskOutcomeFailed, result.Outcome)
	require.ErrorIs(t, result.Error, context.Canceled)
	require.True(t, streamClosed, "task sinks should close when finalization observes a disconnected worker")
	require.Nil(t, tracked[0].owner, "disconnected worker must not gain task ownership")
}

func TestScheduler_StreamClosesWhileAssignmentIsInFlight(t *testing.T) {
	sched := newTestScheduler(t)

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	conn, err := sched.DialFrom(ctx, wire.LocalWorker)
	require.NoError(t, err)
	defer conn.Close()

	var (
		assignmentReceived = make(chan struct{})
		allowAssignmentAck = make(chan struct{})
		closeForwarded     = make(chan ulid.ULID, 1)
	)
	peer := wire.Peer{
		Logger:  log.NewNopLogger(),
		Metrics: wire.NewMetrics(),
		Conn:    conn,
		Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
			switch msg := message.(type) {
			case wire.TaskAssignMessage:
				close(assignmentReceived)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-allowAssignmentAck:
					return nil
				}
			case wire.StreamClosedMessage:
				closeForwarded <- msg.StreamID
			}
			return nil
		},
	}
	go func() { _ = peer.Serve(ctx) }()

	require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{}))
	require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}))

	stream := &workflow.Stream{ULID: ulid.Make()}
	receiver := &workflow.Task{
		ULID:    ulid.Make(),
		Sources: map[physical.Node][]*workflow.Stream{nil: {stream}},
	}
	producer := &workflow.Task{
		ULID:  ulid.Make(),
		Sinks: map[physical.Node][]*workflow.Stream{nil: {stream}},
	}
	manifest := &workflow.Manifest{
		Streams:             []*workflow.Stream{stream},
		Tasks:               []*workflow.Task{receiver, producer},
		StreamClosedHandler: nopStreamHandler,
		TaskResultHandler:   nopTaskHandler,
	}
	require.NoError(t, sched.RegisterManifest(t.Context(), manifest))
	require.NoError(t, sched.Start(t.Context(), receiver))

	select {
	case <-ctx.Done():
		t.Fatal("timed out waiting for assignment")
	case <-assignmentReceived:
	}

	// The assignment snapshot did not include this closure, and the receiver
	// owner is not installed until the assignment ACK arrives. Cancelling the
	// producer closes its sink while the receiver assignment is in flight.
	require.NoError(t, sched.Start(t.Context(), producer))
	require.NoError(t, sched.Cancel(t.Context(), producer))
	close(allowAssignmentAck)

	select {
	case <-ctx.Done():
		t.Fatal("timed out waiting for reconciled stream closure")
	case got := <-closeForwarded:
		require.Equal(t, stream.ULID, got)
	}
}

func TestScheduler_assignmentMetricsUseBoundedLabels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
					return ctx.Err()
				case messages <- message:
					return nil
				}
			},
		}
		go func() { _ = peer.Serve(ctx) }()

		require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{}))
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}))

		exampleTask := &workflow.Task{ULID: ulid.Make()}
		manifest := &workflow.Manifest{
			Tasks:               []*workflow.Task{exampleTask},
			StreamClosedHandler: nopStreamHandler,
			TaskResultHandler:   nopTaskHandler,
		}
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest))
		require.NoError(t, sched.Start(t.Context(), exampleTask))

	WaitAssign:
		for {
			select {
			case <-ctx.Done():
				t.Fatal("timed out before assignment")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.TaskAssignMessage:
					require.Equal(t, exampleTask.ULID, msg.Task.ULID)
					break WaitAssign
				default:
					t.Fatalf("unexpected message type %T", msg)
				}
			}
		}

		synctest.Wait()
		require.Equal(t, float64(1), testutil.ToFloat64(sched.metrics.assignmentAttemptsTotal.WithLabelValues(outcomeSuccess.String())))
		require.Equal(t, float64(1), testutil.ToFloat64(sched.metrics.tasksAssignedTotal))

		requireMetricLabelsBounded(t, sched.metrics.reg, "loki_engine_scheduler_handler_phase_seconds", map[string]map[string]struct{}{
			"message_type": stringSet(wire.WorkerHelloMessage{}.Kind().String(), wire.WorkerReadyMessage{}.Kind().String()),
			"phase":        stringSet(phaseTotal),
			"outcome":      stringSet(outcomeAck),
		})
		requireMetricLabelsBounded(t, sched.metrics.reg, "loki_engine_scheduler_assignment_hop_seconds", map[string]map[string]struct{}{
			"phase": stringSet(
				"worker_ready_handler", "prepare_assignment", "tasks_ch_handoff_wait",
				"wait_assignment", "taskassign_roundtrip", "error_requeue",
				"finalize_assignment", "park_worker_wait",
			),
			"outcome": stringSet(
				outcomeAck, outcomeNack, outcomeSuccess, outcomeEmpty, outcomeAssigned,
				outcomeCanceled, outcomeConnClosed, outcomeReady, outcomeUnassignable,
				outcomeNack429, outcomeTimeout, outcomeSendError,
			),
		})

		require.GreaterOrEqual(t, histogramSampleCount(t, sched.metrics.reg, "loki_engine_scheduler_handler_phase_seconds", map[string]string{
			"message_type": wire.WorkerReadyMessage{}.Kind().String(),
			"phase":        phaseTotal.String(),
			"outcome":      outcomeAck.String(),
		}), uint64(1))
	})
}

// TestScheduler_workerParkOnBackoff verifies that a 429 from a worker parks the
// worker's assignment loop, and that a subsequent ready message resumes the
// requeued task.
func TestScheduler_workerParkOnBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sched := newTestScheduler(t)

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		conn, err := sched.DialFrom(ctx, wire.LocalWorker)
		require.NoError(t, err)
		defer conn.Close()

		var reject atomic.Bool
		reject.Store(true)
		assigns := make(chan *workflow.Task, 10)

		peer := wire.Peer{
			Logger:  log.NewNopLogger(),
			Metrics: wire.NewMetrics(),
			Conn:    conn,
			Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
				msg, ok := message.(wire.TaskAssignMessage)
				if !ok {
					return nil
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case assigns <- msg.Task:
				}
				if reject.Load() {
					return wire.Errorf(http.StatusTooManyRequests, "no threads available")
				}
				return nil
			},
		}
		go func() { _ = peer.Serve(ctx) }()

		require.NoError(t, peer.SendMessage(ctx, wire.WorkerHelloMessage{}))
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}))

		exampleTask := &workflow.Task{ULID: ulid.Make()}
		manifest := &workflow.Manifest{
			Tasks:               []*workflow.Task{exampleTask},
			StreamClosedHandler: nopStreamHandler,
			TaskResultHandler:   nopTaskHandler,
		}
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest))
		require.NoError(t, sched.Start(t.Context(), exampleTask))

		select {
		case <-ctx.Done():
			t.Fatal("timed out before first assignment attempt")
		case got := <-assigns:
			require.Equal(t, exampleTask.ULID, got.ULID)
		}

		synctest.Wait()
		require.GreaterOrEqual(t, testutil.ToFloat64(sched.metrics.backoffsTotal), float64(1))
		guard := sched.assignMut.RLock("collector_assign_stats")
		require.Len(t, sched.connectedWorkers, 1)
		guard.RUnlock()
		require.Equal(t, float64(1), testutil.ToFloat64(sched.metrics.assignmentAttemptsTotal))
		require.Equal(t, float64(1), testutil.ToFloat64(sched.metrics.assignmentAttemptsTotal.WithLabelValues(outcomeNack429.String())))

		reject.Store(false)
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}))

		select {
		case <-ctx.Done():
			t.Fatal("timed out before requeued task was reassigned")
		case got := <-assigns:
			require.Equal(t, exampleTask.ULID, got.ULID)
		}
	})
}

func newTestScheduler(t *testing.T) *Scheduler {
	t.Helper()

	sched, err := New(Config{
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

func nopStreamHandler(context.Context, *workflow.Stream)                  {}
func nopTaskHandler(context.Context, *workflow.Task, workflow.TaskResult) {}

type mockRecordWriter struct {
	writes atomic.Int64
	wrote  chan struct{}
}

func (m *mockRecordWriter) Write(_ context.Context, _ arrow.RecordBatch) error {
	m.writes.Inc()
	if m.wrote != nil {
		select {
		case m.wrote <- struct{}{}:
		default:
		}
	}
	return nil
}
