package concurrency

import (
	"context"
	"sync"

	tsdb_errors "github.com/prometheus/prometheus/tsdb/errors"
	"golang.org/x/sync/errgroup"

	util_math "github.com/cortexproject/cortex/pkg/util/math"
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
	errs := tsdb_errors.NewMulti()
	errsMx := sync.Mutex{}

	wg := sync.WaitGroup{}
	for ix := 0; ix < util_math.Min(concurrency, len(userIDs)); ix++ {
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

	errsMx.Lock()
	defer errsMx.Unlock()
	return errs.Err()
}

// ForEach runs the provided jobFunc for each job up to concurrency concurrent workers.
// The execution breaks on first error encountered.
func ForEach(ctx context.Context, jobs []interface{}, concurrency int, jobFunc func(ctx context.Context, job interface{}) error) error {
	if len(jobs) == 0 {
		return nil
	}

	// Push all jobs to a channel.
	ch := make(chan interface{}, len(jobs))
	for _, job := range jobs {
		ch <- job
	}
	close(ch)

	// Start workers to process jobs.
	g, ctx := errgroup.WithContext(ctx)
	for ix := 0; ix < util_math.Min(concurrency, len(jobs)); ix++ {
		g.Go(func() error {
			for job := range ch {
				if err := ctx.Err(); err != nil {
					return err
				}

				if err := jobFunc(ctx, job); err != nil {
					return err
				}
			}

			return nil
		})
	}

	// Wait until done (or context has canceled).
	return g.Wait()
}

// CreateJobsFromStrings is an utility to create jobs from an slice of strings.
func CreateJobsFromStrings(values []string) []interface{} {
	jobs := make([]interface{}, len(values))
	for i := 0; i < len(values); i++ {
		jobs[i] = values[i]
	}
	return jobs
}
