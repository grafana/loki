package worker

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

func TestWorkerSelfBootstrapsAndRearmsReadyAfter429(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		listener := &wire.Local{Address: wire.LocalScheduler}
		t.Cleanup(func() { require.NoError(t, listener.Close(context.Background())) })

		acceptedConn := make(chan wire.Conn, 1)
		acceptErr := make(chan error, 1)
		go func() {
			conn, err := listener.Accept(ctx)
			if err != nil {
				acceptErr <- err
				return
			}
			acceptedConn <- conn
		}()

		workerConn, err := listener.DialFrom(ctx, wire.LocalWorker)
		require.NoError(t, err)

		var schedulerConn wire.Conn
		select {
		case err := <-acceptErr:
			require.NoError(t, err)
		case schedulerConn = <-acceptedConn:
		}

		messages := make(chan wire.Message, 2)
		schedulerPeer := &wire.Peer{
			Logger:  log.NewNopLogger(),
			Metrics: wire.NewMetrics(),
			Conn:    schedulerConn,
			Handler: func(ctx context.Context, _ *wire.Peer, msg wire.Message) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case messages <- msg:
					return nil
				}
			},
		}
		schedulerDone := make(chan error, 1)
		go func() { schedulerDone <- schedulerPeer.Serve(ctx) }()

		w := &Worker{
			logger:      log.NewNopLogger(),
			wireMetrics: wire.NewMetrics(),
			metrics:     newMetrics(),
			jobManager:  newJobManager(),
			sources:     make(map[ulid.ULID]*streamSource),
			sinks:       make(map[ulid.ULID]*streamSink),
			jobs:        make(map[ulid.ULID]*threadJob),
		}

		firstJob := make(chan *threadJob, 1)
		firstRecvErr := make(chan error, 1)
		go func() {
			job, err := w.jobManager.Recv(ctx)
			if err != nil {
				firstRecvErr <- err
				return
			}
			firstJob <- job
		}()
		require.NoError(t, w.jobManager.WaitReady(ctx))

		workerDone := make(chan error, 1)
		go func() { workerDone <- w.handleSchedulerConn(ctx, log.NewNopLogger(), workerConn) }()

		receiveMessage := func() wire.Message {
			t.Helper()
			select {
			case <-ctx.Done():
				t.Fatal("timed out waiting for worker message")
				return nil
			case msg := <-messages:
				return msg
			}
		}

		require.IsType(t, wire.WorkerHelloMessage{}, receiveMessage())
		require.IsType(t, wire.WorkerReadyMessage{}, receiveMessage())
		synctest.Wait()
		select {
		case msg := <-messages:
			t.Fatalf("unexpected worker message after initial ready: %T", msg)
		default:
		}

		firstTask := &workflow.Task{ULID: ulid.Make(), TenantID: "test-tenant"}
		require.NoError(t, schedulerPeer.SendMessage(ctx, wire.TaskAssignMessage{Task: firstTask}))

		var acceptedJob *threadJob
		select {
		case err := <-firstRecvErr:
			require.NoError(t, err)
		case acceptedJob = <-firstJob:
		}
		acceptedJob.Cancel()
		acceptedJob.Close()

		secondTask := &workflow.Task{ULID: ulid.Make(), TenantID: "test-tenant"}
		err = schedulerPeer.SendMessage(ctx, wire.TaskAssignMessage{Task: secondTask})
		var wireErr *wire.Error
		require.ErrorAs(t, err, &wireErr)
		require.EqualValues(t, http.StatusTooManyRequests, wireErr.Code)

		guard := w.resourcesMut.RLock("test_rejected_assignment_cleanup")
		require.Empty(t, w.jobs)
		require.Empty(t, w.sources)
		require.Empty(t, w.sinks)
		guard.RUnlock()

		secondRecvErr := make(chan error, 1)
		go func() {
			_, err := w.jobManager.Recv(ctx)
			secondRecvErr <- err
		}()

		require.IsType(t, wire.WorkerReadyMessage{}, receiveMessage())
		synctest.Wait()
		select {
		case msg := <-messages:
			t.Fatalf("unexpected worker message after re-armed ready: %T", msg)
		default:
		}

		cancel()
		synctest.Wait()

		require.ErrorIs(t, <-secondRecvErr, context.Canceled)
		for _, err := range []error{<-workerDone, <-schedulerDone} {
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, wire.ErrConnClosed) {
				require.NoError(t, err)
			}
		}
	})
}
