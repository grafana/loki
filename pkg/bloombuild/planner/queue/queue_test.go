package queue

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/bloombuild/planner/plannertest"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	"github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

type taskMeta struct {
	stat1 int
	stat2 string
}

type taskWithMeta struct {
	*protos.ProtoTask
	*taskMeta
}

func createTasks(n int) []*taskWithMeta {
	tasks := make([]*taskWithMeta, 0, n)
	// Enqueue tasks
	for i := 0; i < n; i++ {
		task := &taskWithMeta{
			ProtoTask: protos.NewTask(
				config.NewDayTable(plannertest.TestDay, "fake"),
				"fakeTenant",
				v1.NewBounds(model.Fingerprint(i), model.Fingerprint(i+10)),
				plannertest.TsdbID(1),
				[]protos.Gap{
					{
						Bounds: v1.NewBounds(0, 10),
						Series: plannertest.GenSeries(v1.NewBounds(0, 10)),
						Blocks: []bloomshipper.BlockRef{
							plannertest.GenBlockRef(0, 5),
							plannertest.GenBlockRef(6, 10),
						},
					},
				},
			).ToProtoTask(),
			taskMeta: &taskMeta{stat1: i, stat2: fmt.Sprintf("task-%d", i)},
		}
		tasks = append(tasks, task)
	}
	return tasks
}

func TestQueue(t *testing.T) {
	for _, tc := range []struct {
		name    string
		useDisk bool
	}{
		{
			name:    "in-memory",
			useDisk: false,
		},
		{
			name:    "on-disk",
			useDisk: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := log.NewNopLogger()
			//logger := log.NewLogfmtLogger(os.Stdout)

			taskPath := t.TempDir()
			count, err := filesInDir(taskPath)
			require.NoError(t, err)
			require.Equal(t, 0, count)

			// Create 10 random files that should be deleted on startup
			if tc.useDisk {
				createFiles(taskPath, 10)
			}

			clientMetrics := storage.NewClientMetrics()
			defer clientMetrics.Unregister()
			queueMetrics := NewMetrics(prometheus.NewPedanticRegistry(), "test", "queue")
			cfg := Config{
				MaxQueuedTasksPerTenant: 1000,
				StoreTasksOnDisk:        tc.useDisk,
				TasksDiskDirectory:      taskPath,
				CleanTasksDirectory:     true,
			}

			queue, err := NewQueue(logger, cfg, fakeLimits{}, queueMetrics, clientMetrics)
			require.NoError(t, err)

			err = services.StartAndAwaitRunning(context.Background(), queue)
			require.NoError(t, err)

			// Previously written files should be deleted
			if tc.useDisk {
				count, err = filesInDir(taskPath)
				require.NoError(t, err)
				require.Equal(t, 0, count)
			}

			const consumer = "fakeConsumer"
			queue.RegisterConsumerConnection(consumer)
			defer queue.UnregisterConsumerConnection(consumer)

			// Write some tasks to the queue
			tasks := createTasks(10)
			for _, task := range tasks {
				err = queue.Enqueue(task.ProtoTask, task.taskMeta, nil)
				require.NoError(t, err)
			}

			// There should be 10 task pending
			require.Equal(t, len(tasks), queue.TotalPending())
			count, err = filesInDir(taskPath)
			require.NoError(t, err)
			if tc.useDisk {
				require.Equal(t, len(tasks), count)
			} else {
				require.Equal(t, 0, count)
			}

			idx := StartIndex
			const nDequeue = 5
			var dequeuedTasks []*taskWithMeta
			for i := 0; i < nDequeue; i++ {
				var task *protos.ProtoTask
				var meta any
				task, meta, idx, err = queue.Dequeue(context.Background(), idx, consumer)
				require.NoError(t, err)
				require.NotNil(t, task)
				require.NotNil(t, meta)

				require.Equal(t, task, tasks[i].ProtoTask)
				require.Equal(t, meta.(*taskMeta), tasks[i].taskMeta)

				dequeuedTasks = append(dequeuedTasks, &taskWithMeta{ProtoTask: task, taskMeta: meta.(*taskMeta)})
			}

			// The task files should still be there
			require.Equal(t, len(tasks), queue.TotalPending())
			count, err = filesInDir(taskPath)
			require.NoError(t, err)
			if tc.useDisk {
				require.Equal(t, len(tasks), count)
			} else {
				require.Equal(t, 0, count)
			}

			// Release the tasks that were dequeued
			for _, task := range dequeuedTasks {
				queue.Release(task.ProtoTask)
			}

			// The task files should be gone
			require.Equal(t, len(tasks)-nDequeue, queue.TotalPending())
			count, err = filesInDir(taskPath)
			require.NoError(t, err)
			if tc.useDisk {
				require.Equal(t, len(tasks)-nDequeue, count)
			} else {
				require.Equal(t, 0, count)
			}
		})
	}
}

func filesInDir(path string) (int, error) {
	var count int

	if err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			count++
		}
		return nil
	}); err != nil {
		return 0, err
	}

	return count, nil
}

func createFiles(path string, n int) {
	for i := 0; i < n; i++ {
		file, err := os.Create(filepath.Join(path, fmt.Sprintf("file-%d", i)))
		if err != nil {
			panic(err)
		}
		_ = file.Close()
	}
}

type fakeLimits struct{}

func (f fakeLimits) MaxConsumers(_ string, _ int) int {
	return 0 // Unlimited
}

// TestQueueLengthMetricStaysNonNegativeWhileDraining is a regression test for the
// scenario where a tenant's bloom build backlog is drained by builders for longer
// than the active-users inactivity timeout without any new enqueues.
//
// Previously the cleanup ticker would DeleteLabelValues the per-tenant queue_length
// gauge series mid-drain (because UpdateUserTimestamp was only called from Enqueue),
// after which subsequent Dequeue() calls would lazily recreate the series at zero
// and decrement it into negative values. See https://github.com/grafana/loki/issues/19490.
func TestQueueLengthMetricStaysNonNegativeWhileDraining(t *testing.T) {
	const (
		cleanupInterval = 10 * time.Millisecond
		inactiveTimeout = 50 * time.Millisecond
		dequeueInterval = 30 * time.Millisecond
		numTasks        = 5
	)

	reg := prometheus.NewPedanticRegistry()
	queueMetrics := NewMetrics(reg, "test", "queue")
	clientMetrics := storage.NewClientMetrics()
	defer clientMetrics.Unregister()

	cfg := Config{MaxQueuedTasksPerTenant: 1000}
	q, err := newQueue(log.NewNopLogger(), cfg, fakeLimits{}, queueMetrics, clientMetrics, cleanupInterval, inactiveTimeout)
	require.NoError(t, err)

	require.NoError(t, services.StartAndAwaitRunning(context.Background(), q))
	t.Cleanup(func() { _ = services.StopAndAwaitTerminated(context.Background(), q) })

	const consumer = "fakeConsumer"
	q.RegisterConsumerConnection(consumer)
	defer q.UnregisterConsumerConnection(consumer)

	for _, task := range createTasks(numTasks) {
		require.NoError(t, q.Enqueue(task.ProtoTask, task.taskMeta, nil))
	}

	idx := StartIndex
	for i := 0; i < numTasks; i++ {
		time.Sleep(dequeueInterval)
		var task *protos.ProtoTask
		task, _, idx, err = q.Dequeue(context.Background(), idx, consumer)
		require.NoError(t, err)
		require.NotNil(t, task)
		q.Release(task)
	}

	// numTasks * dequeueInterval == 150ms, well past inactiveTimeout (50ms), so the
	// cleanup ticker (10ms) had many opportunities to purge "fakeTenant". With the
	// fix in place each Dequeue/Release refreshes the timestamp, so the gauge series
	// for the tenant is preserved and ends at exactly zero after the drain.
	expected := `# HELP test_queue_queue_length Number of queries in the queue.
# TYPE test_queue_queue_length gauge
test_queue_queue_length{user="fakeTenant"} 0
`
	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(expected), "test_queue_queue_length"))
}
