package ingester

import (
	"sync"

	"go.uber.org/atomic"
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
		instance.streamsMtx.Lock()

		for _, stream := range instance.streams {
			f.i.removeFlushedChunks(instance, stream, false)
		}

		instance.streamsMtx.Unlock()
	}
}

type Flusher interface {
	Flush()
}

// replayController handles coordinating backpressure between WAL replays and chunk flushing.
type replayController struct {
	cfg          WALConfig
	metrics      *ingesterMetrics
	currentBytes atomic.Int64
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
	if c.isFlushing.CAS(false, true) {
		c.flusher.Flush()
		c.isFlushing.Store(false)

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
