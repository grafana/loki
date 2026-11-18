// Package parallel provides helpers to run work on several goroutines.
package parallel

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// ForEachOrdered calls work(ctx, i) for each i in [0, n) on up to parallelism
// goroutines, and calls yield(i, result) with the result of each call, in the
// order of i. Only one goroutine calls yield at a time, so yield does not need
// to be safe for concurrent use.
//
// A task holds its slot until yield returns for its result. So at most
// parallelism results are running, waiting for an earlier result, or in yield
// at any time, even when an early task is slow. This bounds the memory of the
// results.
//
// The first error from work or yield cancels the context that work receives,
// skips the tasks that did not start, and becomes the return value. After the
// first error, yield does not run again. A parallelism below 1 runs the tasks
// one at a time. n must not be negative. ForEachOrdered returns after every
// started task returns.
func ForEachOrdered[T any](
	ctx context.Context,
	n, parallelism int,
	work func(ctx context.Context, i int) (T, error),
	yield func(i int, result T) error,
) error {
	var (
		g, gctx = errgroup.WithContext(ctx)
		slots   = make(chan struct{}, max(1, parallelism))
		results = make([]chan T, n)
	)
	for i := range results {
		results[i] = make(chan T, 1)
	}

	g.Go(func() error {
		for i := range n {
			select {
			case slots <- struct{}{}:
			case <-gctx.Done():
				return gctx.Err()
			}
			g.Go(func() error {
				result, err := work(gctx, i)
				if err != nil {
					return err
				}
				results[i] <- result
				return nil
			})
		}
		return nil
	})

	g.Go(func() error {
		for i := range n {
			select {
			case result := <-results[i]:
				// A result and the cancellation can be ready together, and select
				// picks one at random.
				if err := gctx.Err(); err != nil {
					return err
				}
				if err := yield(i, result); err != nil {
					return err
				}
			case <-gctx.Done():
				return gctx.Err()
			}
			<-slots
		}
		return nil
	})

	return g.Wait()
}
