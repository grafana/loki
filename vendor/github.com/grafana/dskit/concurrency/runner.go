package concurrency

import (
	"context"
	"sync"

	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/dskit/internal/math"
	"github.com/grafana/dskit/multierror"
)

// ForEachUser runs the provided userFunc for each userIDs up to concurrency concurrent workers.
// In case userFunc returns error, it will continue to process remaining users but returns an
// error with all errors userFunc has returned.
func ForEachUser(ctx context.Context, userIDs []string, concurrency int, userFunc func(ctx context.Context, userID string) error) error {
	if len(userIDs) == 0 {
		return nil
	}

	// Push all jobs to a channel.
	ch := make(chan string, len(userIDs))
	for _, userID := range userIDs {
		ch <- userID
	}
	close(ch)

	// Keep track of all errors occurred.
	errs := multierror.MultiError{}
	errsMx := sync.Mutex{}

	wg := sync.WaitGroup{}
	for ix := 0; ix < math.Min(concurrency, len(userIDs)); ix++ {
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

	// wait for ongoing workers to finish.
	wg.Wait()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	return errs.Err()
}

// ForEach runs the provided jobFunc for each job up to concurrency concurrent workers.
// The execution breaks on first error encountered.
//
// Deprecated: use ForEachJob instead.
func ForEach(ctx context.Context, jobs []interface{}, concurrency int, jobFunc func(ctx context.Context, job interface{}) error) error {
	return ForEachJob(ctx, len(jobs), concurrency, func(ctx context.Context, idx int) error {
		return jobFunc(ctx, jobs[idx])
	})
}

// CreateJobsFromStrings is an utility to create jobs from an slice of strings.
//
// Deprecated: will be removed as it's not needed when using ForEachJob.
func CreateJobsFromStrings(values []string) []interface{} {
	jobs := make([]interface{}, len(values))
	for i := 0; i < len(values); i++ {
		jobs[i] = values[i]
	}
	return jobs
}

// ForEachJob runs the provided jobFunc for each job index in [0, jobs) up to concurrency concurrent workers.
// The execution breaks on first error encountered.
func ForEachJob(ctx context.Context, jobs int, concurrency int, jobFunc func(ctx context.Context, idx int) error) error {
	if jobs == 0 {
		return nil
	}

	// Initialise indexes with -1 so first Inc() returns index 0.
	indexes := atomic.NewInt64(-1)

	// Start workers to process jobs.
	g, ctx := errgroup.WithContext(ctx)
	for ix := 0; ix < math.Min(concurrency, jobs); ix++ {
		g.Go(func() error {
			for ctx.Err() == nil {
				idx := int(indexes.Inc())
				if idx >= jobs {
					return nil
				}

				if err := jobFunc(ctx, idx); err != nil {
					return err
				}
			}
			return ctx.Err()
		})
	}

	// Wait until done (or context has canceled).
	return g.Wait()
}
