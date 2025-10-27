package scheduler

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

func TestScheduler_AddStreams(t *testing.T) {
	t.Run("Succeeds with new stream", func(t *testing.T) {
		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream))
	})

	t.Run("Fails with existing stream", func(t *testing.T) {
		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream))
		require.Error(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should reject existing stream")
	})

	t.Run("Fails with zero ULID", func(t *testing.T) {
		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Zero}
		require.Error(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should reject zero-value ULID")
	})
}

func TestScheduler_RemoveStreams(t *testing.T) {
	t.Run("Fails with unrecognized stream", func(t *testing.T) {
		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.Error(t, sched.RemoveStreams(t.Context(), &stream), "Scheduler should reject unrecognized stream")
	})

	t.Run("Succeeds with recognized stream", func(t *testing.T) {
		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream))
		require.NoError(t, sched.RemoveStreams(t.Context(), &stream))
	})

	t.Run("Fails with removed stream", func(t *testing.T) {
		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream))
		require.NoError(t, sched.RemoveStreams(t.Context(), &stream))
		require.Error(t, sched.RemoveStreams(t.Context(), &stream), "Scheduler should reject already removed stream")
	})

	t.Run("Fails with stream that has active tasks", func(t *testing.T) {
		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should accept new stream")

		var (
			receiver = workflow.Task{
				ULID:     ulid.Make(),
				Fragment: nil,

				Sources: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}
			sender = workflow.Task{
				ULID:     ulid.Make(),
				Fragment: nil,

				Sinks: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}
		)

		require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &receiver, &sender), "Scheduler should accept new tasks")
		require.Error(t, sched.RemoveStreams(t.Context(), &stream), "Scheduler should reject stream with active tasks")

		require.NoError(t, sched.Cancel(t.Context(), &receiver, &sender), "Scheduler should allow cancellation of active tasks")
		require.NoError(t, sched.RemoveStreams(t.Context(), &stream), "Scheduler should allow removing stream with inactive tasks")
	})

	t.Run("Succeeds with stream with listener and inactive tasks", func(t *testing.T) {
		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should accept new stream")

		var (
			sender = workflow.Task{
				ULID:     ulid.Make(),
				Fragment: nil,

				Sinks: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}
		)

		// Only active *tasks* should prevent from removing a stream. An active
		// listener should not.
		listener, err := sched.Listen(t.Context(), &stream)
		require.NoError(t, err)
		defer listener.Close()

		require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &sender), "Scheduler should accept new tasks")
		require.Error(t, sched.RemoveStreams(t.Context(), &stream), "Scheduler should reject stream with active tasks")

		require.NoError(t, sched.Cancel(t.Context(), &sender), "Scheduler should allow cancellation of active tasks")
		require.NoError(t, sched.RemoveStreams(t.Context(), &stream), "Scheduler should allow removing stream with inactive tasks")
	})

	t.Run("Streams move to closed state before removal", func(t *testing.T) {
		var streamState workflow.StreamState
		handler := func(_ context.Context, _ *workflow.Stream, newState workflow.StreamState) {
			streamState = newState
		}

		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.NoError(t, sched.AddStreams(t.Context(), handler, &stream), "Scheduler should accept new stream")
		require.NoError(t, sched.RemoveStreams(t.Context(), &stream), "Scheduler should allow removing stream")

		require.Equal(t, workflow.StreamStateClosed, streamState)
	})
}

func TestScheduler_Listen(t *testing.T) {
	t.Run("Succeeds on stream with no receiver or listener", func(t *testing.T) {
		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should accept new stream")

		listener, err := sched.Listen(t.Context(), &stream)
		require.NoError(t, err, "Listen should succeed on stream with no receiver or listener")
		listener.Close()
	})

	t.Run("Fails on stream with receiver", func(t *testing.T) {
		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should accept new stream")

		var (
			receiver = workflow.Task{
				ULID:     ulid.Make(),
				Fragment: nil,

				Sources: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}
		)

		require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &receiver))

		_, err := sched.Listen(t.Context(), &stream)
		require.Error(t, err, "Listen should fail on stream with receiver")
	})

	t.Run("Fails on stream with listener", func(t *testing.T) {
		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should accept new stream")

		listener, err := sched.Listen(t.Context(), &stream)
		require.NoError(t, err, "Listen should succeed on stream with no listener or receiver")
		defer listener.Close()

		_, err = sched.Listen(t.Context(), &stream)
		require.Error(t, err, "Listen should fail on stream with listener")
	})

	t.Run("Data is sent to listener", func(t *testing.T) {
		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should accept new stream")

		listener, err := sched.Listen(t.Context(), &stream)
		require.NoError(t, err, "Listen should succeed on stream with no listener or receiver")
		defer listener.Close()

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		var wg sync.WaitGroup
		wg.Add(1)

		go func() {
			defer wg.Done()

			// Create a fake worker to send a message to the scheduler.
			conn, err := sched.DialFrom(ctx, wire.LocalWorker)
			require.NoError(t, err, "Should be able to connect to scheduler")
			defer conn.Close()

			peer := wire.Peer{
				Logger:  log.NewNopLogger(),
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
		}()

		_, err = listener.Read(ctx)
		require.NoError(t, err, "Data should be forwarded to listener")

		wg.Wait()
	})

	t.Run("Closing the stream closes the listener", func(t *testing.T) {
		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should accept new stream")

		listener, err := sched.Listen(t.Context(), &stream)
		require.NoError(t, err, "Listen should succeed on stream with no listener or receiver")
		defer listener.Close()

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		go func() {
			require.NoError(t, sched.RemoveStreams(ctx, &stream), "Scheduler should accept stream removal")
		}()

		_, err = listener.Read(ctx)
		require.ErrorIs(t, err, executor.EOF, "Read should exit with EOF")
	})

	t.Run("Stream is automatically closed with terminated sender", func(t *testing.T) {
		var streamState workflow.StreamState
		handler := func(_ context.Context, _ *workflow.Stream, newState workflow.StreamState) {
			streamState = newState
		}

		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.NoError(t, sched.AddStreams(t.Context(), handler, &stream), "Scheduler should accept new stream")

		var (
			sender = workflow.Task{
				ULID:     ulid.Make(),
				Fragment: nil,

				Sinks: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}
		)
		require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &sender), "Scheduler should accept new tasks")

		// Cancel the tasks; this should cause the stream the close.
		require.NoError(t, sched.Cancel(t.Context(), &sender), "Scheduler should allow cancelling tasks")
		require.Equal(t, workflow.StreamStateClosed, streamState)
	})
}

func TestScheduler_Start(t *testing.T) {
	t.Run("Fails with unrecognized stream", func(t *testing.T) {
		sched := newTestScheduler(t)

		// Create a stream but don't add it.
		stream := workflow.Stream{ULID: ulid.Make()}

		var (
			receiver = workflow.Task{
				ULID:     ulid.Make(),
				Fragment: nil,

				Sources: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}
			sender = workflow.Task{
				ULID:     ulid.Make(),
				Fragment: nil,

				Sinks: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}
		)

		// We call Start twice below, to test sources and sinks separately.
		require.Error(t, sched.Start(t.Context(), nopTaskHandler, &receiver), "Scheduler should not accept task using unrecognized source")
		require.Error(t, sched.Start(t.Context(), nopTaskHandler, &sender), "Scheduler should not accept task using unrecognized sink")
	})

	t.Run("Fails with bound stream (sink)", func(t *testing.T) {
		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should accept new stream")

		var (
			sender1 = workflow.Task{
				ULID:     ulid.Make(),
				Fragment: nil,

				Sinks: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}
			sender2 = workflow.Task{
				ULID:     ulid.Make(),
				Fragment: nil,

				Sinks: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}
		)

		require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &sender1), "Scheduler should accept task")
		require.Error(t, sched.Start(t.Context(), nopTaskHandler, &sender2), "Scheduler should reject task with bound sink")
	})

	t.Run("Fails with bound stream (source)", func(t *testing.T) {
		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should accept new stream")

		var (
			receiver1 = workflow.Task{
				ULID:     ulid.Make(),
				Fragment: nil,

				Sources: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}
			receiver2 = workflow.Task{
				ULID:     ulid.Make(),
				Fragment: nil,

				Sources: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}
		)

		require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &receiver1), "Scheduler should accept task")
		require.Error(t, sched.Start(t.Context(), nopTaskHandler, &receiver2), "Scheduler should reject task with bound source")
	})

	t.Run("Fails with bound listener (source)", func(t *testing.T) {
		sched := newTestScheduler(t)

		stream := workflow.Stream{ULID: ulid.Make()}
		require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should accept new stream")

		var (
			receiver = workflow.Task{
				ULID:     ulid.Make(),
				Fragment: nil,

				Sources: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}
		)

		listener, err := sched.Listen(t.Context(), &stream)
		require.NoError(t, err, "Listen should succeed")
		defer listener.Close()

		require.Error(t, sched.Start(t.Context(), nopTaskHandler, &receiver), "Scheduler should reject task with bound source")
	})

	t.Run("New tasks are moved to pending state", func(t *testing.T) {
		var taskStatus workflow.TaskStatus
		handler := func(_ context.Context, _ *workflow.Task, newStatus workflow.TaskStatus) {
			taskStatus = newStatus
		}

		sched := newTestScheduler(t)

		exampleTask := workflow.Task{ULID: ulid.Make(), Fragment: nil}
		require.NoError(t, sched.Start(t.Context(), handler, &exampleTask), "Scheduler should accept new tasks")
		require.Equal(t, workflow.TaskStatePending, taskStatus.State, "Started tasks should move to pending state")
	})

	t.Run("Fails with existing task", func(t *testing.T) {
		sched := newTestScheduler(t)

		exampleTask := workflow.Task{ULID: ulid.Make(), Fragment: nil}
		require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &exampleTask), "Scheduler should accept new tasks")
		require.Error(t, sched.Start(t.Context(), nopTaskHandler, &exampleTask), "Scheduler should reject existing tasks")
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

		exampleTask := workflow.Task{ULID: ulid.Make(), Fragment: nil}
		require.NoError(t, sched.Start(t.Context(), handler, &exampleTask), "Scheduler should accept new tasks")
		require.NoError(t, sched.Cancel(t.Context(), &exampleTask), "Scheduler should permit canceling tasks")

		require.Equal(t, workflow.TaskStateCancelled, taskStatus.State, "Canceled tasks should be in the canceled state")
	})
}

func TestScheduler_worker(t *testing.T) {
	t.Run("Tasks are assigned to ready worker", func(t *testing.T) {
		sched := newTestScheduler(t)

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		conn, err := sched.DialFrom(ctx, wire.LocalWorker)
		require.NoError(t, err)
		defer conn.Close()

		messages := make(chan wire.Message, 10)

		peer := wire.Peer{
			Logger: log.NewNopLogger(),
			Conn:   conn,
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
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		exampleTask := workflow.Task{ULID: ulid.Make()}
		require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &exampleTask), "Scheduler should accept new task")

		select {
		case <-ctx.Done():
			require.Fail(t, "time out before receiving task")
		case msg := <-messages:
			switch msg := msg.(type) {
			case wire.TaskAssignMessage:
				require.Equal(t, exampleTask.ULID, msg.Task.ULID, "Should have been assigned expected task")
			default:
				require.Fail(t, "Unexpected message type %T", msg)
			}
		}
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
			Logger: log.NewNopLogger(),
			Conn:   conn,
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
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		exampleTask := workflow.Task{ULID: ulid.Make()}
		require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &exampleTask), "Scheduler should accept new task")

		// Wait for assignment.
		select {
		case <-ctx.Done():
			require.Fail(t, "time out before receiving expected message")
		case msg := <-messages:
			switch msg := msg.(type) {
			case wire.TaskAssignMessage:
			default:
				require.Fail(t, "Unexpected message type %T", msg)
			}
		}

		require.NoError(t, sched.Cancel(t.Context(), &exampleTask), "Scheduler should permit cancelation of task")

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
			Logger: log.NewNopLogger(),
			Conn:   conn,
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
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		exampleTask := workflow.Task{ULID: ulid.Make()}
		require.NoError(t, sched.Start(t.Context(), handler, &exampleTask), "Scheduler should accept new task")

		// Wait for assignment.
		select {
		case <-ctx.Done():
			require.Fail(t, "time out before receiving expected message")
		case msg := <-messages:
			switch msg := msg.(type) {
			case wire.TaskAssignMessage:
			default:
				require.Fail(t, "Unexpected message type %T", msg)
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
			)

			require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should accept new stream")
			require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &task), "Scheduler should accept new task")

			listener, err := sched.Listen(ctx, &stream)
			require.NoError(t, err, "Scheduler should permit listening to stream")
			defer listener.Close()

			messages := make(chan wire.Message, 10)

			peer := wire.Peer{
				Logger: log.NewNopLogger(),
				Conn:   conn,
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
			)

			require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should accept new stream")
			require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &task), "Scheduler should accept new task")

			messages := make(chan wire.Message, 10)

			peer := wire.Peer{
				Logger: log.NewNopLogger(),
				Conn:   conn,
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
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

			// Wait for assignment.
			select {
			case <-ctx.Done():
				require.Fail(t, "time out before receiving task")
			case msg := <-messages:
				switch msg := msg.(type) {
				case wire.TaskAssignMessage:
				default:
					require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
				}
			}

			// Listen to the stream, then wait for the binding.
			listener, err := sched.Listen(ctx, &stream)
			require.NoError(t, err, "Scheduler should permit listening to stream")
			defer listener.Close()

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
					ULID: ulid.Make(),

					Sources: map[physical.Node][]*workflow.Stream{
						nil: {&stream},
					},
				}

				sender = workflow.Task{
					ULID: ulid.Make(),

					Sinks: map[physical.Node][]*workflow.Stream{
						nil: {&stream},
					},
				}
			)

			require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should accept new stream")

			messages := make(chan wire.Message, 10)

			peer := wire.Peer{
				Logger: log.NewNopLogger(),
				Conn:   conn,
				Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
					select {
					case <-ctx.Done():
					case messages <- message:
					}
					return nil
				},
			}
			go func() { _ = peer.Serve(ctx) }()

			// Send two ready messages (so we can accept two tasks).
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

			// Scheduler the receiver first; we'll schedule the sender once the task has been assigned.
			require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &receiver), "Scheduler should accept new task")

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
							require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &sender), "Scheduler should accept new task")
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
					ULID: ulid.Make(),

					Sources: map[physical.Node][]*workflow.Stream{
						nil: {&stream},
					},
				}

				sender = workflow.Task{
					ULID: ulid.Make(),

					Sinks: map[physical.Node][]*workflow.Stream{
						nil: {&stream},
					},
				}
			)

			require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should accept new stream")

			messages := make(chan wire.Message, 10)

			peer := wire.Peer{
				Logger: log.NewNopLogger(),
				Conn:   conn,
				Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
					select {
					case <-ctx.Done():
					case messages <- message:
					}
					return nil
				},
			}
			go func() { _ = peer.Serve(ctx) }()

			// Send two ready messages (so we can accept two tasks).
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")
			require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

			// Scheduler the sender first; we'll schedule the receiver once the task has been assigned.
			require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &sender), "Scheduler should accept new task")

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
							require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &receiver), "Scheduler should accept new task")
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

		exampleTask := workflow.Task{ULID: ulid.Make()}
		require.NoError(t, sched.Start(t.Context(), handler, &exampleTask), "Scheduler should accept new task")

		messages := make(chan wire.Message, 10)

		peer := wire.Peer{
			Logger: log.NewNopLogger(),
			Conn:   conn,
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
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		// Wait for assignment.
		select {
		case <-ctx.Done():
			require.Fail(t, "time out before receiving task")
		case msg := <-messages:
			switch msg := msg.(type) {
			case wire.TaskAssignMessage:
			default:
				require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
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
		)
		require.NoError(t, sched.AddStreams(t.Context(), handler, &stream), "Scheduler should accept new stream")
		require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &task), "Scheduler should accept new task")

		messages := make(chan wire.Message, 10)

		peer := wire.Peer{
			Logger: log.NewNopLogger(),
			Conn:   conn,
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
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		// Wait for assignment.
		select {
		case <-ctx.Done():
			require.Fail(t, "time out before receiving task")
		case msg := <-messages:
			switch msg := msg.(type) {
			case wire.TaskAssignMessage:
			default:
				require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
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
			stream   = workflow.Stream{ULID: ulid.Make()}
			receiver = workflow.Task{
				ULID: ulid.Make(),

				Sources: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}
			sender = workflow.Task{
				ULID: ulid.Make(),

				Sinks: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}
		)
		require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should accept new stream")
		require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &receiver, &sender), "Scheduler should accept new task")

		messages := make(chan wire.Message, 10)

		peer := wire.Peer{
			Logger: log.NewNopLogger(),
			Conn:   conn,
			Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
				select {
				case <-ctx.Done():
				case messages <- message:
				}
				return nil
			},
		}
		go func() { _ = peer.Serve(ctx) }()

		// Send two ready messages so we get the tasks.
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")
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
			sender = workflow.Task{
				ULID: ulid.Make(),

				Sinks: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}
		)
		require.NoError(t, sched.AddStreams(t.Context(), nopStreamHandler, &stream), "Scheduler should accept new stream")
		require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &sender), "Scheduler should accept new task")

		messages := make(chan wire.Message, 10)

		peer := wire.Peer{
			Logger: log.NewNopLogger(),
			Conn:   conn,
			Handler: func(ctx context.Context, _ *wire.Peer, message wire.Message) error {
				select {
				case <-ctx.Done():
				case messages <- message:
				}
				return nil
			},
		}
		go func() { _ = peer.Serve(ctx) }()

		// Send two ready messages so we get the tasks.
		require.NoError(t, peer.SendMessage(ctx, wire.WorkerReadyMessage{}), "Scheduler should accept ready message")

		// Wait for task assignment.
		select {
		case <-ctx.Done():
			require.Fail(t, "time out before receiving task")
		case msg := <-messages:
			switch msg := msg.(type) {
			case wire.TaskAssignMessage:
			default:
				require.Fail(t, "Unexpected message", "Unexpected message type %T", msg)
			}
		}

		require.NoError(t, peer.SendMessage(ctx, wire.StreamStatusMessage{
			StreamID: stream.ULID,
			State:    workflow.StreamStateOpen,
		}), "Scheduler should accept state change")

		// Create the receiver. It should be assigned with a message indicating
		// the existing state of the stream it uses.
		var (
			receiver = workflow.Task{
				ULID: ulid.Make(),

				Sources: map[physical.Node][]*workflow.Stream{
					nil: {&stream},
				},
			}
		)

		require.NoError(t, sched.Start(t.Context(), nopTaskHandler, &receiver), "Scheduler should accept new task")
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

func newTestScheduler(t *testing.T) *Scheduler {
	t.Helper()

	sched, err := New(Config{
		Logger: log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr)),
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

func nopStreamHandler(context.Context, *workflow.Stream, workflow.StreamState) {}
func nopTaskHandler(context.Context, *workflow.Task, workflow.TaskStatus)      {}
