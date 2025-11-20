package bucket

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"golang.org/x/time/rate"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
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
	logger  log.Logger
}

func newRateLimitedReader(ctx context.Context, readCloser io.ReadCloser, logger log.Logger) *rateLimitedReader {
	return &rateLimitedReader{
		ReadCloser: readCloser,
		limiter:    getQueryRateLimiter(ctx),
		ctx:        ctx,
		logger:     logger,
	}
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

	// Cap the read size to the minimum read size and the burst
	minReadSize := min(minReadSize, burst)
	totalRead := 0

	// Other logging stats
	var (
		rateLimitedCount int
		totalWaitTime    time.Duration
		maxWaitTime      time.Duration
	)

	// Defer logging to ensure it happens on all exit paths
	defer func() {
		if rateLimitedCount > 0 && r.logger != nil {
			logger := util_log.WithContext(r.ctx, r.logger)
			level.Debug(logger).Log(
				"msg", "query rate limited during bucket read operation",
				"rateLimitedCount", rateLimitedCount,
				"totalWaitTime", totalWaitTime.String(),
				"maxWaitTime", maxWaitTime.String(),
				"readBufferSize", humanize.Bytes(uint64(len(p))),
				"readBytes", humanize.Bytes(uint64(totalRead)),
				"remainingBytes", humanize.Bytes(uint64(len(p)-totalRead)),
				"err", err,
			)
		}
	}()

	for totalRead < len(p) {
		remaining := len(p) - totalRead
		// Use minReadSize but cap to the remaining
		readSize := min(minReadSize, remaining)

		// Reserve rate limiter tokens for this batch read
		reservation := r.limiter.ReserveN(time.Now(), readSize)
		if !reservation.OK() {
			// Reservation failed (e.g., readSize > burst), return error
			// This should not happen in practice since we cap readSize to burst
			if totalRead > 0 {
				return totalRead, nil
			}
			return 0, fmt.Errorf("rate limited reader: reservation failed. readSize (%d) > burst: (%d)?", readSize, burst)
		}

		// If we need to wait, record the logging stats and wait for the delay
		if delay := reservation.Delay(); delay > 0 {
			rateLimitedCount++
			totalWaitTime += delay
			maxWaitTime = max(maxWaitTime, delay)

			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
				// Delay completed, proceed
			case <-r.ctx.Done():
				timer.Stop()
				reservation.Cancel()
				if totalRead > 0 {
					return totalRead, nil
				}
				return 0, r.ctx.Err()
			}
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
