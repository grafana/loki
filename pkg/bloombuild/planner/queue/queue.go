package queue

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/queue"
	"github.com/grafana/loki/v3/pkg/util"
)

type Task interface {
	Tenant() string
	Table() string
	ID() string
}

// Queue is a wrapper of queue.RequestQueue that uses the file system to store the pending tasks.
// When a task is enqueued, it's stored in the file system and recorded ad pending.
// When it's dequeued, it's removed from the queue but kept in FS until removed.
type Queue struct {
	services.Service

	queue *queue.RequestQueue
	// pendingTasks is a map of task ID to the file where the task is stored.
	pendingTasks sync.Map
	activeUsers  *util.ActiveUsersCleanupService

	cfg    Config
	logger log.Logger

	// Subservices manager.
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher
}

func NewQueue(logger log.Logger, cfg Config, limits Limits, metrics *Metrics) (*Queue, error) {
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
		subservices:        subservices,
		subservicesWatcher: subservicesWatcher,
	}

	q.Service = services.NewIdleService(q.starting, q.stopping)

	return q, nil
}

func (q *Queue) starting(ctx context.Context) error {
	if err := services.StartManagerAndAwaitHealthy(ctx, q.subservices); err != nil {
		return fmt.Errorf("failed to start task queue subservices: %w", err)
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

// Enqueue adds a task to the queue.
func (q *Queue) Enqueue(tenant string, task Task, successFn func()) error {
	q.activeUsers.UpdateUserTimestamp(tenant, time.Now())
	return q.queue.Enqueue(tenant, nil, task, func() {
		taskPath := getTaskPath(task)
		_, existed := q.pendingTasks.LoadOrStore(task.ID(), taskPath)
		if existed {
			// Task already exists, so it's already in the FS
			return
		}

		// TODO: Write to FS
		_ = taskPath

		if successFn != nil {
			successFn()
		}
	})
}

// Dequeue takes a task from the queue. The task is not removed from the filesystem until Release is called.
func (q *Queue) Dequeue(ctx context.Context, last Index, consumerID string) (Task, Index, error) {
	item, idx, err := q.queue.Dequeue(ctx, last, consumerID)
	if err != nil {
		return nil, idx, err
	}

	return item.(Task), idx, nil
}

// Release removes a task from the filesystem.
// Dequeue should be called before Remove.
func (q *Queue) Release(task Task) {
	taskPath, existed := q.pendingTasks.LoadAndDelete(task.ID())
	if !existed {
		// Task doesn't exist, so it's not in the FS
		return
	}

	// TODO: Remove from FS
	_ = taskPath
}

func (q *Queue) TotalPending() (total int) {
	q.pendingTasks.Range(func(_, _ interface{}) bool {
		total++
		return true
	})
	return total
}

func getTaskPath(task Task) string {
	return filepath.Join("tasks", task.Tenant(), task.Table(), task.ID())
}

// The following are aliases for the queue package types.

type Metrics = queue.Metrics

func NewMetrics(registerer prometheus.Registerer, metricsNamespace string, subsystem string) *Metrics {
	return queue.NewMetrics(registerer, metricsNamespace, subsystem)
}

type Index = queue.QueueIndex

var StartIndex = queue.StartIndex

var ErrStopped = queue.ErrStopped
