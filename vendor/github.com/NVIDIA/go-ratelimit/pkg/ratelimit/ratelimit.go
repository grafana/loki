/*
 * Copyright (c) 2025, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
// Package ratelimit provides a flexible and configurable rate limiting solution
// for HTTP services based on Redis.
package ratelimit

import (
	"context"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/NVIDIA/go-ratelimit/pkg/api"
	"github.com/NVIDIA/go-ratelimit/pkg/config"
	"github.com/NVIDIA/go-ratelimit/pkg/limiter"
	"github.com/NVIDIA/go-ratelimit/pkg/middleware"
	"github.com/NVIDIA/go-ratelimit/pkg/utils"
)

// Options configures the rate limiter
type Options struct {
	// Redis client for storing rate limit data
	Redis *redis.Client

	// Prefix for Redis keys
	Prefix string

	// Default request limit per window
	RequestLimit int

	// Default byte limit per window
	ByteLimit int64

	// Time window for rate limiting
	Window time.Duration

	// Function to extract key from request
	KeyFunc func(*http.Request) string

	// Logger for rate limiting operations (optional)
	Logger middleware.Logger
}

// DefaultOptions returns sensible default options
func DefaultOptions(redis *redis.Client) Options {
	return Options{
		Redis:        redis,
		Prefix:       "ratelimit",
		RequestLimit: 60,
		ByteLimit:    1048576, // 1MB
		Window:       time.Minute,
		KeyFunc:      utils.KeyByIP,
	}
}

// Service provides rate limiting functionality
type Service struct {
	limiter    *limiter.DynamicLimiter
	configAPI  *api.ConfigHandler
	middleware func(http.Handler) http.Handler
	options    Options
	logger     middleware.Logger
}

// New creates a new rate limiting service
func New(options Options) *Service {
	if options.Redis == nil {
		panic("Redis client is required")
	}

	if options.KeyFunc == nil {
		options.KeyFunc = utils.KeyByIP
	}

	if options.Prefix == "" {
		options.Prefix = "ratelimit"
	}

	// Create the dynamic limiter
	dynamicLimiter := limiter.NewDynamicLimiter(
		options.Redis,
		options.Prefix,
		options.RequestLimit,
		options.ByteLimit,
		options.Window,
		options.KeyFunc,
	)

	// Create the config API handler
	configHandler := api.NewConfigHandler(dynamicLimiter)

	// Use provided logger or default to no-op
	logger := options.Logger
	if logger == nil {
		logger = middleware.NoOpLogger{}
	}

	// Create the middleware with logger
	middlewareFunc := middleware.DynamicRateLimitMiddlewareWithLogger(dynamicLimiter, logger)

	return &Service{
		limiter:    dynamicLimiter,
		configAPI:  configHandler,
		middleware: middlewareFunc,
		options:    options,
		logger:     logger,
	}
}

// NewFromLimiter creates a new service from an existing limiter
func NewFromLimiter(dynamicLimiter *limiter.DynamicLimiter) *Service {
	// Create the config API handler
	configHandler := api.NewConfigHandler(dynamicLimiter)

	// Create the middleware
	middlewareFunc := middleware.DynamicRateLimitMiddleware(dynamicLimiter)

	return &Service{
		limiter:    dynamicLimiter,
		configAPI:  configHandler,
		middleware: middlewareFunc,
		options:    Options{}, // Empty options since we're using an existing limiter
	}
}

// Middleware returns an http.Handler middleware that applies rate limiting to all paths
func (s *Service) Middleware() func(http.Handler) http.Handler {
	return s.middleware
}

// MiddlewareWithRequestSize returns middleware that can use actual request size instead of response size
func (s *Service) MiddlewareWithRequestSize(requestSizeFunc middleware.RequestSizeFunc) func(http.Handler) http.Handler {
	return middleware.DynamicRateLimitMiddlewareWithRequestSize(s.limiter, s.logger, requestSizeFunc)
}

// SelectiveMiddleware returns a middleware that only applies rate limiting to paths matching the criteria
func (s *Service) SelectiveMiddleware(matcher middleware.PathMatcher) func(http.Handler) http.Handler {
	return middleware.SelectiveRateLimitMiddleware(s.limiter, matcher)
}

// MatchPathPrefix returns a matcher that matches paths with a specific prefix
func MatchPathPrefix(prefix string) middleware.PathMatcher {
	return middleware.MatchPathPrefix(prefix)
}

// MatchPathExact returns a matcher that matches paths exactly
func MatchPathExact(exactPath string) middleware.PathMatcher {
	return middleware.MatchPathExact(exactPath)
}

// MatchPathRegex returns a matcher that matches paths using a regular expression
func MatchPathRegex(pattern string) middleware.PathMatcher {
	return middleware.MatchPathRegex(pattern)
}

// MatchAny returns a matcher that matches if any of the provided matchers match
func MatchAny(matchers ...middleware.PathMatcher) middleware.PathMatcher {
	return middleware.MatchAny(matchers...)
}

// MatchNone returns a matcher that matches if none of the provided matchers match
func MatchNone(matchers ...middleware.PathMatcher) middleware.PathMatcher {
	return middleware.MatchNone(matchers...)
}

// RegisterConfigAPI registers the configuration API endpoints on the provided mux
func (s *Service) RegisterConfigAPI(mux *http.ServeMux) {
	s.configAPI.RegisterRoutes(mux)
}

// GetLimiter returns the underlying dynamic limiter
func (s *Service) GetLimiter() *limiter.DynamicLimiter {
	return s.limiter
}

// SetClientConfig sets rate limit configuration for a specific client
func (s *Service) SetClientConfig(clientID string, requestLimit int, byteLimit int64, windowSecs int) error {
	cfg := config.ClientConfig{
		RequestLimit: requestLimit,
		ByteLimit:    byteLimit,
		WindowSecs:   windowSecs,
	}
	return s.limiter.SetClientConfig(context.TODO(), clientID, cfg)
}

// ResetClientConfig resets a client to default configuration
func (s *Service) ResetClientConfig(clientID string) error {
	return s.limiter.ResetClientConfig(context.TODO(), clientID)
}

// WithKeyFunc returns a key extraction function that can be used in options
func WithKeyFunc(keyFunc func(*http.Request) string) func(*http.Request) string {
	return keyFunc
}

// KeyByIP extracts key based on client IP
func KeyByIP(r *http.Request) string {
	return utils.KeyByIP(r)
}

// KeyByClientID returns a function that extracts key based on client ID
// The client ID should be extracted by the API implementation after authentication
func KeyByClientID(clientID string) func(*http.Request) string {
	return func(r *http.Request) string {
		return utils.KeyByClientID(r, clientID)
	}
}

// KeyByClientIDAndRoute returns a function that extracts key based on client ID and route
// The client ID should be extracted by the API implementation after authentication
func KeyByClientIDAndRoute(clientID string) func(*http.Request) string {
	return func(r *http.Request) string {
		return utils.KeyByClientIDAndRoute(r, clientID)
	}
}

// KeyByHeader returns a key extraction function that uses a specific HTTP header
// This is useful for rate limiting by API key, tenant ID, customer ID, custom fields, etc.
//
// Example:
//
//	// Rate limit by API key
//	options.KeyFunc = ratelimit.KeyByHeader("X-API-Key")
//
//	// Rate limit by custom field header
//	options.KeyFunc = ratelimit.KeyByHeader("X-Custom-Field")
func KeyByHeader(headerName string) func(*http.Request) string {
	return utils.KeyByHeader(headerName)
}

// KeyByHeaderWithOptions returns a key extraction function with advanced options
// This provides fine-grained control over header-based key extraction
//
// Example:
//
//	options.KeyFunc = ratelimit.KeyByHeaderWithOptions(utils.HeaderKeyOptions{
//	    HeaderName: "X-Tenant-ID",
//	    Prefix: "tenant",
//	    DefaultValue: "default-tenant",
//	    Sanitizer: utils.AlphanumericSanitizer,
//	})
func KeyByHeaderWithOptions(opts utils.HeaderKeyOptions) func(*http.Request) string {
	return utils.KeyByHeaderWithOptions(opts)
}

// KeyByMultipleHeaders returns a key extraction function that combines multiple headers
// This is useful when you need composite keys
//
// Example:
//
//	// Rate limit by tenant AND region
//	options.KeyFunc = ratelimit.KeyByMultipleHeaders([]string{"X-Tenant-ID", "X-Region"}, ":")
func KeyByMultipleHeaders(headerNames []string, separator string) func(*http.Request) string {
	return utils.KeyByMultipleHeaders(headerNames, separator)
}

// KeyByHeaderWithFallback returns a key extraction function that tries multiple headers in order
// This is useful when clients might send the same information in different headers
//
// Example:
//
//	// Try X-API-Key first, then Authorization header
//	options.KeyFunc = ratelimit.KeyByHeaderWithFallback([]string{"X-API-Key", "Authorization"})
func KeyByHeaderWithFallback(headerNames []string) func(*http.Request) string {
	return utils.KeyByHeaderWithFallback(headerNames)
}
