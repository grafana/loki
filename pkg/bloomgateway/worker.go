package bloomgateway

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/queue"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
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

func (w *worker) running(ctx context.Context) error {
	idx := queue.StartIndexWithLocalQueue

	p := newProcessor(w.id, w.cfg.queryConcurrency, w.store, w.logger, w.metrics)

	for st := w.State(); st == services.Running || st == services.Stopping; {
		start := time.Now()
		item, newIdx, err := w.queue.Dequeue(ctx, idx, w.id)
		w.metrics.dequeueDuration.WithLabelValues(w.id).Observe(time.Since(start).Seconds())
		if err != nil {
			// We only return an error if the queue is stopped and dequeuing did not yield any items
			if err == queue.ErrStopped {
				return err
			}
			w.metrics.tasksDequeued.WithLabelValues(w.id, labelFailure).Inc()
			level.Error(w.logger).Log("msg", "failed to dequeue tasks", "err", err)
		}
		idx = newIdx

		w.metrics.tasksDequeued.WithLabelValues(w.id, labelSuccess).Inc()

		task, ok := item.(Task)
		if !ok {
			// This really should never happen, because only the bloom gateway itself can enqueue tasks.
			return errors.Errorf("failed to cast dequeued item to Task: %v", item)
		}
		level.Debug(w.logger).Log("msg", "dequeued task", "task", task.ID)
		_ = w.pending.Dec()
		w.metrics.queueDuration.WithLabelValues(w.id).Observe(time.Since(task.enqueueTime).Seconds())
		FromContext(task.ctx).AddQueueTime(time.Since(task.enqueueTime))
		first, last := getFirstLast(task.series)

		start = time.Now()
		err = p.runWithBounds(
			task.ctx,
			[]Task{task},
			[]v1.FingerprintBounds{v1.NewBounds(model.Fingerprint(first.Fingerprint), model.Fingerprint(last.Fingerprint))},
		)

		if err != nil {
			w.metrics.processDuration.WithLabelValues(w.id, labelFailure).Observe(time.Since(start).Seconds())
			w.metrics.tasksProcessed.WithLabelValues(w.id, labelFailure).Inc()
			level.Error(w.logger).Log("msg", "failed to process tasks", "err", err)
		} else {
			w.metrics.processDuration.WithLabelValues(w.id, labelSuccess).Observe(time.Since(start).Seconds())
			w.metrics.tasksProcessed.WithLabelValues(w.id, labelSuccess).Inc()
		}
	}

	return nil
}

func (w *worker) stopping(err error) error {
	level.Debug(w.logger).Log("msg", "stopping worker", "err", err)
	w.queue.UnregisterConsumerConnection(w.id)
	return nil
}
