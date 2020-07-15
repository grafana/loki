package util

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"time"
)

// BackoffConfig configures a Backoff
type BackoffConfig struct {
	MinBackoff time.Duration `yaml:"min_period"`  // start backoff at this level
	MaxBackoff time.Duration `yaml:"max_period"`  // increase exponentially to this level
	MaxRetries int           `yaml:"max_retries"` // give up after this many; zero means infinite retries
}

// RegisterFlags for BackoffConfig.
func (cfg *BackoffConfig) RegisterFlags(prefix string, f *flag.FlagSet) {
	f.DurationVar(&cfg.MinBackoff, prefix+".backoff-min-period", 100*time.Millisecond, "Minimum delay when backing off.")
	f.DurationVar(&cfg.MaxBackoff, prefix+".backoff-max-period", 10*time.Second, "Maximum delay when backing off.")
	f.IntVar(&cfg.MaxRetries, prefix+".backoff-retries", 10, "Number of times to backoff and retry before failing.")
}

// Backoff implements exponential backoff with randomized wait times
type Backoff struct {
	cfg          BackoffConfig
	ctx          context.Context
	numRetries   int
	nextDelayMin time.Duration
	nextDelayMax time.Duration
}

// NewBackoff creates a Backoff object. Pass a Context that can also terminate the operation.
func NewBackoff(ctx context.Context, cfg BackoffConfig) *Backoff {
	return &Backoff{
		cfg:          cfg,
		ctx:          ctx,
		nextDelayMin: cfg.MinBackoff,
		nextDelayMax: doubleDuration(cfg.MinBackoff, cfg.MaxBackoff),
	}
}

// Reset the Backoff back to its initial condition
func (b *Backoff) Reset() {
	b.numRetries = 0
	b.nextDelayMin = b.cfg.MinBackoff
	b.nextDelayMax = doubleDuration(b.cfg.MinBackoff, b.cfg.MaxBackoff)
}

// Ongoing returns true if caller should keep going
func (b *Backoff) Ongoing() bool {
	// Stop if Context has errored or max retry count is exceeded
	return b.ctx.Err() == nil && (b.cfg.MaxRetries == 0 || b.numRetries < b.cfg.MaxRetries)
}

// Err returns the reason for terminating the backoff, or nil if it didn't terminate
func (b *Backoff) Err() error {
	if b.ctx.Err() != nil {
		return b.ctx.Err()
	}
	if b.cfg.MaxRetries != 0 && b.numRetries >= b.cfg.MaxRetries {
		return fmt.Errorf("terminated after %d retries", b.numRetries)
	}
	return nil
}

// NumRetries returns the number of retries so far
func (b *Backoff) NumRetries() int {
	return b.numRetries
}

// Wait sleeps for the backoff time then increases the retry count and backoff time
// Returns immediately if Context is terminated
func (b *Backoff) Wait() {
	// Increase the number of retries and get the next delay
	sleepTime := b.NextDelay()

	if b.Ongoing() {
		select {
		case <-b.ctx.Done():
		case <-time.After(sleepTime):
		}
	}
}

func (b *Backoff) NextDelay() time.Duration {
	b.numRetries++

	// Handle the edge case the min and max have the same value
	// (or due to some misconfig max is < min)
	if b.nextDelayMin >= b.nextDelayMax {
		return b.nextDelayMin
	}

	// Add a jitter within the next exponential backoff range
	sleepTime := b.nextDelayMin + time.Duration(rand.Int63n(int64(b.nextDelayMax-b.nextDelayMin)))

	// Apply the exponential backoff to calculate the next jitter
	// range, unless we've already reached the max
	if b.nextDelayMax < b.cfg.MaxBackoff {
		b.nextDelayMin = doubleDuration(b.nextDelayMin, b.cfg.MaxBackoff)
		b.nextDelayMax = doubleDuration(b.nextDelayMax, b.cfg.MaxBackoff)
	}

	return sleepTime
}

func doubleDuration(value time.Duration, max time.Duration) time.Duration {
	value = value * 2

	if value <= max {
		return value
	}

	return max
}
