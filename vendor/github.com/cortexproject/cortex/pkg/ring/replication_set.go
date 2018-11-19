package ring

import (
	"context"
	"time"
)

// ReplicationSet describes the ingesters to talk to for a given key, and how
// many errors to tolerate.
type ReplicationSet struct {
	Ingesters []IngesterDesc
	MaxErrors int
}

// Do function f in parallel for all replicas in the set, erroring is we exceed
// MaxErrors and returning early otherwise.
func (r ReplicationSet) Do(ctx context.Context, delay time.Duration, f func(*IngesterDesc) (interface{}, error)) ([]interface{}, error) {
	var (
		errs        = make(chan error, len(r.Ingesters))
		resultsChan = make(chan interface{}, len(r.Ingesters))
		minSuccess  = len(r.Ingesters) - r.MaxErrors
		done        = make(chan struct{})
		forceStart  = make(chan struct{}, r.MaxErrors)
	)
	defer func() {
		close(done)
	}()

	for i := range r.Ingesters {
		go func(i int, ing *IngesterDesc) {
			// wait to send extra requests
			if i >= minSuccess && delay > 0 {
				after := time.NewTimer(delay)
				defer after.Stop()
				select {
				case <-done:
					return
				case <-forceStart:
				case <-after.C:
				}
			}
			result, err := f(ing)
			if err != nil {
				errs <- err
			} else {
				resultsChan <- result
			}
		}(i, &r.Ingesters[i])
	}

	var (
		numErrs    int
		numSuccess int
		results    = make([]interface{}, 0, len(r.Ingesters))
	)
	for numSuccess < minSuccess {
		select {
		case err := <-errs:
			numErrs++
			if numErrs > r.MaxErrors {
				return nil, err
			}
			// force one of the delayed requests to start
			forceStart <- struct{}{}

		case result := <-resultsChan:
			numSuccess++
			results = append(results, result)

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return results, nil
}
