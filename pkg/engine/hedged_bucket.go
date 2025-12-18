package engine

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/thanos-io/objstore"
)

type HedgedBucket struct {
	bucket      objstore.Bucket
	hedgeDelay  time.Duration
	maxRequests int
}

func NewHedgedBucket(
	bucket objstore.Bucket,
	hedgeDelay time.Duration,
	maxRequests int,
) *HedgedBucket {
	return &HedgedBucket{
		bucket:      bucket,
		hedgeDelay:  hedgeDelay,
		maxRequests: maxRequests,
	}
}

// Generic hedging function that works for any operation
func hedgeOperation[T any](
	ctx context.Context,
	h *HedgedBucket,
	fn func(context.Context) (T, error),
) (T, error) {
	type result struct {
		value    T
		err      error
		reqIndex int
	}

	resultCh := make(chan result, h.maxRequests)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	requestsLaunched := 0

	launchRequest := func(index int) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := fn(ctx)
			select {
			case resultCh <- result{value, err, index}:
			case <-ctx.Done():
				// Clean up if value has a Close method (for readers)
				if closer, ok := any(value).(io.Closer); ok && closer != nil {
					closer.Close()
				}
			}
		}()
	}

	// Launch first request immediately
	launchRequest(0)
	requestsLaunched++

	// Launch hedged requests after delays
	hedgeTimer := time.NewTimer(h.hedgeDelay)
	defer hedgeTimer.Stop()

	hedgingTriggered := false

	for {
		select {
		case res := <-resultCh:
			// Cancel remaining requests
			cancel()

			// Clean up remaining goroutines asynchronously
			go func() {
				wg.Wait()
				// Drain any remaining results and close resources
				for {
					select {
					case extraRes := <-resultCh:
						if closer, ok := any(extraRes.value).(io.Closer); ok && closer != nil {
							closer.Close()
						}
					default:
						return
					}
				}
			}()

			return res.value, res.err

		case <-hedgeTimer.C:
			if requestsLaunched >= h.maxRequests {
				continue
			}

			if !hedgingTriggered {
				hedgingTriggered = true
			}

			launchRequest(requestsLaunched)
			requestsLaunched++

			// Reset timer for next hedge
			hedgeTimer.Reset(h.hedgeDelay)

		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}
}

// GetRange implements hedged requests for range retrieval
func (h *HedgedBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	return hedgeOperation(ctx, h, func(ctx context.Context) (io.ReadCloser, error) {
		return h.bucket.GetRange(ctx, name, off, length)
	})
}

// Get implements hedged requests for full object retrieval
func (h *HedgedBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	return hedgeOperation(ctx, h, func(ctx context.Context) (io.ReadCloser, error) {
		return h.bucket.Get(ctx, name)
	})
}

// Attributes implements hedged requests for object metadata
func (h *HedgedBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	return hedgeOperation(ctx, h, func(ctx context.Context) (objstore.ObjectAttributes, error) {
		return h.bucket.Attributes(ctx, name)
	})
}

// Forward non-hedged methods to underlying bucket
func (h *HedgedBucket) Exists(ctx context.Context, name string) (bool, error) {
	return h.bucket.Exists(ctx, name)
}

func (h *HedgedBucket) IsObjNotFoundErr(err error) bool {
	return h.bucket.IsObjNotFoundErr(err)
}

func (h *HedgedBucket) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	return h.bucket.Iter(ctx, dir, f, options...)
}

func (h *HedgedBucket) Close() error {
	return h.bucket.Close()
}

func (h *HedgedBucket) Delete(ctx context.Context, name string) error {
	return h.bucket.Delete(ctx, name)
}

func (h *HedgedBucket) Upload(ctx context.Context, name string, r io.Reader) error {
	return h.bucket.Upload(ctx, name, r)
}

func (h *HedgedBucket) GetAndReplace(ctx context.Context, name string, f func(existing io.ReadCloser) (io.ReadCloser, error)) error {
	return h.bucket.GetAndReplace(ctx, name, f)
}

func (h *HedgedBucket) IsAccessDeniedErr(err error) bool {
	return h.bucket.IsAccessDeniedErr(err)
}

func (h *HedgedBucket) Name() string {
	return h.bucket.Name()
}

func (h *HedgedBucket) Provider() objstore.ObjProvider {
	return h.bucket.Provider()
}

func (h *HedgedBucket) IterWithAttributes(ctx context.Context, dir string, f func(objstore.IterObjectAttributes) error, options ...objstore.IterOption) error {
	return h.bucket.IterWithAttributes(ctx, dir, f, options...)
}

func (h *HedgedBucket) SupportedIterOptions() []objstore.IterOptionType {
	return h.bucket.SupportedIterOptions()
}
