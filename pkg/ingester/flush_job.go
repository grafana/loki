package ingester

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/util"
)

// errFlushNotRunning is returned when a flush is requested while the ingester is not running.
var errFlushNotRunning = errors.New("ingester is not running")

// flushQueueIndex returns the index of the flush queue that owns a stream.
func (i *Ingester) flushQueueIndex(fp model.Fingerprint) int {
	return int(uint64(fp) % uint64(i.cfg.ConcurrentFlushes))
}

// flushStreams enqueues flushOps given the passed parameters. It can be
// scoped to a single tenant or a subset of streams via the matchers. If
// these parameters are empty it enqueues flushOps for all streams and all tenants.
// If synchronous it will block until all flushOps are processed otherwise
// it returns immediately.
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
	} else {
		level.Warn(i.logger).Log("msg", "flush requested for unknown tenant", "tenant", tenantID)
		return nil
	}

	var job *flushJob
	if synchronous {
		job = newFlushJob()
		job.add() // hold one reference as we start enqueueing to prevent an early completion
	}
	for _, inst := range instances {
		var enqueued int
		if err := inst.forMatchingStreams(ctx, time.Now(), matchers, nil, func(s *stream) error {
			op := newImmediateFlushOp(inst, s)
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

// newImmediateFlushOp returns an immediate flushOp for a stream, or nil if the
// stream has no chunks to flush.
func newImmediateFlushOp(inst *instance, s *stream) *flushOp {
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
