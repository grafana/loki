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
	MinBackoff time.Duration // start backoff at this level
	MaxBackoff time.Duration // increase exponentially to this level
	MaxRetries int           // give up after this many; zero means infinite retries
}

// RegisterFlags for BackoffConfig.
func (cfg *BackoffConfig) RegisterFlags(prefix string, f *flag.FlagSet) {
	f.DurationVar(&cfg.MinBackoff, prefix+".backoff-min-period", 100*time.Millisecond, "Minimum delay when backing off.")
	f.DurationVar(&cfg.MaxBackoff, prefix+".backoff-max-period", 10*time.Second, "Maximum delay when backing off.")
	f.IntVar(&cfg.MaxRetries, prefix+".backoff-retries", 10, "Number of times to backoff and retry before failing.")
}

// Backoff implements exponential backoff with randomized wait times
type Backoff struct {
	cfg        BackoffConfig
	ctx        context.Context
	numRetries int
	duration   time.Duration
}

// NewBackoff creates a Backoff object. Pass a Context that can also terminate the operation.
func NewBackoff(ctx context.Context, cfg BackoffConfig) *Backoff {
	return &Backoff{
		cfg:      cfg,
		ctx:      ctx,
		duration: cfg.MinBackoff,
	}
}

// Reset the Backoff back to its initial condition
func (b *Backoff) Reset() {
	b.numRetries = 0
	b.duration = b.cfg.MinBackoff
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
	b.numRetries++
	// Based on the "Full Jitter" approach from https://www.awsarchitectureblog.com/2015/03/backoff.html
	// sleep = random_between(0, min(cap, base * 2 ** attempt))
	if b.Ongoing() {
		sleepTime := time.Duration(rand.Int63n(int64(b.duration)))
		select {
		case <-b.ctx.Done():
		case <-time.After(sleepTime):
		}
	}
	b.duration = b.duration * 2
	if b.duration > b.cfg.MaxBackoff {
		b.duration = b.cfg.MaxBackoff
	}
}
