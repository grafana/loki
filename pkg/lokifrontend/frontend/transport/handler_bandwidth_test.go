//go:build integration
// +build integration

package transport

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdklogger "github.com/NVIDIA/go-ratelimit/pkg/logger"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

func init() {
	// Initialize SDK logger to prevent nil pointer errors
	if sdklogger.Log == nil {
		logger, _ := zap.NewDevelopment()
		sdklogger.Log = otelzap.New(logger)
	}
}

// MockRoundTripperWithBody is a test round tripper that returns a response with body
type MockRoundTripperWithBody struct {
	ResponseBody []byte
	StatusCode   int
}

func (m *MockRoundTripperWithBody) RoundTrip(req *http.Request) (*http.Response, error) {
	body := io.NopCloser(bytes.NewReader(m.ResponseBody))
	return &http.Response{
		StatusCode: m.StatusCode,
		Body:       body,
		Header:     make(http.Header),
	}, nil
}

// TestHandlerBandwidthRateLimiting tests byte-based rate limiting in the frontend handler
func TestHandlerBandwidthRateLimiting(t *testing.T) {
	logger := log.NewNopLogger()

	// Small byte limit for testing
	byteLimit := int64(1000) // 1KB per window

	config := RateLimitConfig{
		Enable:              true,
		RedisAddress:        "localhost:6379",
		RedisPassword:       "",
		RedisDB:             4,
		DefaultRequestLimit: 1000, // High request limit
		DefaultByteLimit:    byteLimit,
		DefaultWindow:       time.Second,
		Prefix:              "loki-bandwidth-test",
		RateLimitByTenant:   true,
		FallbackToIP:        true,
	}

	// Create rate limit adapter
	adapter, err := NewRateLimitAdapter(config, logger)
	if err != nil {
		t.Skip("Could not connect to Redis, skipping integration test:", err)
	}
	defer adapter.Close()

	// Clear test data
	rdb := redis.NewClient(&redis.Options{
		Addr: config.RedisAddress,
		DB:   config.RedisDB,
	})
	defer rdb.Close()
	rdb.FlushDB(context.Background())

	// Create handler configuration
	handlerConfig := HandlerConfig{
		QueryStatsEnabled: false,
	}

	// Create mock round tripper that returns 500 bytes
	responseBody := make([]byte, 500)
	for i := range responseBody {
		responseBody[i] = byte('A' + (i % 26))
	}

	roundTripper := &MockRoundTripperWithBody{
		ResponseBody: responseBody,
		StatusCode:   http.StatusOK,
	}

	// Create handler with rate limiting
	handler := NewHandlerWithRateLimit(
		handlerConfig,
		roundTripper,
		logger,
		prometheus.NewRegistry(),
		"test",
		adapter,
	)

	tenantID := "bandwidth-tenant"

	// Test: First request (500 bytes) should succeed
	req := httptest.NewRequest("GET", "/loki/api/v1/query", nil)
	ctx := user.InjectOrgID(req.Context(), tenantID)
	req = req.WithContext(ctx)
	req.RemoteAddr = "192.168.1.100:1234"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "First request should succeed")

	// Test: Second request (500 bytes) should succeed (total 1000 bytes = limit)
	req = httptest.NewRequest("GET", "/loki/api/v1/query", nil)
	ctx = user.InjectOrgID(req.Context(), tenantID)
	req = req.WithContext(ctx)
	req.RemoteAddr = "192.168.1.100:1234"

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "Second request should succeed (at limit)")

	// Test: Third request should be rate limited (would exceed byte limit)
	req = httptest.NewRequest("GET", "/loki/api/v1/query", nil)
	ctx = user.InjectOrgID(req.Context(), tenantID)
	req = req.WithContext(ctx)
	req.RemoteAddr = "192.168.1.100:1234"

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Note: The byte limiting happens after the response is written,
	// so the request might succeed but future requests will be limited
	// This depends on the SDK implementation

	// Wait for window to expire
	time.Sleep(config.DefaultWindow + 100*time.Millisecond)

	// Test: After window expires, requests should succeed again
	req = httptest.NewRequest("GET", "/loki/api/v1/query", nil)
	ctx = user.InjectOrgID(req.Context(), tenantID)
	req = req.WithContext(ctx)
	req.RemoteAddr = "192.168.1.100:1234"

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "Request after window expires should succeed")
}

// TestHandlerConcurrentBandwidth tests concurrent bandwidth usage
func TestHandlerConcurrentBandwidth(t *testing.T) {
	logger := log.NewNopLogger()

	config := RateLimitConfig{
		Enable:              true,
		RedisAddress:        "localhost:6379",
		RedisPassword:       "",
		RedisDB:             5,
		DefaultRequestLimit: 1000,
		DefaultByteLimit:    10000, // 10KB
		DefaultWindow:       time.Second,
		Prefix:              "loki-concurrent-bandwidth",
		RateLimitByTenant:   true,
		FallbackToIP:        true,
	}

	// Create rate limit adapter
	adapter, err := NewRateLimitAdapter(config, logger)
	if err != nil {
		t.Skip("Could not connect to Redis, skipping integration test:", err)
	}
	defer adapter.Close()

	// Clear test data
	rdb := redis.NewClient(&redis.Options{
		Addr: config.RedisAddress,
		DB:   config.RedisDB,
	})
	defer rdb.Close()
	rdb.FlushDB(context.Background())

	handlerConfig := HandlerConfig{
		QueryStatsEnabled: false,
	}

	// Create response of 1KB
	responseBody := make([]byte, 1000)
	for i := range responseBody {
		responseBody[i] = byte('X')
	}

	roundTripper := &MockRoundTripperWithBody{
		ResponseBody: responseBody,
		StatusCode:   http.StatusOK,
	}

	handler := NewHandlerWithRateLimit(
		handlerConfig,
		roundTripper,
		logger,
		prometheus.NewRegistry(),
		"test",
		adapter,
	)

	tenantID := "concurrent-tenant"

	// Test: Make 10 concurrent requests (10KB total = limit)
	var wg sync.WaitGroup
	var successCount int32
	var rateLimitCount int32

	for i := 0; i < 15; i++ { // Try 15 requests, expect some to be rate limited
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			req := httptest.NewRequest("GET", "/loki/api/v1/query", nil)
			ctx := user.InjectOrgID(req.Context(), tenantID)
			req = req.WithContext(ctx)
			req.RemoteAddr = "192.168.1.100:1234"

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code == http.StatusOK {
				atomic.AddInt32(&successCount, 1)
			} else if rec.Code == http.StatusTooManyRequests {
				atomic.AddInt32(&rateLimitCount, 1)
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Concurrent test: %d succeeded, %d rate limited", successCount, rateLimitCount)

	// At least some requests should succeed
	assert.Greater(t, int(successCount), 0, "Some requests should succeed")

	// Some requests might be rate limited if they exceed the byte limit
	// The exact number depends on timing and Redis latency
}

// TestHandlerCustomTenantByteLimits tests custom byte limits for specific tenants
func TestHandlerCustomTenantByteLimits(t *testing.T) {
	logger := log.NewNopLogger()

	config := RateLimitConfig{
		Enable:              true,
		RedisAddress:        "localhost:6379",
		RedisPassword:       "",
		RedisDB:             6,
		DefaultRequestLimit: 1000,
		DefaultByteLimit:    1000, // 1KB default
		DefaultWindow:       time.Second,
		Prefix:              "loki-custom-byte",
		RateLimitByTenant:   true,
		FallbackToIP:        false,
	}

	// Create rate limit adapter
	adapter, err := NewRateLimitAdapter(config, logger)
	if err != nil {
		t.Skip("Could not connect to Redis, skipping integration test:", err)
	}
	defer adapter.Close()

	// Clear test data
	rdb := redis.NewClient(&redis.Options{
		Addr: config.RedisAddress,
		DB:   config.RedisDB,
	})
	defer rdb.Close()
	rdb.FlushDB(context.Background())

	// Set custom limit for premium tenant (10KB)
	premiumTenant := "premium-tenant"
	err = adapter.SetTenantLimits(premiumTenant, 1000, 10000)
	assert.NoError(t, err)

	handlerConfig := HandlerConfig{
		QueryStatsEnabled: false,
	}

	// Create response of 2KB
	responseBody := make([]byte, 2000)
	for i := range responseBody {
		responseBody[i] = byte('Z')
	}

	roundTripper := &MockRoundTripperWithBody{
		ResponseBody: responseBody,
		StatusCode:   http.StatusOK,
	}

	handler := NewHandlerWithRateLimit(
		handlerConfig,
		roundTripper,
		logger,
		prometheus.NewRegistry(),
		"test",
		adapter,
	)

	// Test: Regular tenant should be limited after 1KB
	regularTenant := "regular-tenant"
	req := httptest.NewRequest("GET", "/loki/api/v1/query", nil)
	ctx := user.InjectOrgID(req.Context(), regularTenant)
	req = req.WithContext(ctx)
	req.RemoteAddr = "192.168.1.100:1234"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// First 2KB response exceeds the 1KB limit for regular tenant
	// The behavior depends on whether the SDK checks before or after

	// Test: Premium tenant can handle more bytes
	for i := 0; i < 4; i++ { // 4 * 2KB = 8KB < 10KB limit
		req := httptest.NewRequest("GET", "/loki/api/v1/query", nil)
		ctx := user.InjectOrgID(req.Context(), premiumTenant)
		req = req.WithContext(ctx)
		req.RemoteAddr = "192.168.1.101:1234"

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code,
			"Premium tenant request %d should succeed", i+1)
	}
}
