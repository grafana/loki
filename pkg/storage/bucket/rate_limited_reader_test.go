package bucket

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRateLimitedReader(t *testing.T) {
	tests := []struct {
		name           string
		dataSize       int
		rateLimit      int64 // bytes per second, 0 means unlimited
		burstLimit     int64 // burst bytes, 0 means use rate limit
		expectThrottle bool
		minDuration    time.Duration // minimum expected duration if throttled
	}{
		{
			name:           "no rate limit configured",
			dataSize:       1000,
			rateLimit:      0,
			burstLimit:     0,
			expectThrottle: false,
		},
		{
			name:           "rate limit higher than data size",
			dataSize:       1000,
			rateLimit:      100000, // 100KB/s, much higher than data
			burstLimit:     0,
			expectThrottle: false, // Burst allows immediate read
		},
		{
			name:           "rate limit throttles read",
			dataSize:       1000, // 1KB
			rateLimit:      100,  // 100 bytes/s
			burstLimit:     0,    // Defaults to rate limit
			expectThrottle: true,
			// First 100 bytes are immediate (burst), remaining 900 bytes at 100 bytes/s = 9 seconds
			minDuration: 8 * time.Second,
		},
		{
			name:           "rate limit with custom burst",
			dataSize:       1000, // 1KB
			rateLimit:      100,  // 100 bytes/s
			burstLimit:     200,  // 200 bytes burst
			expectThrottle: true,
			// First 200 bytes are immediate (burst), remaining 800 bytes at 100 bytes/s = 8 seconds
			minDuration: 7 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test data
			data := make([]byte, tt.dataSize)
			for i := range data {
				data[i] = byte(i % 256)
			}

			// Create reader
			reader := io.NopCloser(bytes.NewReader(data))

			// Configure context with rate limit (or not)
			ctx := context.Background()
			if tt.rateLimit > 0 {
				ctx = WithQueryBandwidthLimit(ctx, tt.rateLimit, tt.burstLimit)
			}

			// Create rate-limited reader if limiter exists
			var rlReader io.ReadCloser = reader
			if limiter := getQueryRateLimiter(ctx); limiter != nil {
				rlReader = &rateLimitedReader{
					ReadCloser: reader,
					limiter:    limiter,
					ctx:        ctx,
				}
			}

			// Read data and measure time
			start := time.Now()
			buf := make([]byte, tt.dataSize)
			n, err := rlReader.Read(buf)
			elapsed := time.Since(start)

			require.NoError(t, err)
			require.Equal(t, tt.dataSize, n)
			require.Equal(t, data, buf)

			// Verify rate limit was respected
			if tt.expectThrottle {
				require.GreaterOrEqual(t, elapsed, tt.minDuration,
					"read should be throttled and take at least %v (read %d bytes at %d bytes/s)", tt.minDuration, tt.dataSize, tt.rateLimit)
			}
		})
	}
}

func TestRateLimitedReader_MultipleReadersShareLimiter(t *testing.T) {
	// Test that multiple readers sharing the same limiter respect the total rate limit
	dataSize := 500         // 500 bytes per reader
	rateLimit := int64(200) // 200 bytes/s total
	numReaders := 2

	// Create test data
	data := make([]byte, dataSize)
	for i := range data {
		data[i] = byte(i % 256)
	}

	// Configure context with rate limit
	ctx := WithQueryBandwidthLimit(context.Background(), rateLimit, 0)
	limiter := getQueryRateLimiter(ctx)
	require.NotNil(t, limiter)

	// Create multiple readers sharing the same limiter
	readers := make([]io.ReadCloser, numReaders)
	for i := 0; i < numReaders; i++ {
		reader := io.NopCloser(bytes.NewReader(data))
		readers[i] = &rateLimitedReader{
			ReadCloser: reader,
			limiter:    limiter,
			ctx:        ctx,
		}
	}

	// Read from all readers concurrently
	start := time.Now()
	done := make(chan bool, numReaders)
	buffers := make([][]byte, numReaders)

	for i := 0; i < numReaders; i++ {
		buffers[i] = make([]byte, dataSize)
		go func(idx int) {
			_, err := readers[idx].Read(buffers[idx])
			require.NoError(t, err)
			done <- true
		}(i)
	}

	// Wait for all reads to complete
	for i := 0; i < numReaders; i++ {
		<-done
	}
	elapsed := time.Since(start)

	// Verify all data was read correctly
	for i := 0; i < numReaders; i++ {
		require.Equal(t, data, buffers[i])
	}

	// Total bytes read: 2 * 500 = 1000 bytes
	// At 200 bytes/s with 200 byte burst: first 200 bytes immediate, remaining 800 bytes at 200 bytes/s = 4 seconds
	require.GreaterOrEqual(t, elapsed, 3*time.Second,
		"concurrent reads should share rate limit and be throttled (read %d bytes total at %d bytes/s)", dataSize*numReaders, rateLimit)
}
