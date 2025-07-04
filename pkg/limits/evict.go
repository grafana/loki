package limits

import (
	"context"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

type evictable interface {
	evict(context.Context) error
}

// evictor runs scheduled evictions.
type evictor struct {
	ctx       context.Context
	interval  time.Duration
	evictable evictable
	logger    log.Logger

	// Used for tests.
	clock quartz.Clock
}

// newEvictor returns a new evictor over the interval.
func newEvictor(ctx context.Context, interval time.Duration, evictable evictable, logger log.Logger) (*evictor, error) {
	return &evictor{
		ctx:       ctx,
		interval:  interval,
		evictable: evictable,
		logger:    logger,
		clock:     quartz.NewReal(),
	}, nil
}

// Runs the scheduler loop until the context is canceled.
func (e *evictor) Run() error {
	t := e.clock.TickerFunc(e.ctx, e.interval, e.doTick)
	return t.Wait()
}

func (e *evictor) doTick() error {
	ctx, cancel := context.WithTimeout(e.ctx, e.interval)
	defer cancel()
	if err := e.evictable.evict(ctx); err != nil {
		level.Warn(e.logger).Log("failed to run eviction", "err", err.Error())
	}
	return nil
}
