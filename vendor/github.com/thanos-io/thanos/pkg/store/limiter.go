// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package store

import (
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

type SampleLimiter interface {
	Check(num uint64) error
}

// Limiter is a simple mechanism for checking if something has passed a certain threshold.
type Limiter struct {
	limit uint64

	// Counter metric which we will increase if Check() fails.
	failedCounter prometheus.Counter
}

// NewLimiter returns a new limiter with a specified limit. 0 disables the limit.
func NewLimiter(limit uint64, ctr prometheus.Counter) *Limiter {
	return &Limiter{limit: limit, failedCounter: ctr}
}

// Check checks if the passed number exceeds the limits or not.
func (l *Limiter) Check(num uint64) error {
	if l.limit == 0 {
		return nil
	}
	if num > l.limit {
		l.failedCounter.Inc()
		return errors.Errorf("limit %v violated (got %v)", l.limit, num)
	}
	return nil
}
