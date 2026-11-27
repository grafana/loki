package multidb

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9/internal"
)

// Default probe settings, shared by the root default policy and the multidb
// package.
const (
	DefaultProbes       = 3
	DefaultProbeDelay   = 500 * time.Millisecond
	DefaultProbeTimeout = 3 * time.Second
)

// ProbeConfig is how one health check is probed. Its fields match
// redis.MultiDBHealthCheckConfig so the two convert directly.
type ProbeConfig struct {
	Probes  int
	Delay   time.Duration
	Timeout time.Duration
}

// ProbeFunc runs one probe of a health check.
type ProbeFunc func(ctx context.Context) (bool, error)

// ProbeRunner interprets the probes of one health check (all / majority /
// any) and reports whether the check passed.
type ProbeRunner func(ctx context.Context, cfg ProbeConfig, probe ProbeFunc) bool

// RunChecks runs n checks concurrently and returns true only if all pass
// (AND across checks). check(ctx, i) evaluates check i. A panicking check
// counts as failed. The first failure cancels the others.
func RunChecks(ctx context.Context, n int, check func(ctx context.Context, i int) bool) bool {
	if n == 0 {
		return true
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Buffered so a worker never blocks on send after an early return.
	results := make(chan bool, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results <- runCheckSafely(ctx, i, check)
		}(i)
	}
	go func() { wg.Wait(); close(results) }()

	for ok := range results {
		if !ok {
			return false
		}
	}
	return true
}

// runCheckSafely runs one check and turns a panic into a failure, so every
// worker sends exactly one result.
func runCheckSafely(ctx context.Context, i int, check func(context.Context, int) bool) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			internal.Logger.Printf(ctx, "multidb: health check panicked: %v", r)
			ok = false
		}
	}()
	return check(ctx, i)
}

// ProbesAllMustPass runs cfg.Probes probes; all must pass.
func ProbesAllMustPass(ctx context.Context, cfg ProbeConfig, probe ProbeFunc) bool {
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	for i := 0; i < cfg.Probes; i++ {
		ok, _ := probe(ctx)
		// A pass that arrives after the check's Timeout does not count:
		// the timeout bounds how long a healthy member may take to answer,
		// and a check that ignores ctx can otherwise return true late.
		if !ok || ctx.Err() != nil {
			return false
		}
		if i < cfg.Probes-1 && !sleepCtx(ctx, cfg.Delay) {
			return false
		}
	}
	return true
}

// ProbesMajority runs probes until a strict majority passed or failed.
func ProbesMajority(ctx context.Context, cfg ProbeConfig, probe ProbeFunc) bool {
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	// Strict majority: more than half must pass.
	allowedFailures := (cfg.Probes - 1) / 2
	requiredSuccesses := cfg.Probes - allowedFailures
	failures, successes := 0, 0

	for i := 0; i < cfg.Probes; i++ {
		ok, _ := probe(ctx)
		if ok && ctx.Err() != nil {
			// Late pass (see ProbesAllMustPass): counts as a failure.
			ok = false
		}
		if ok {
			successes++
			if successes >= requiredSuccesses {
				return true
			}
		} else {
			failures++
			if failures > allowedFailures {
				return false
			}
		}
		if i < cfg.Probes-1 && !sleepCtx(ctx, cfg.Delay) {
			return false
		}
	}
	return true
}

// ProbesAny runs probes until one passes.
func ProbesAny(ctx context.Context, cfg ProbeConfig, probe ProbeFunc) bool {
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	for i := 0; i < cfg.Probes; i++ {
		ok, _ := probe(ctx)
		if ctx.Err() != nil {
			// Late pass (see ProbesAllMustPass): does not count, and no
			// probe after the timeout can.
			return false
		}
		if ok {
			return true
		}
		if i < cfg.Probes-1 && !sleepCtx(ctx, cfg.Delay) {
			return false
		}
	}
	return false
}

// sleepCtx waits d, or returns false when ctx ends first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
