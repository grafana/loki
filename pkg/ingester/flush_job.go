package ingester

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/util"
)

// errFlushNotRunning is returned when a flush is requested while the ingester is
// not running: its flush queues don't exist before startup completes and are
// closed during shutdown, where the shutdown flush owns flushing.
var errFlushNotRunning = errors.New("ingester is not running")

// flushQueueIndex returns the index of the flush queue that owns a stream. A
// stream is always flushed by the same queue, and each queue has a single
// worker, so only one flush op operates on a stream at a time.
func (i *Ingester) flushQueueIndex(fp model.Fingerprint) int {
	return int(uint64(fp) % uint64(i.cfg.ConcurrentFlushes))
}

// flushStreams force flushes the in-memory chunks of the streams matching
// matchers. An empty tenantID flushes all tenants. An empty matcher set matches
// all of a tenant's streams.
//
// In asynchronous mode it enqueues an immediate flushOp per stream and returns.
// In synchronous mode it does the same, but attaches a flushJob to each op and
// waits for the job to complete. If an op for a stream is already queued the job
// is attached to it instead of enqueueing a duplicate. Either way the work runs
// on the flush workers, which keeps the guarantee that a stream is only flushed
// by one op at a time.
//
// It returns errFlushNotRunning or ErrReadOnly if the ingester is not running or
// is shutting down. Otherwise it returns the first flush error, or the context's
// error if ctx ends first, in which case the enqueued ops still run.
func (i *Ingester) flushStreams(ctx context.Context, tenantID string, matchers []*labels.Matcher, synchronous bool) error {
	if i.State() != services.Running {
		return errFlushNotRunning
	}

	// Shutdown marks the ingester read-only under shutdownMtx before it closes the
	// flush queues, so holding it while enqueueing guarantees the queues stay open.
	// It is released before waiting so a slow flush can't block shutdown.
	i.shutdownMtx.Lock()
	unlock := sync.OnceFunc(i.shutdownMtx.Unlock)
	defer unlock()
	if i.readonly {
		return ErrReadOnly
	}

	var instances []*instance
	if tenantID == "" {
		instances = i.getInstances()
	} else if inst, ok := i.getInstanceByID(tenantID); ok {
		instances = []*instance{inst}
	}

	var job *flushJob
	if synchronous {
		job = newFlushJob()
		job.add() // hold one reference as we start enqueueing to prevent an early completion
	}
	for _, inst := range instances {
		var enqueued int
		if err := inst.forMatchingStreams(ctx, time.Now(), matchers, nil, func(s *stream) error {
			op := newForcedFlushOp(inst, s)
			if op == nil {
				return nil
			}
			enqueued++

			queue := i.flushQueues[i.flushQueueIndex(s.fp)]
			if job == nil {
				queue.Enqueue(op)
				return nil
			}

			// The job counts the op even when it merges into a queued one, which
			// completes the job once it has run.
			job.add()
			op.jobs = []*flushJob{job}
			queue.EnqueueOrMerge(op, func(existing util.Op) {
				queued := existing.(*flushOp)
				queued.jobs = append(queued.jobs, job)
			})
			return nil
		}); err != nil {
			return err
		}

		level.Info(i.logger).Log("msg", "enqueued tenant streams for flush", "tenant", inst.instanceID, "streams", enqueued, "synchronous", synchronous)
	}

	if job == nil {
		return nil
	}

	job.done(nil) // release the one reference added above now that we have enqueued all ops
	unlock()
	return job.wait(ctx)
}

// newForcedFlushOp returns an immediate flushOp for a stream, or nil if the
// stream has no chunks to flush.
func newForcedFlushOp(inst *instance, s *stream) *flushOp {
	s.chunkMtx.RLock()
	defer s.chunkMtx.RUnlock()
	if len(s.chunks) == 0 {
		return nil
	}
	firstTime, _ := s.chunks[0].chunk.Bounds()
	return &flushOp{
		from:      model.TimeFromUnixNano(firstTime.UnixNano()),
		userID:    inst.instanceID,
		fp:        s.fp,
		immediate: true,
	}
}

// flushJob tracks the ops of one synchronous flush request. A single struct
// covers all of them, so completion is a counter rather than a handle per op.
type flushJob struct {
	pending atomic.Int64
	doneCh  chan struct{}

	mtx sync.Mutex
	err error
}

func newFlushJob() *flushJob {
	return &flushJob{doneCh: make(chan struct{})}
}

// add registers an op that will call done.
func (j *flushJob) add() { j.pending.Add(1) }

// done releases one reference, completing the job when none are left. A non-nil
// err is recorded if it is the first error of the job.
func (j *flushJob) done(err error) {
	if err != nil {
		j.mtx.Lock()
		if j.err == nil {
			j.err = err
		}
		j.mtx.Unlock()
	}
	if j.pending.Add(-1) == 0 {
		close(j.doneCh)
	}
}

// wait blocks until the job completes or ctx ends, and returns the first
// recorded error.
func (j *flushJob) wait(ctx context.Context) error {
	select {
	case <-j.doneCh:
	case <-ctx.Done():
		return ctx.Err()
	}
	j.mtx.Lock()
	defer j.mtx.Unlock()
	return j.err
}
