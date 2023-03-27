package ruler

import (
	"context"
	"hash"
	"math"
	"sync"
	"time"

	"github.com/grafana/loki/pkg/logqlmodel"
)

// EvaluatorWithJitter wraps a given Evaluator. It applies a consistent jitter based on a rule's query string by hashing
// the query string to produce a 32-bit unsigned integer. From this hash, we calculate a ratio between 0 and 1 and
// multiply it by the configured max jitter. This ratio is used to delay evaluation by a consistent amount of random time.
//
// Consistent jitter is important because it allows rules to be evaluated on a regular, predictable cadence
// while also ensuring that we spread evaluations across the configured jitter window to avoid resource contention scenarios.
type EvaluatorWithJitter struct {
	mu sync.Mutex

	inner     Evaluator
	maxJitter time.Duration
	hasher    hash.Hash32
}

func NewEvaluatorWithJitter(inner Evaluator, maxJitter time.Duration, hasher hash.Hash32) Evaluator {
	if maxJitter <= 0 {
		// jitter is disabled or invalid
		return inner
	}

	return &EvaluatorWithJitter{
		inner:     inner,
		maxJitter: maxJitter,
		hasher:    hasher,
	}
}

func (e *EvaluatorWithJitter) Eval(ctx context.Context, qs string, now time.Time) (*logqlmodel.Result, error) {
	time.Sleep(e.calculateJitter(qs))

	return e.inner.Eval(ctx, qs, now)
}

func (e *EvaluatorWithJitter) calculateJitter(qs string) time.Duration {
	var h uint32

	// rules can be evaluated concurrently, so we protect the hasher with a mutex
	e.mu.Lock()
	{
		_, _ = e.hasher.Write([]byte(qs))
		h = e.hasher.Sum32()
		e.hasher.Reset()
	}
	e.mu.Unlock()

	ratio := float32(h) / math.MaxUint32
	return time.Duration(ratio * float32(e.maxJitter.Nanoseconds()))
}
