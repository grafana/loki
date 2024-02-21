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

const (
	labelSuccess = "success"
	labelFailure = "failure"
)

type workerConfig struct {
	maxItems int
}

type workerMetrics struct {
	dequeueDuration   *prometheus.HistogramVec
	processDuration   *prometheus.HistogramVec
	metasFetched      *prometheus.HistogramVec
	blocksFetched     *prometheus.HistogramVec
	tasksDequeued     *prometheus.CounterVec
	tasksProcessed    *prometheus.CounterVec
	blockQueryLatency *prometheus.HistogramVec
}

func newWorkerMetrics(registerer prometheus.Registerer, namespace, subsystem string) *workerMetrics {
	labels := []string{"worker"}
	r := promauto.With(registerer)
	return &workerMetrics{
		dequeueDuration: r.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "dequeue_duration_seconds",
			Help:      "Time spent dequeuing tasks from queue in seconds",
		}, labels),
		processDuration: r.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "process_duration_seconds",
			Help:      "Time spent processing tasks in seconds",
		}, append(labels, "status")),
		metasFetched: r.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "metas_fetched",
			Help:      "Amount of metas fetched",
		}, labels),
		blocksFetched: r.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "blocks_fetched",
			Help:      "Amount of blocks fetched",
		}, labels),
		tasksDequeued: r.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "tasks_dequeued_total",
			Help:      "Total amount of tasks that the worker dequeued from the queue",
		}, append(labels, "status")),
		tasksProcessed: r.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "tasks_processed_total",
			Help:      "Total amount of tasks that the worker processed",
		}, append(labels, "status")),
		blockQueryLatency: r.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "block_query_latency",
			Help:      "Time spent running searches against a bloom block",
		}, append(labels, "status")),
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

	p := newProcessor(w.id, w.store, w.logger, w.metrics)

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
			level.Debug(w.logger).Log("msg", "dequeued task", "task", task.ID)
			w.pending.Delete(task.ID)
			tasks = append(tasks, task)
		}

		start = time.Now()
		err = p.run(taskCtx, tasks)

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
