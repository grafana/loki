package worker_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v4/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v4/pkg/engine/internal/scheduler"
	"github.com/grafana/loki/v4/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v4/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v4/pkg/engine/internal/worker"
	"github.com/grafana/loki/v4/pkg/engine/internal/workflow"
)

var errReleased = errors.New("bucket released")

// blockingBucket blocks every read until release is closed, then fails the
// read with errReleased. A read fails with the context error if its context
// ends first. It closes started when the first read arrives.
type blockingBucket struct {
	objstore.Bucket

	started chan struct{}
	release chan struct{}

	startedOnce sync.Once
}

func (b *blockingBucket) block(ctx context.Context) error {
	b.startedOnce.Do(func() { close(b.started) })

	select {
	case <-b.release:
		return errReleased
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *blockingBucket) Attributes(ctx context.Context, _ string) (objstore.ObjectAttributes, error) {
	return objstore.ObjectAttributes{}, b.block(ctx)
}

func (b *blockingBucket) Get(ctx context.Context, _ string) (io.ReadCloser, error) {
	return nil, b.block(ctx)
}

func (b *blockingBucket) GetRange(ctx context.Context, _ string, _, _ int64) (io.ReadCloser, error) {
	return nil, b.block(ctx)
}

// TestWorkerGracefulShutdown tests that the worker finishes a running task
// after its context is canceled.
func TestWorkerGracefulShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		schedulerListener := &wire.Local{Address: wire.LocalScheduler}
		workerListener := &wire.Local{Address: wire.LocalWorker}

		sched, err := scheduler.New(scheduler.Config{Logger: log.NewNopLogger(), Listener: schedulerListener})
		require.NoError(t, err)
		require.NoError(t, services.StartAndAwaitRunning(t.Context(), sched.Service()))
		t.Cleanup(func() {
			require.NoError(t, services.StopAndAwaitTerminated(context.WithoutCancel(t.Context()), sched.Service()))
		})

		bucket := &blockingBucket{
			Bucket:  objstore.NewInMemBucket(),
			started: make(chan struct{}),
			release: make(chan struct{}),
		}

		runCtx, cancel := context.WithCancel(t.Context())
		defer cancel()

		w, err := worker.New(worker.Config{
			Logger:           log.NewNopLogger(),
			Bucket:           bucket,
			Dialer:           wire.NewLocalDialer(schedulerListener, workerListener),
			Listener:         workerListener,
			SchedulerAddress: wire.LocalScheduler,
			NumThreads:       1,
		})
		require.NoError(t, err)
		require.NoError(t, services.StartAndAwaitRunning(runCtx, w.Service()))
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
			defer cancel()
			require.NoError(t, services.StopAndAwaitTerminated(ctx, w.Service()))
		})

		node := &physical.IndexFilter{
			NodeID:          ulid.Make(),
			Tenant:          "tenant",
			SourceIndexPath: "index/source",
			ObjectPaths:     []string{"objects/a"},
		}
		var graph dag.Graph[physical.Node]
		graph.Add(node)

		resultsStream := &workflow.Stream{ULID: ulid.Make()}
		task := &workflow.Task{
			ULID:     ulid.Make(),
			TenantID: "tenant",
			Fragment: physical.FromGraph(graph),
			Sources:  map[physical.Node][]*workflow.Stream{},
			Sinks:    map[physical.Node][]*workflow.Stream{node: {resultsStream}},
		}

		results := make(chan workflow.TaskResult, 1)
		manifest := &workflow.Manifest{
			Streams: []*workflow.Stream{resultsStream},
			Tasks:   []*workflow.Task{task},
			TaskResultHandler: func(_ context.Context, _ *workflow.Task, result workflow.TaskResult) {
				results <- result
			},
			StreamClosedHandler: func(context.Context, *workflow.Stream) {},
		}
		require.NoError(t, sched.RegisterManifest(t.Context(), manifest))
		require.NoError(t, sched.Listen(t.Context(), discardRecordWriter{}, resultsStream))
		require.NoError(t, sched.Start(t.Context(), task))

		synctest.Wait()
		select {
		case <-bucket.started:
		default:
			t.Fatal("task did not start reading from the bucket")
		}

		cancel()
		synctest.Wait()
		require.Empty(t, results, "task must keep running after the worker context is canceled")

		close(bucket.release)
		synctest.Wait()

		select {
		case result := <-results:
			require.Equal(t, workflow.TaskOutcomeFailed, result.Outcome)
			require.ErrorIs(t, result.Error, errReleased)
		default:
			t.Fatal("task did not report a result after the worker context was canceled")
		}
		require.Equal(t, services.Terminated, w.Service().State())
	})
}

// discardRecordWriter is a [workflow.RecordWriter] that drops all records.
type discardRecordWriter struct{}

func (discardRecordWriter) Write(context.Context, arrow.RecordBatch) error { return nil }
