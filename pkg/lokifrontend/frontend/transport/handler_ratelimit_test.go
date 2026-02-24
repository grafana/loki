//go:build integration
// +build integration

package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
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

// MockRoundTripper is a test round tripper that returns a fixed response
type MockRoundTripper struct {
	Response *http.Response
	Error    error
}

func (m *MockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	if m.Response != nil {
		return m.Response, nil
	}
	// Default response
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
		Header:     make(http.Header),
	}, nil
}

// TestHandlerWithRateLimiting tests the frontend handler with rate limiting enabled
func TestHandlerWithRateLimiting(t *testing.T) {
	// This test requires a running Redis instance
	logger := log.NewNopLogger()

	config := RateLimitConfig{
		Enable:              true,
		RedisAddress:        "localhost:6379",
		RedisPassword:       "",
		RedisDB:             2, // Use a different DB for tests
		DefaultRequestLimit: 5,
		DefaultByteLimit:    1048576,
		DefaultWindow:       time.Second,
		Prefix:              "loki-frontend-test",
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

	// Create mock round tripper
	roundTripper := &MockRoundTripper{}

	// Create handler with rate limiting
	handler := NewHandlerWithRateLimit(
		handlerConfig,
		roundTripper,
		logger,
		prometheus.NewRegistry(),
		"test",
		adapter,
	)

	// Test: Make requests up to the limit for tenant-a
	tenantA := "tenant-a"
	for i := 0; i < config.DefaultRequestLimit; i++ {
		req := httptest.NewRequest("GET", "/loki/api/v1/labels", nil)
		ctx := user.InjectOrgID(req.Context(), tenantA)
		req = req.WithContext(ctx)
		req.RemoteAddr = "192.168.1.100:1234"

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code,
			"Request %d for tenant %s should succeed", i+1, tenantA)

		// Check rate limit headers
		assert.NotEmpty(t, rec.Header().Get("RateLimit-Request-Limit"))
		assert.NotEmpty(t, rec.Header().Get("RateLimit-Request-Remaining"))
		assert.NotEmpty(t, rec.Header().Get("RateLimit-Request-Reset"))
	}

	// Test: Next request should be rate limited
	req := httptest.NewRequest("GET", "/loki/api/v1/labels", nil)
	ctx := user.InjectOrgID(req.Context(), tenantA)
	req = req.WithContext(ctx)
	req.RemoteAddr = "192.168.1.100:1234"

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code,
		"Request should be rate limited for tenant %s", tenantA)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))

	// Test: Different tenant should have separate limits
	tenantB := "tenant-b"
	for i := 0; i < config.DefaultRequestLimit; i++ {
		req := httptest.NewRequest("GET", "/loki/api/v1/labels", nil)
		ctx := user.InjectOrgID(req.Context(), tenantB)
		req = req.WithContext(ctx)
		req.RemoteAddr = "192.168.1.101:1234"

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code,
			"Request %d for tenant %s should succeed", i+1, tenantB)
	}
}

// TestHandlerWithoutRateLimiting tests the handler works correctly when rate limiting is disabled
func TestHandlerWithoutRateLimiting(t *testing.T) {
	logger := log.NewNopLogger()

	// Create handler configuration
	handlerConfig := HandlerConfig{
		QueryStatsEnabled: false,
	}

	// Create mock round tripper
	roundTripper := &MockRoundTripper{}

	// Create handler without rate limiting (nil adapter)
	handler := NewHandlerWithRateLimit(
		handlerConfig,
		roundTripper,
		logger,
		prometheus.NewRegistry(),
		"test",
		nil, // No rate limiting
	)

	// Test: Make many requests, all should succeed
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest("GET", "/loki/api/v1/labels", nil)
		req.RemoteAddr = "192.168.1.100:1234"

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code,
			"Request %d should succeed without rate limiting", i+1)

		// Check no rate limit headers
		assert.Empty(t, rec.Header().Get("RateLimit-Request-Limit"))
		assert.Empty(t, rec.Header().Get("RateLimit-Request-Remaining"))
	}
}

// TestHandlerRateLimitByIP tests rate limiting by IP when tenant ID is not available
func TestHandlerRateLimitByIP(t *testing.T) {
	logger := log.NewNopLogger()

	config := RateLimitConfig{
		Enable:              true,
		RedisAddress:        "localhost:6379",
		RedisPassword:       "",
		RedisDB:             3,
		DefaultRequestLimit: 3,
		DefaultByteLimit:    1048576,
		DefaultWindow:       time.Second,
		Prefix:              "loki-ip-test",
		RateLimitByTenant:   true,
		FallbackToIP:        true, // Enable IP fallback
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

	// Create handler
	handlerConfig := HandlerConfig{
		QueryStatsEnabled: false,
	}

	roundTripper := &MockRoundTripper{}

	handler := NewHandlerWithRateLimit(
		handlerConfig,
		roundTripper,
		logger,
		prometheus.NewRegistry(),
		"test",
		adapter,
	)

	// Test: Make requests without tenant ID (should use IP)
	clientIP := "192.168.1.200:1234"
	for i := 0; i < config.DefaultRequestLimit; i++ {
		req := httptest.NewRequest("GET", "/loki/api/v1/labels", nil)
		req.RemoteAddr = clientIP
		// No tenant ID injected

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code,
			"Request %d from IP %s should succeed", i+1, clientIP)
	}

	// Next request from same IP should be rate limited
	req := httptest.NewRequest("GET", "/loki/api/v1/labels", nil)
	req.RemoteAddr = clientIP

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code,
		"Request from IP %s should be rate limited", clientIP)

	// Different IP should have separate limit
	differentIP := "192.168.1.201:1234"
	req = httptest.NewRequest("GET", "/loki/api/v1/labels", nil)
	req.RemoteAddr = differentIP

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code,
		"Request from different IP %s should succeed", differentIP)
}
