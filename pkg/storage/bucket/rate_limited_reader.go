package bucket

import (
	"context"
	"io"

	"golang.org/x/time/rate"
)

// minReadSize is the minimum chunk size for reading data.
// This ensures we read in reasonable-sized batches rather than very small ones.
const minReadSize = 512

type rateLimiterKey struct{}

// queryRateLimiter holds a rate limiter for a query.
type queryRateLimiter struct {
	limiter *rate.Limiter
}

// WithQueryBandwidthLimit adds a bandwidth limit to the context for this query.
// All GetObject calls within this query will share the same rate limiter.
// bytesPerSecond is the maximum bytes per second for this query.
// burstBytes is the maximum burst bytes allowed.
// If bytesPerSecond is 0 or negative, rate limiting is disabled.
func WithQueryBandwidthLimit(ctx context.Context, bytesPerSecond int64, burstBytes int64) context.Context {
	if bytesPerSecond <= 0 {
		return ctx
	}

	// Set burst to rate if not specified or invalid
	burst := int(bytesPerSecond)
	if burstBytes > 0 {
		burst = int(burstBytes)
	}

	// Create a limiter with the specified rate and burst
	limiter := rate.NewLimiter(rate.Limit(bytesPerSecond), burst)

	return context.WithValue(ctx, rateLimiterKey{}, &queryRateLimiter{
		limiter: limiter,
	})
}

// getQueryRateLimiter extracts the rate limiter from context.
// Returns nil if no rate limiter is configured.
func getQueryRateLimiter(ctx context.Context) *rate.Limiter {
	rl, ok := ctx.Value(rateLimiterKey{}).(*queryRateLimiter)
	if !ok || rl == nil {
		return nil
	}
	return rl.limiter
}

// rateLimitedReader wraps an io.ReadCloser and limits the read rate using a shared limiter.
type rateLimitedReader struct {
	io.ReadCloser
	limiter *rate.Limiter
	ctx     context.Context
}

// Read reads data from the underlying reader while respecting the rate limit.
// It reads in batches that don't exceed the burst size, waiting for rate limiter
// approval before each read to ensure we don't read ahead of the rate limit.
func (r *rateLimitedReader) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	burst := r.limiter.Burst()
	if burst <= 0 {
		// This should never happen with limiters created by WithQueryBandwidthLimit
		// but handle it defensively - if burst is invalid, we can't rate limit
		return r.ReadCloser.Read(p)
	}

	// Read in batches, waiting for rate limiter approval before each batch
	totalRead := 0
	for totalRead < len(p) {
		remaining := len(p) - totalRead
		// Use minReadSize as the read size, but don't exceed burst or remaining buffer
		readSize := minReadSize
		if readSize > remaining {
			readSize = remaining
		}
		if readSize > burst {
			readSize = burst
		}

		// Wait for rate limiter approval before reading this batch
		if err := r.limiter.WaitN(r.ctx, readSize); err != nil {
			if totalRead > 0 {
				// Return what we've read so far
				return totalRead, nil
			}
			return 0, err
		}

		// Read from underlying reader (up to the approved read size)
		batch := p[totalRead : totalRead+readSize]
		read, err := r.ReadCloser.Read(batch)
		totalRead += read

		if err != nil {
			return totalRead, err
		}

		// If we read less than requested, we've hit EOF or the reader is done
		if read < readSize {
			return totalRead, err
		}
	}

	return totalRead, nil
}
