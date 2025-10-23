package workflow

import (
	"context"

	"golang.org/x/sync/semaphore"
)

type weightedSemaphore struct {
	name     string
	capacity int64
	sem      *semaphore.Weighted
}

func newWeightedSemaphore(capacity int64, name string) *weightedSemaphore {
	return &weightedSemaphore{
		name:     name,
		capacity: capacity,
		sem:      semaphore.NewWeighted(capacity),
	}
}

func (b *weightedSemaphore) Acquire(ctx context.Context, n int64) error {
	return b.sem.Acquire(ctx, n)
}

func (b *weightedSemaphore) Release(n int64) {
	b.sem.Release(n)
}
