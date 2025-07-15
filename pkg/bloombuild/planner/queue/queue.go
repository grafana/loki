package queue

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	"github.com/grafana/loki/v3/pkg/queue"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util"
)

// Queue is a wrapper of queue.RequestQueue that uses the file system to store the pending tasks.
// The queue also allows to store metadata with the task. This metadata can be anything. Metadata is stored in memory.
// When a task is enqueued (Enqueue), it's stored in the file system and recorded as pending.
// When it's dequeued (Dequeue), it's removed from the queue but kept in FS until released (Release).
//
// TODO(salvacorts): In the future we may reuse this queue to store any proto message. We would need to use generics for that.
type Queue struct {
	services.Service

	// queue is the in-memory queue where the tasks are stored.
	// If cfg.StoreTasksOnDisk is false, we store the whole task here and the metadata.
	// Otherwise, we store the task path and the metadata.
	queue *queue.RequestQueue
	// pendingTasks is a map of task ID to the file where the task is stored.
	// If cfg.StoreTasksOnDisk is false, the value is empty.
	pendingTasks sync.Map
	activeUsers  *util.ActiveUsersCleanupService

	cfg    Config
	logger log.Logger
	disk   client.ObjectClient

	// Subservices manager.
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher
}

func NewQueue(
	logger log.Logger,
	cfg Config,
	limits Limits,
	metrics *Metrics,
	storeMetrics storage.ClientMetrics,
) (*Queue, error) {
	// Configure the filesystem client if we are storing tasks on disk.
	var diskClient client.ObjectClient
	if cfg.StoreTasksOnDisk {
		storeCfg := storage.Config{
			FSConfig: local.FSConfig{Directory: cfg.TasksDiskDirectory},
		}
		var err error
		diskClient, err = storage.NewObjectClient(types.StorageTypeFileSystem, "bloom-planner-queue", storeCfg, storeMetrics)
		if err != nil {
			return nil, fmt.Errorf("failed to create object client: %w", err)
		}
	}

	tasksQueue := queue.NewRequestQueue(cfg.MaxQueuedTasksPerTenant, 0, limits, metrics)

	// Clean metrics for inactive users: do not have added tasks to the queue in the last 1 hour
	activeUsers := util.NewActiveUsersCleanupService(5*time.Minute, 1*time.Hour, func(user string) {
		metrics.Cleanup(user)
	})

	svcs := []services.Service{tasksQueue, activeUsers}
	subservices, err := services.NewManager(svcs...)
	if err != nil {
		return nil, fmt.Errorf("failed to create subservices manager: %w", err)
	}
	subservicesWatcher := services.NewFailureWatcher()
	subservicesWatcher.WatchManager(subservices)

	q := &Queue{
		queue:              tasksQueue,
		activeUsers:        activeUsers,
		cfg:                cfg,
		logger:             logger,
		disk:               diskClient,
		subservices:        subservices,
		subservicesWatcher: subservicesWatcher,
	}

	q.Service = services.NewIdleService(q.starting, q.stopping)

	return q, nil
}

func (q *Queue) starting(ctx context.Context) error {
	// TODO(salvacorts): We do not recover the queue from the disk yet.
	// Until then, we just remove all the files in the directory so the disk
	// doesn't grow indefinitely.
	if q.cfg.StoreTasksOnDisk && q.cfg.CleanTasksDirectory {
		if err := q.cleanTasksOnDisk(ctx); err != nil {
			return fmt.Errorf("failed to clean tasks on disk during startup: %w", err)
		}
	}

	if err := services.StartManagerAndAwaitHealthy(ctx, q.subservices); err != nil {
		return fmt.Errorf("failed to start task queue subservices: %w", err)
	}

	return nil
}

func (q *Queue) cleanTasksOnDisk(ctx context.Context) error {
	objects, _, err := q.disk.List(ctx, "", "")
	if err != nil {
		return fmt.Errorf("failed to list tasks: %w", err)
	}

	for _, o := range objects {
		if err = q.disk.DeleteObject(ctx, o.Key); err != nil {
			return fmt.Errorf("failed to delete task (%s)", o.Key)
		}
	}

	return nil
}

func (q *Queue) stopping(_ error) error {
	if err := services.StopManagerAndAwaitStopped(context.Background(), q.subservices); err != nil {
		return fmt.Errorf("failed to stop task queue subservices: %w", err)
	}

	return nil
}

func (q *Queue) GetConnectedConsumersMetric() float64 {
	return q.queue.GetConnectedConsumersMetric()
}

func (q *Queue) NotifyConsumerShutdown(consumer string) {
	q.queue.NotifyConsumerShutdown(consumer)
}

func (q *Queue) RegisterConsumerConnection(consumer string) {
	q.queue.RegisterConsumerConnection(consumer)
}
func (q *Queue) UnregisterConsumerConnection(consumer string) {
	q.queue.UnregisterConsumerConnection(consumer)
}

type metaWithPath struct {
	taskPath string
	metadata any
}

type metaWithTask struct {
	task     *protos.ProtoTask
	metadata any
}

// Enqueue adds a task to the queue.
// The task is enqueued only if it doesn't already exist in the queue.
func (q *Queue) Enqueue(task *protos.ProtoTask, metadata any, successFn func()) error {
	tenant := task.Tenant

	q.activeUsers.UpdateUserTimestamp(tenant, time.Now())

	var taskPath string
	var enqueuedValue any = metaWithTask{task: task, metadata: metadata}
	if q.cfg.StoreTasksOnDisk {
		taskPath = getTaskPath(task)
		if err := q.writeTask(task, taskPath); err != nil {
			return err
		}

		// If we're storing tasks on disk, we don't want to store the task in the queue but just the path (and the metadata)
		enqueuedValue = metaWithPath{taskPath: taskPath, metadata: metadata}
	}

	return q.queue.Enqueue(tenant, nil, enqueuedValue, func() {
		// Note: If we are storing tasks in-mem, taskPath is empty
		q.pendingTasks.Store(task.Id, taskPath)
		if successFn != nil {
			successFn()
		}
	})
}

// Dequeue takes a task from the queue. The task is not removed from the filesystem until Release is called.
func (q *Queue) Dequeue(ctx context.Context, last Index, consumerID string) (*protos.ProtoTask, any, Index, error) {
	item, idx, err := q.queue.Dequeue(ctx, last, consumerID)
	if err != nil {
		return nil, nil, idx, err
	}

	if !q.cfg.StoreTasksOnDisk {
		val := item.(metaWithTask)
		return val.task, val.metadata, idx, nil
	}

	// Read task from disk
	meta := item.(metaWithPath)
	task, err := q.readTask(meta.taskPath)
	if err != nil {
		return nil, nil, idx, err
	}

	return task, meta.metadata, idx, nil
}

// Release removes a task from the filesystem.
// Dequeue should be called before Remove.
func (q *Queue) Release(task *protos.ProtoTask) {
	val, existed := q.pendingTasks.LoadAndDelete(task.Id)
	if !existed {
		// Task doesn't exist, so it's not in the FS
		return
	}

	if !q.cfg.StoreTasksOnDisk {
		return
	}

	// Remove task from FS
	taskPath := val.(string)
	if err := q.deleteTask(taskPath); err != nil {
		level.Error(q.logger).Log("msg", "failed to remove task from disk", "task", task.Id, "err", err)
	}
}

func (q *Queue) writeTask(task *protos.ProtoTask, taskPath string) error {
	// Do not write the task if it's already pending. I.e. was already enqueued thus written to disk.
	if _, exists := q.pendingTasks.Load(task.Id); exists {
		return nil
	}

	taskSer, err := proto.Marshal(task)
	if err != nil {
		q.logger.Log("msg", "failed to serialize task", "task", task.Id, "err", err)
		return fmt.Errorf("failed to serialize task: %w", err)
	}

	reader := bytes.NewReader(taskSer)
	if err = q.disk.PutObject(context.Background(), taskPath, reader); err != nil {
		q.logger.Log("msg", "failed to write task to disk", "task", task.Id, "err", err)
		return fmt.Errorf("failed to write task to disk: %w", err)
	}

	return nil
}

func (q *Queue) readTask(taskPath string) (*protos.ProtoTask, error) {
	reader, _, err := q.disk.GetObject(context.Background(), taskPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read task from disk: %w", err)
	}

	taskSer, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read task from disk: %w", err)
	}

	task := &protos.ProtoTask{}
	if err = proto.Unmarshal(taskSer, task); err != nil {
		return nil, fmt.Errorf("failed to deserialize task: %w", err)
	}

	return task, nil
}

func (q *Queue) deleteTask(taskPath string) error {
	if err := q.disk.DeleteObject(context.Background(), taskPath); err != nil {
		return fmt.Errorf("failed to delete task from disk: %w", err)
	}
	return nil
}

func (q *Queue) TotalPending() (total int) {
	q.pendingTasks.Range(func(_, _ interface{}) bool {
		total++
		return true
	})
	return total
}

func getTaskPath(task *protos.ProtoTask) string {
	table := protos.FromProtoDayTableToDayTable(task.Table)
	taskFile := task.Id + ".protobuf"
	return filepath.Join("tasks", task.Tenant, table.String(), taskFile)
}

// The following are aliases for the queue package types.

type Metrics = queue.Metrics

func NewMetrics(registerer prometheus.Registerer, metricsNamespace string, subsystem string) *Metrics {
	return queue.NewMetrics(registerer, metricsNamespace, subsystem)
}

type Index = queue.QueueIndex

var StartIndex = queue.StartIndex

var ErrStopped = queue.ErrStopped
