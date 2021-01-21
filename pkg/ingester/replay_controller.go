package ingester

import (
	fmt "fmt"
	"sync"

	"go.uber.org/atomic"
)

type Flusher interface {
	InitFlushQueues()
	Flush()
	RemoveFlushedChunks()
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
		fmt.Println("flushing, before ", c.Cur())
		c.flusher.InitFlushQueues()
		c.flusher.Flush()
		c.flusher.RemoveFlushedChunks()
		fmt.Println("flushing, after ", c.Cur())
		c.isFlushing.Store(false)
		c.cond.Broadcast()
	}
}

// WithBackPressure is expected to call replayController.Add in the passed function to increase the managed byte count.
// It will call the function as long as there is expected room before the memory cap and will then flush data intermittently
// when needed.
func (c *replayController) WithBackPressure(fn func() error) error {
	defer func() {
		fmt.Println("hit, cur", c.Cur())
	}()
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
