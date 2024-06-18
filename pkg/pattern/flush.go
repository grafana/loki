package pattern

import (
	"fmt"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/util"
)

const retainSampleFor = 3 * time.Hour

func (i *Ingester) initFlushQueues() {
	// i.flushQueuesDone.Add(i.cfg.ConcurrentFlushes)
	for j := 0; j < i.cfg.ConcurrentFlushes; j++ {
		i.flushQueues[j] = util.NewPriorityQueue(i.metrics.flushQueueLength)
		// for now we don't flush only prune old samples.
		// go i.flushLoop(j)
	}
}

func (i *Ingester) Flush() {
	i.flush(true)
}

func (i *Ingester) flush(mayRemoveStreams bool) {
	i.sweepUsers(true, mayRemoveStreams)

	// Close the flush queues, to unblock waiting workers.
	for _, flushQueue := range i.flushQueues {
		flushQueue.Close()
	}

	i.flushQueuesDone.Wait()
	level.Debug(i.logger).Log("msg", "flush queues have drained")
}

type flushOp struct {
	from      model.Time
	userID    string
	fp        model.Fingerprint
	immediate bool
}

func (o *flushOp) Key() string {
	return fmt.Sprintf("%s-%s-%v", o.userID, o.fp, o.immediate)
}

func (o *flushOp) Priority() int64 {
	return -int64(o.from)
}

// sweepUsers periodically schedules series for flushing and garbage collects users with no series
func (i *Ingester) sweepUsers(immediate, mayRemoveStreams bool) {
	instances := i.getInstances()

	for _, instance := range instances {
		i.sweepInstance(instance, immediate, mayRemoveStreams)
	}
}

func (i *Ingester) sweepInstance(instance *instance, _, mayRemoveStreams bool) {
	_ = instance.streams.ForEach(func(s *stream) (bool, error) {
		if mayRemoveStreams {
			instance.streams.WithLock(func() {
				if s.prune(retainSampleFor) {
					instance.removeStream(s)
				}
			})
		}
		return true, nil
	})
}
