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
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/queue"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

type workerConfig struct {
	maxWaitTime time.Duration
	maxItems    int
}

type workerMetrics struct {
	dequeuedTasks      *prometheus.CounterVec
	dequeueErrors      *prometheus.CounterVec
	dequeueWaitTime    *prometheus.SummaryVec
	storeAccessLatency *prometheus.HistogramVec
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
	tasks   *pendingTasks
	logger  log.Logger
	metrics *workerMetrics
}

func newWorker(id string, cfg workerConfig, queue *queue.RequestQueue, store bloomshipper.Store, tasks *pendingTasks, logger log.Logger, metrics *workerMetrics) *worker {
	w := &worker{
		id:      id,
		cfg:     cfg,
		queue:   queue,
		store:   store,
		tasks:   tasks,
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

func (w *worker) running(ctx context.Context) error {
	idx := queue.StartIndexWithLocalQueue

	for {
		select {

		case <-ctx.Done():
			return ctx.Err()

		default:
			taskCtx := context.Background()
			dequeueStart := time.Now()
			items, newIdx, err := w.queue.DequeueMany(taskCtx, idx, w.id, w.cfg.maxItems, w.cfg.maxWaitTime)
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

			tasksPerDay := make(map[time.Time][]Task)

			for _, item := range items {
				task, ok := item.(Task)
				if !ok {
					// This really should never happen, because only the bloom gateway itself can enqueue tasks.
					w.queue.ReleaseRequests(items)
					return errors.Errorf("failed to cast dequeued item to Task: %v", item)
				}
				level.Debug(w.logger).Log("msg", "dequeued task", "task", task.ID)
				w.tasks.Delete(task.ID)

				fromDay, throughDay := task.Bounds()

				if fromDay.Equal(throughDay) {
					tasksPerDay[fromDay] = append(tasksPerDay[fromDay], task)
				} else {
					for i := fromDay; i.Before(throughDay); i = i.Add(24 * time.Hour) {
						tasksPerDay[i] = append(tasksPerDay[i], task)
					}
				}
			}

			for day, tasks := range tasksPerDay {
				logger := log.With(w.logger, "day", day)
				level.Debug(logger).Log("msg", "process tasks", "tasks", len(tasks))

				storeFetchStart := time.Now()
				blockRefs, err := w.store.GetBlockRefs(taskCtx, tasks[0].Tenant, day, day.Add(Day).Add(-1*time.Nanosecond))
				w.metrics.storeAccessLatency.WithLabelValues(w.id, "GetBlockRefs").Observe(time.Since(storeFetchStart).Seconds())
				if err != nil {
					for _, t := range tasks {
						t.ErrCh <- err
					}
					// continue with tasks of next day
					continue
				}
				// No blocks found.
				// Since there are no blocks for the given tasks, we need to return the
				// unfiltered list of chunk refs.
				if len(blockRefs) == 0 {
					level.Warn(logger).Log("msg", "no blocks found")
					for _, t := range tasks {
						for _, ref := range t.Request.Refs {
							t.ResCh <- v1.Output{
								Fp:       model.Fingerprint(ref.Fingerprint),
								Removals: nil,
							}
						}
					}
					// continue with tasks of next day
					continue
				}

				boundedRefs := partitionFingerprintRange(tasks, blockRefs)
				blockRefs = blockRefs[:0]
				for _, b := range boundedRefs {
					blockRefs = append(blockRefs, b.blockRef)
				}

				// GetBlockQueriersForBlockRefs() waits until all blocks are downloaded and available for querying.
				// TODO(chaudum): Add API that allows to process blocks as soon as they become available.
				// This will require to change the taskMergeIterator to a slice of requests so we can seek
				// to the appropriate fingerprint range within the slice that matches the block's fingerprint range.
				storeFetchStart = time.Now()
				blockQueriers, err := w.store.GetBlockQueriersForBlockRefs(taskCtx, tasks[0].Tenant, blockRefs)
				w.metrics.storeAccessLatency.WithLabelValues(w.id, "GetBlockQueriersForBlockRefs").Observe(time.Since(storeFetchStart).Seconds())
				if err != nil {
					for _, t := range tasks {
						t.ErrCh <- err
					}
					// continue with tasks of next day
					continue
				}

				for i, blockQuerier := range blockQueriers {
					it := newTaskMergeIterator(day, boundedRefs[i].tasks...)
					fq := blockQuerier.Fuse([]v1.PeekingIterator[v1.Request]{it})
					err := fq.Run()
					if err != nil {
						for _, t := range boundedRefs[i].tasks {
							t.ErrCh <- errors.Wrap(err, "failed to run chunk check")
						}
					}
				}
			}

			// return dequeued items back to the pool
			w.queue.ReleaseRequests(items)

		}
	}
}

func (w *worker) stopping(err error) error {
	level.Debug(w.logger).Log("msg", "stopping worker", "err", err)
	w.queue.UnregisterConsumerConnection(w.id)
	return nil
}
