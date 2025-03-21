package limits

import (
	"context"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

type Evictable interface {
	Evict(context.Context) error
}

// Evictor runs scheduled evictions.
type Evictor struct {
	ctx      context.Context
	interval time.Duration
	target   Evictable
	logger   log.Logger

	// Used for tests.
	clock quartz.Clock
}

// NewEvictor returns a new evictor over the interval.
func NewEvictor(ctx context.Context, interval time.Duration, target Evictable, logger log.Logger) (*Evictor, error) {
	return &Evictor{
		ctx:      ctx,
		interval: interval,
		target:   target,
		logger:   logger,
		clock:    quartz.NewReal(),
	}, nil
}

// Runs the scheduler loop until the context is canceled.
func (e *Evictor) Run() error {
	t := e.clock.TickerFunc(e.ctx, e.interval, e.doTick)
	return t.Wait()
}

func (e *Evictor) doTick() error {
	ctx, cancel := context.WithTimeout(e.ctx, e.interval)
	defer cancel()
	if err := e.target.Evict(ctx); err != nil {
		level.Warn(e.logger).Log("failed to run eviction", "err", err.Error())
	}
	return nil
}
