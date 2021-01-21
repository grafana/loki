package ingester

import (
	"sync"

	"go.uber.org/atomic"
)

type Flusher interface {
	InitFlushQueues()
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
	c.metrics.setRecoveryBytesInUse(c.currentBytes.Add(int64(x)))
}

func (c *replayController) Sub(x int64) {
	c.metrics.setRecoveryBytesInUse(c.currentBytes.Sub(int64(x)))
}

func (c *replayController) Cur() int {
	return int(c.currentBytes.Load())
}

func (c *replayController) Flush() {
	if c.isFlushing.CAS(false, true) {
		c.flusher.InitFlushQueues()
		c.flusher.Flush()
		c.isFlushing.Store(false)
		c.cond.Broadcast()
	}
}

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
