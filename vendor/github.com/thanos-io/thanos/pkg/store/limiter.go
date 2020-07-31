// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package store

import (
	"sync"
	"sync/atomic"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

type ChunksLimiter interface {
	// Reserve num chunks out of the total number of chunks enforced by the limiter.
	// Returns an error if the limit has been exceeded. This function must be
	// goroutine safe.
	Reserve(num uint64) error
}

// ChunksLimiterFactory is used to create a new ChunksLimiter. The factory is useful for
// projects depending on Thanos (eg. Cortex) which have dynamic limits.
type ChunksLimiterFactory func(failedCounter prometheus.Counter) ChunksLimiter

// Limiter is a simple mechanism for checking if something has passed a certain threshold.
type Limiter struct {
	limit    uint64
	reserved uint64

	// Counter metric which we will increase if limit is exceeded.
	failedCounter prometheus.Counter
	failedOnce    sync.Once
}

// NewLimiter returns a new limiter with a specified limit. 0 disables the limit.
func NewLimiter(limit uint64, ctr prometheus.Counter) *Limiter {
	return &Limiter{limit: limit, failedCounter: ctr}
}

// Reserve implements ChunksLimiter.
func (l *Limiter) Reserve(num uint64) error {
	if l.limit == 0 {
		return nil
	}
	if reserved := atomic.AddUint64(&l.reserved, num); reserved > l.limit {
		// We need to protect from the counter being incremented twice due to concurrency
		// while calling Reserve().
		l.failedOnce.Do(l.failedCounter.Inc)
		return errors.Errorf("limit %v violated (got %v)", l.limit, reserved)
	}
	return nil
}

// NewChunksLimiterFactory makes a new ChunksLimiterFactory with a static limit.
func NewChunksLimiterFactory(limit uint64) ChunksLimiterFactory {
	return func(failedCounter prometheus.Counter) ChunksLimiter {
		return NewLimiter(limit, failedCounter)
	}
}
