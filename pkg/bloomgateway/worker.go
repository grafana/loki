package bloomgateway

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/queue"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

const (
	labelSuccess = "success"
	labelFailure = "failure"
)

type workerConfig struct {
	maxItems         int
	queryConcurrency int
}

// worker is a datastructure that consumes tasks from the request queue,
// processes them and returns the result/error back to the response channels of
// the tasks.
// It is responsible for multiplexing tasks so they can be processes in a more
// efficient way.
type worker struct {
	services.Service

	id      string
	cfg     workerConfig
	queue   *queue.RequestQueue
	store   bloomshipper.Store
	pending *atomic.Int64
	logger  log.Logger
	metrics *workerMetrics
}

func newWorker(id string, cfg workerConfig, queue *queue.RequestQueue, store bloomshipper.Store, pending *atomic.Int64, logger log.Logger, metrics *workerMetrics) *worker {
	w := &worker{
		id:      id,
		cfg:     cfg,
		queue:   queue,
		store:   store,
		pending: pending,
		logger:  log.With(logger, "worker", id),
		metrics: metrics,
	}
	w.Service = services.NewBasicService(w.starting, w.running, w.stopping).WithName(id)
	return w
}

func (w *worker) starting(_ context.Context) error {
	level.Debug(w.logger).Log("msg", "starting worker")
	w.queue.RegisterConsumerConnection(w.id)
	return nil
}

func (w *worker) running(_ context.Context) error {
	idx := queue.StartIndexWithLocalQueue

	p := newProcessor(w.id, w.cfg.queryConcurrency, w.store, w.logger, w.metrics)

	for st := w.State(); st == services.Running || st == services.Stopping; {
		taskCtx := context.Background()
		start := time.Now()
		items, newIdx, err := w.queue.DequeueMany(taskCtx, idx, w.id, w.cfg.maxItems)
		w.metrics.dequeueDuration.WithLabelValues(w.id).Observe(time.Since(start).Seconds())
		if err != nil {
			// We only return an error if the queue is stopped and dequeuing did not yield any items
			if err == queue.ErrStopped && len(items) == 0 {
				return err
			}
			w.metrics.tasksDequeued.WithLabelValues(w.id, labelFailure).Inc()
			level.Error(w.logger).Log("msg", "failed to dequeue tasks", "err", err, "items", len(items))
		}
		idx = newIdx

		if len(items) == 0 {
			w.queue.ReleaseRequests(items)
			continue
		}

		w.metrics.tasksDequeued.WithLabelValues(w.id, labelSuccess).Add(float64(len(items)))

		tasks := make([]Task, 0, len(items))
		for _, item := range items {
			task, ok := item.(Task)
			if !ok {
				// This really should never happen, because only the bloom gateway itself can enqueue tasks.
				w.queue.ReleaseRequests(items)
				return errors.Errorf("failed to cast dequeued item to Task: %v", item)
			}
			_ = w.pending.Dec()
			w.metrics.queueDuration.WithLabelValues(w.id).Observe(time.Since(task.enqueueTime).Seconds())
			FromContext(task.ctx).AddQueueTime(time.Since(task.enqueueTime))
			tasks = append(tasks, task)
		}

		start = time.Now()
		err = p.processTasks(taskCtx, tasks)

		if err != nil {
			w.metrics.processDuration.WithLabelValues(w.id, labelFailure).Observe(time.Since(start).Seconds())
			w.metrics.tasksProcessed.WithLabelValues(w.id, labelFailure).Add(float64(len(tasks)))
			level.Error(w.logger).Log("msg", "failed to process tasks", "err", err)
		} else {
			w.metrics.processDuration.WithLabelValues(w.id, labelSuccess).Observe(time.Since(start).Seconds())
			w.metrics.tasksProcessed.WithLabelValues(w.id, labelSuccess).Add(float64(len(tasks)))
		}

		// return dequeued items back to the pool
		w.queue.ReleaseRequests(items)
	}

	return nil
}

func (w *worker) stopping(err error) error {
	level.Debug(w.logger).Log("msg", "stopping worker", "err", err)
	w.queue.UnregisterConsumerConnection(w.id)
	return nil
}
