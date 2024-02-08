package bloomgateway

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/pkg/queue"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

type workerConfig struct {
	maxItems int
}

type workerMetrics struct {
	dequeuedTasks      *prometheus.CounterVec
	dequeueErrors      *prometheus.CounterVec
	dequeueWaitTime    *prometheus.SummaryVec
	storeAccessLatency *prometheus.HistogramVec
	bloomQueryLatency  *prometheus.HistogramVec
}

func newWorkerMetrics(registerer prometheus.Registerer, namespace, subsystem string) *workerMetrics {
	labels := []string{"worker"}
	return &workerMetrics{
		dequeuedTasks: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "dequeued_tasks_total",
			Help:      "Total amount of tasks that the worker dequeued from the bloom query queue",
		}, labels),
		dequeueErrors: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "dequeue_errors_total",
			Help:      "Total amount of failed dequeue operations",
		}, labels),
		dequeueWaitTime: promauto.With(registerer).NewSummaryVec(prometheus.SummaryOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "dequeue_wait_time",
			Help:      "Time spent waiting for dequeuing tasks from queue",
		}, labels),
		bloomQueryLatency: promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "bloom_query_latency",
			Help:      "Latency in seconds of processing bloom blocks",
		}, append(labels, "status")),
		// TODO(chaudum): Move this metric into the bloomshipper
		storeAccessLatency: promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "store_latency",
			Help:      "Latency in seconds of accessing the bloom store component",
		}, append(labels, "operation")),
	}
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
	pending *pendingTasks
	logger  log.Logger
	metrics *workerMetrics
}

func newWorker(id string, cfg workerConfig, queue *queue.RequestQueue, store bloomshipper.Store, pending *pendingTasks, logger log.Logger, metrics *workerMetrics) *worker {
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

	p := processor{store: w.store, logger: w.logger}

	for st := w.State(); st == services.Running || st == services.Stopping; {
		taskCtx := context.Background()
		dequeueStart := time.Now()
		items, newIdx, err := w.queue.DequeueMany(taskCtx, idx, w.id, w.cfg.maxItems)
		w.metrics.dequeueWaitTime.WithLabelValues(w.id).Observe(time.Since(dequeueStart).Seconds())
		if err != nil {
			// We only return an error if the queue is stopped and dequeuing did not yield any items
			if err == queue.ErrStopped && len(items) == 0 {
				return err
			}
			w.metrics.dequeueErrors.WithLabelValues(w.id).Inc()
			level.Error(w.logger).Log("msg", "failed to dequeue tasks", "err", err, "items", len(items))
		}
		idx = newIdx

		if len(items) == 0 {
			w.queue.ReleaseRequests(items)
			continue
		}
		w.metrics.dequeuedTasks.WithLabelValues(w.id).Add(float64(len(items)))

		tasks := make([]Task, 0, len(items))
		for _, item := range items {
			task, ok := item.(Task)
			if !ok {
				// This really should never happen, because only the bloom gateway itself can enqueue tasks.
				w.queue.ReleaseRequests(items)
				return errors.Errorf("failed to cast dequeued item to Task: %v", item)
			}
			level.Debug(w.logger).Log("msg", "dequeued task", "task", task.ID)
			w.pending.Delete(task.ID)
			tasks = append(tasks, task)
		}

		err = p.run(taskCtx, tasks)
		if err != nil {
			level.Error(w.logger).Log("msg", "failed to process tasks", "err", err)
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
