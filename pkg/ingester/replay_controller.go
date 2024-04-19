package ingester

import (
	"sync"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log/level"
	"go.uber.org/atomic"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type replayFlusher struct {
	i *Ingester
}

func (f *replayFlusher) Flush() {
	f.i.InitFlushQueues()
	f.i.flush(false) // flush data but don't remove streams from the ingesters

	// Similar to sweepUsers with the exception that it will not remove streams
	// afterwards to prevent unlinking a stream which may receive later writes from the WAL.
	// We have to do this here after the flushQueues have been drained.
	instances := f.i.getInstances()

	for _, instance := range instances {

		_ = instance.streams.ForEach(func(s *stream) (bool, error) {
			f.i.removeFlushedChunks(instance, s, false)
			return true, nil
		})

	}

}

type Flusher interface {
	Flush()
}

// replayController handles coordinating backpressure between WAL replays and chunk flushing.
type replayController struct {
	// Note, this has to be defined first to make sure it is aligned properly for 32bit ARM OS
	// From https://golang.org/pkg/sync/atomic/#pkg-note-BUG:
	// > On ARM, 386, and 32-bit MIPS, it is the caller's responsibility to arrange for
	// > 64-bit alignment of 64-bit words accessed atomically. The first word in a
	// > variable or in an allocated struct, array, or slice can be relied upon to
	// > be 64-bit aligned.
	currentBytes atomic.Int64
	cfg          WALConfig
	metrics      *ingesterMetrics
	cond         *sync.Cond
	isFlushing   atomic.Bool
	flusher      Flusher
}

// flusher is expected to reduce pressure via calling Sub
func newReplayController(metrics *ingesterMetrics, cfg WALConfig, flusher Flusher) *replayController {
	return &replayController{
		cfg:     cfg,
		metrics: metrics,
		cond:    sync.NewCond(&sync.Mutex{}),
		flusher: flusher,
	}
}

func (c *replayController) Add(x int64) {
	c.metrics.recoveredBytesTotal.Add(float64(x))
	c.metrics.setRecoveryBytesInUse(c.currentBytes.Add(x))
}

func (c *replayController) Sub(x int64) {
	c.metrics.setRecoveryBytesInUse(c.currentBytes.Sub(x))

}

func (c *replayController) Cur() int {
	return int(c.currentBytes.Load())
}

func (c *replayController) Flush() {
	if c.isFlushing.CompareAndSwap(false, true) {
		c.metrics.recoveryIsFlushing.Set(1)
		prior := c.currentBytes.Load()
		level.Debug(util_log.Logger).Log(
			"msg", "replay flusher pre-flush",
			"bytes", humanize.Bytes(uint64(prior)),
		)

		c.flusher.Flush()

		after := c.currentBytes.Load()
		level.Debug(util_log.Logger).Log(
			"msg", "replay flusher post-flush",
			"bytes", humanize.Bytes(uint64(after)),
		)

		c.isFlushing.Store(false)
		c.metrics.recoveryIsFlushing.Set(0)

		// Broadcast after lock is acquired to prevent race conditions with cpu scheduling
		// where the flush code could finish before the goroutine which initiated it gets to call
		// c.cond.Wait()
		c.cond.L.Lock()
		c.cond.Broadcast()
		c.cond.L.Unlock()
	}
}

// WithBackPressure is expected to call replayController.Add in the passed function to increase the managed byte count.
// It will call the function as long as there is expected room before the memory cap and will then flush data intermittently
// when needed.
func (c *replayController) WithBackPressure(fn func() error) error {
	// Account for backpressure and wait until there's enough memory to continue replaying the WAL
	c.cond.L.Lock()

	// use 90% as a threshold since we'll be adding to it.
	for c.Cur() > int(c.cfg.ReplayMemoryCeiling)*9/10 {
		// too much backpressure, flush
		go c.Flush()
		c.cond.Wait()
	}

	// Don't hold the lock while executing the provided function.
	// This ensures we can run functions concurrently.
	c.cond.L.Unlock()

	return fn()
}
