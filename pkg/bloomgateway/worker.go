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
	"golang.org/x/exp/slices"

	"github.com/grafana/loki/pkg/queue"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
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
	shipper bloomshipper.Interface
	pending *pendingTasks
	logger  log.Logger
	metrics *workerMetrics
}

func newWorker(id string, cfg workerConfig, queue *queue.RequestQueue, shipper bloomshipper.Interface, pending *pendingTasks, logger log.Logger, metrics *workerMetrics) *worker {
	w := &worker{
		id:      id,
		cfg:     cfg,
		queue:   queue,
		shipper: shipper,
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

		tasksPerDay := make(map[model.Time][]Task)

		for _, item := range items {
			task, ok := item.(Task)
			if !ok {
				// This really should never happen, because only the bloom gateway itself can enqueue tasks.
				w.queue.ReleaseRequests(items)
				return errors.Errorf("failed to cast dequeued item to Task: %v", item)
			}
			level.Debug(w.logger).Log("msg", "dequeued task", "task", task.ID)
			w.pending.Delete(task.ID)

			tasksPerDay[task.day] = append(tasksPerDay[task.day], task)
		}

		for day, tasks := range tasksPerDay {

			// Remove tasks that are already cancelled
			tasks = slices.DeleteFunc(tasks, func(t Task) bool {
				if res := t.ctx.Err(); res != nil {
					t.CloseWithError(res)
					return true
				}
				return false
			})
			// no tasks to process
			// continue with tasks of next day
			if len(tasks) == 0 {
				continue
			}

			// interval is [Start, End)
			interval := bloomshipper.NewInterval(day, day.Add(Day))
			logger := log.With(w.logger, "day", day.Time(), "tenant", tasks[0].Tenant)
			level.Debug(logger).Log("msg", "process tasks", "tasks", len(tasks))

			storeFetchStart := time.Now()
			blockRefs, err := w.shipper.GetBlockRefs(taskCtx, tasks[0].Tenant, interval)
			w.metrics.storeAccessLatency.WithLabelValues(w.id, "GetBlockRefs").Observe(time.Since(storeFetchStart).Seconds())
			if err != nil {
				for _, t := range tasks {
					t.CloseWithError(err)
				}
				// continue with tasks of next day
				continue
			}
			if len(tasks) == 0 {
				continue
			}

			// No blocks found.
			// Since there are no blocks for the given tasks, we need to return the
			// unfiltered list of chunk refs.
			if len(blockRefs) == 0 {
				level.Warn(logger).Log("msg", "no blocks found")
				for _, t := range tasks {
					t.Close()
				}
				// continue with tasks of next day
				continue
			}

			// Remove tasks that are already cancelled
			tasks = slices.DeleteFunc(tasks, func(t Task) bool {
				if res := t.ctx.Err(); res != nil {
					t.CloseWithError(res)
					return true
				}
				return false
			})
			// no tasks to process
			// continue with tasks of next day
			if len(tasks) == 0 {
				continue
			}

			tasksForBlocks := partitionFingerprintRange(tasks, blockRefs)
			blockRefs = blockRefs[:0]
			for _, b := range tasksForBlocks {
				blockRefs = append(blockRefs, b.blockRef)
			}

			err = w.processBlocksWithCallback(taskCtx, tasks[0].Tenant, blockRefs, tasksForBlocks)
			if err != nil {
				for _, t := range tasks {
					t.CloseWithError(err)
				}
				// continue with tasks of next day
				continue
			}

			// all tasks for this day are done.
			// close them to notify the request handler
			for _, task := range tasks {
				task.Close()
			}
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

func (w *worker) processBlocksWithCallback(taskCtx context.Context, tenant string, blockRefs []bloomshipper.BlockRef, boundedRefs []boundedTasks) error {
	return w.shipper.Fetch(taskCtx, tenant, blockRefs, func(bq *v1.BlockQuerier, bounds v1.FingerprintBounds) error {
		for _, b := range boundedRefs {
			if b.blockRef.Bounds().Equal(bounds) {
				return w.processBlock(bq, b.tasks)
			}
		}
		return nil
	})
}

func (w *worker) processBlock(blockQuerier *v1.BlockQuerier, tasks []Task) error {
	schema, err := blockQuerier.Schema()
	if err != nil {
		return err
	}

	tokenizer := v1.NewNGramTokenizer(schema.NGramLen(), 0)
	iters := make([]v1.PeekingIterator[v1.Request], 0, len(tasks))
	for _, task := range tasks {
		it := v1.NewPeekingIter(task.RequestIter(tokenizer))
		iters = append(iters, it)
	}
	fq := blockQuerier.Fuse(iters)

	start := time.Now()
	err = fq.Run()
	duration := time.Since(start).Seconds()

	if err != nil {
		w.metrics.bloomQueryLatency.WithLabelValues(w.id, "failure").Observe(duration)
		return err
	}

	w.metrics.bloomQueryLatency.WithLabelValues(w.id, "success").Observe(duration)
	return nil
}
