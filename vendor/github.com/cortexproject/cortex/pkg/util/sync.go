package util

import (
	"context"
	"sync"
)

// WaitGroup calls Wait() on a sync.WaitGroup and return once the Wait() completed
// or the context is cancelled or times out, whatever occurs first. Returns the
// specific context error if the context is cancelled or times out before Wait()
// completes.
func WaitGroup(ctx context.Context, wg *sync.WaitGroup) error {
	c := make(chan struct{})

	go func() {
		defer close(c)
		wg.Wait()
	}()

	select {
	case <-c:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
