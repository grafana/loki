package ingester

import (
	"fmt"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log/level"
	"go.uber.org/atomic"
	"golang.org/x/sync/singleflight"

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
	currentBytes    atomic.Int64
	totalSubtracted atomic.Int64 // monotonically increasing; used to detect flush no-progress without being affected by concurrent Add calls
	cfg             WALConfig
	metrics         *ingesterMetrics

	flusher Flusher
	flushSF singleflight.Group
}

// flusher is expected to reduce pressure via calling Sub
func newReplayController(metrics *ingesterMetrics, cfg WALConfig, flusher Flusher) *replayController {
	return &replayController{
		cfg:     cfg,
		metrics: metrics,
		flusher: flusher,
	}
}

func (c *replayController) Add(x int64) {
	c.metrics.recoveredBytesTotal.Add(float64(x))
	c.metrics.setRecoveryBytesInUse(c.currentBytes.Add(x))
}

func (c *replayController) Sub(x int64) {
	c.totalSubtracted.Add(x)
	c.metrics.setRecoveryBytesInUse(c.currentBytes.Sub(x))
}

func (c *replayController) Cur() int {
	return int(c.currentBytes.Load())
}

// Flush runs (or joins) a single in-flight flush and returns the number of
// bytes that flush subtracted. The returned value is shared with every caller
// coalesced into the same flush, so each caller observes the progress made by
// the flush it actually participated in rather than comparing against a
// snapshot taken outside the flush (which can miss progress made by an already
// in-flight flush before the snapshot was taken).
func (c *replayController) Flush() int64 {
	// Use singleflight to ensure only one flush happens at a time
	subtracted, _, _ := c.flushSF.Do("flush", func() (interface{}, error) {
		return c.flush(), nil
	})
	return subtracted.(int64)
}

// flush performs a single flush and returns how many bytes it subtracted.
// Because singleflight guarantees only one flush runs at a time and Sub is only
// called from within a flush, the delta of totalSubtracted across this call is
// exactly the progress attributable to this flush.
func (c *replayController) flush() int64 {
	c.metrics.recoveryIsFlushing.Set(1)
	subtractedBefore := c.totalSubtracted.Load()
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

	c.metrics.recoveryIsFlushing.Set(0)
	return c.totalSubtracted.Load() - subtractedBefore
}

// WithBackPressure is expected to call replayController.Add in the passed function to increase the managed byte count.
// It will call the function as long as there is expected room before the memory cap and will then flush data intermittently
// when needed.
func (c *replayController) WithBackPressure(fn func() error) error {
	ceiling := int(c.cfg.ReplayMemoryCeiling) * 9 / 10
	if ceiling <= 0 {
		return fn()
	}
	// use 90% as a threshold since we'll be adding to it.
	for c.Cur() > ceiling {
		// too much backpressure, flush. Flush reports how many bytes the flush
		// we participated in subtracted, so a caller coalesced into an already
		// in-flight flush still observes that flush's progress.
		//
		// Only treat a zero-progress flush as fatal if we are *still* over the
		// ceiling afterwards. A concurrent worker's flush may have drained memory
		// below the ceiling between our loop guard and this call, in which case
		// our own flush legitimately has nothing to do and we should simply exit
		// the loop rather than report a spurious no-progress error.
		if c.Flush() == 0 && c.Cur() > ceiling {
			return fmt.Errorf("WAL replay flush made no progress: %s in use, ceiling %s; cannot recover",
				humanize.Bytes(uint64(c.currentBytes.Load())),
				humanize.Bytes(uint64(ceiling)),
			)
		}
	}

	return fn()
}
