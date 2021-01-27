package concurrency

import (
	"context"
	"sync"

	tsdb_errors "github.com/prometheus/prometheus/tsdb/errors"
)

// ForEachUser runs the provided userFunc for each userIDs up to concurrency concurrent workers.
// In case userFunc returns error, it will continue to process remaining users but returns an
// error with all errors userFunc has returned.
func ForEachUser(ctx context.Context, userIDs []string, concurrency int, userFunc func(ctx context.Context, userID string) error) error {
	wg := sync.WaitGroup{}
	ch := make(chan string)

	// Keep track of all errors occurred.
	errs := tsdb_errors.NewMulti()
	errsMx := sync.Mutex{}

	for ix := 0; ix < concurrency; ix++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for userID := range ch {
				// Ensure the context has not been canceled (ie. shutdown has been triggered).
				if ctx.Err() != nil {
					break
				}

				if err := userFunc(ctx, userID); err != nil {
					errsMx.Lock()
					errs.Add(err)
					errsMx.Unlock()
				}
			}
		}()
	}

sendLoop:
	for _, userID := range userIDs {
		select {
		case ch <- userID:
			// ok
		case <-ctx.Done():
			// don't start new tasks.
			break sendLoop
		}
	}

	close(ch)

	// wait for ongoing workers to finish.
	wg.Wait()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	errsMx.Lock()
	defer errsMx.Unlock()
	return errs.Err()
}
