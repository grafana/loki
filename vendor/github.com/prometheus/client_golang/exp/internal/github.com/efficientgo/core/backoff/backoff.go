// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

// Initially copied from Cortex project.

// Package backoff implements backoff timers which increases wait time on every retry, incredibly useful
// in distributed system timeout functionalities.
package backoff

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// Config configures a Backoff.
type Config struct {
	Min        time.Duration `yaml:"min_period"`  // Start backoff at this level
	Max        time.Duration `yaml:"max_period"`  // Increase exponentially to this level
	MaxRetries int           `yaml:"max_retries"` // Give up after this many; zero means infinite retries
}

// Backoff implements exponential backoff with randomized wait times.
type Backoff struct {
	cfg          Config
	ctx          context.Context
	numRetries   int
	nextDelayMin time.Duration
	nextDelayMax time.Duration
}

// New creates a Backoff object. Pass a Context that can also terminate the operation.
func New(ctx context.Context, cfg Config) *Backoff {
	return &Backoff{
		cfg:          cfg,
		ctx:          ctx,
		nextDelayMin: cfg.Min,
		nextDelayMax: doubleDuration(cfg.Min, cfg.Max),
	}
}

// Reset the Backoff back to its initial condition.
func (b *Backoff) Reset() {
	b.numRetries = 0
	b.nextDelayMin = b.cfg.Min
	b.nextDelayMax = doubleDuration(b.cfg.Min, b.cfg.Max)
}

// Ongoing returns true if caller should keep going.
func (b *Backoff) Ongoing() bool {
	// Stop if Context has errored or max retry count is exceeded.
	return b.ctx.Err() == nil && (b.cfg.MaxRetries == 0 || b.numRetries < b.cfg.MaxRetries)
}

// Err returns the reason for terminating the backoff, or nil if it didn't terminate.
func (b *Backoff) Err() error {
	if b.ctx.Err() != nil {
		return b.ctx.Err()
	}
	if b.cfg.MaxRetries != 0 && b.numRetries >= b.cfg.MaxRetries {
		return fmt.Errorf("terminated after %d retries", b.numRetries)
	}
	return nil
}

// NumRetries returns the number of retries so far.
func (b *Backoff) NumRetries() int {
	return b.numRetries
}

// Wait sleeps for the backoff time then increases the retry count and backoff time.
// Returns immediately if Context is terminated.
func (b *Backoff) Wait() {
	// Increase the number of retries and get the next delay.
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
	// (or due to some misconfig max is < min).
	if b.nextDelayMin >= b.nextDelayMax {
		return b.nextDelayMin
	}

	// Add a jitter within the next exponential backoff range.
	sleepTime := b.nextDelayMin + time.Duration(rand.Int63n(int64(b.nextDelayMax-b.nextDelayMin)))

	// Apply the exponential backoff to calculate the next jitter
	// range, unless we've already reached the max.
	if b.nextDelayMax < b.cfg.Max {
		b.nextDelayMin = doubleDuration(b.nextDelayMin, b.cfg.Max)
		b.nextDelayMax = doubleDuration(b.nextDelayMax, b.cfg.Max)
	}

	return sleepTime
}

func doubleDuration(value, maxValue time.Duration) time.Duration {
	value = value * 2
	if value <= maxValue {
		return value
	}
	return maxValue
}
