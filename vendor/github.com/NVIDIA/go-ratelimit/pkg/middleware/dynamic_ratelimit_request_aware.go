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
package middleware

import (
	"fmt"
	"net/http"

	"github.com/NVIDIA/go-ratelimit/pkg/limiter"
)

// DynamicRateLimitMiddlewareWithRequestSize creates middleware that can use actual request size
func DynamicRateLimitMiddlewareWithRequestSize(dl limiter.RateLimiter, log Logger, requestSizeFunc RequestSizeFunc) func(http.Handler) http.Handler {
	// Use a no-op logger if none provided
	if log == nil {
		log = NoOpLogger{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get the actual rate limit key that was used
			rateLimitKey := dl.GetLastKey(r)

			// Get client ID for retrieving config
			clientID := extractClientID(r)
			cfg, _ := dl.GetClientConfig(r.Context(), clientID)

			// First check if the request is allowed based on request count
			allowed, remaining, reset, err := dl.AllowRequest(r.Context(), r)
			if err != nil {
				http.Error(w, "rate limiter error", http.StatusInternalServerError)
				return
			}

			// Set request count rate limit headers - use the actual rate limit key for logging
			log.Info(fmt.Sprintf("Setting rate limit headers for client %s (limit=%d, allowed=%v, remaining=%d)", rateLimitKey, cfg.RequestLimit, allowed, remaining))
			if cfg.RequestLimit < 0 {
				// Request limiting is disabled
				w.Header().Set("RateLimit-Request-Limit", "unlimited")
				w.Header().Set("RateLimit-Request-Remaining", "unlimited")
			} else {
				w.Header().Set("RateLimit-Request-Limit", fmt.Sprintf("%d", cfg.RequestLimit))
				w.Header().Set("RateLimit-Request-Remaining", fmt.Sprintf("%d", remaining))
			}
			w.Header().Set("RateLimit-Request-Reset", fmt.Sprintf("%d", reset))

			// Get current byte usage to calculate bytes remaining
			currentUsage, _ := dl.GetCurrentUsage(r.Context(), r)
			bytesRemaining := cfg.ByteLimit - currentUsage
			if bytesRemaining < 0 {
				bytesRemaining = 0
			}

			// Also set byte rate limit headers (these are always active)
			w.Header().Set("RateLimit-Bytes-Limit", fmt.Sprintf("%d", cfg.ByteLimit))
			w.Header().Set("RateLimit-Bytes-Remaining", fmt.Sprintf("%d", bytesRemaining))
			w.Header().Set("RateLimit-Bytes-Window", fmt.Sprintf("%d", cfg.WindowSecs))

			if !allowed {
				log.Info(fmt.Sprintf("Request rate limit exceeded for client %s", rateLimitKey))
				w.Header().Set("Retry-After", fmt.Sprintf("%d", reset))
				http.Error(w, "request rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			// Pre-flight byte check: Check if we have a request size and if it would exceed the limit
			if requestSizeFunc != nil {
				if expectedSize, ok := requestSizeFunc(r); ok && expectedSize > 0 {
					// Check if this request would exceed the byte limit
					if cfg.ByteLimit > 0 && bytesRemaining < expectedSize {
						mbExpected := float64(expectedSize) / (1024 * 1024)
						mbRemaining := float64(bytesRemaining) / (1024 * 1024)
						mbLimit := float64(cfg.ByteLimit) / (1024 * 1024)
						
						log.Error(fmt.Sprintf("Byte rate limit would be exceeded for client %s: %.3f MB requested, %.3f MB remaining of %.3f MB/s limit", 
							rateLimitKey, mbExpected, mbRemaining, mbLimit))
						
						// Calculate when they can retry
						w.Header().Set("Retry-After", fmt.Sprintf("%d", reset))
						http.Error(w, "byte rate limit exceeded", http.StatusTooManyRequests)
						return
					}
				}
			}

			// Create a request-aware byte counter wrapper
			bcw := NewRequestAwareByteCounterWriter(w, r, requestSizeFunc)

			// Call the next handler with our wrapped response writer
			next.ServeHTTP(bcw, r)

			// Only count successful responses
			if bcw.StatusCode < 400 {
				// Record bytes and check if byte limit would be exceeded
				allowed, remaining, reset, err := dl.RecordBytes(r.Context(), r, bcw.BytesWritten)

				// Log with actual remaining bytes from rate limiter
				mbUsed := float64(bcw.BytesWritten) / (1024 * 1024)
				mbRemaining := float64(remaining) / (1024 * 1024)
				mbLimit := float64(cfg.ByteLimit) / (1024 * 1024)

				log.Info(fmt.Sprintf("Recording bytes for client %s: %.3f MB used, %.3f MB remaining of %.3f MB/s limit",
					rateLimitKey, mbUsed, mbRemaining, mbLimit))

				// Set byte rate limit headers
				w.Header().Set("RateLimit-Bytes-Limit", fmt.Sprintf("%d", cfg.ByteLimit))
				w.Header().Set("RateLimit-Bytes-Remaining", fmt.Sprintf("%d", remaining))
				w.Header().Set("RateLimit-Bytes-Reset", fmt.Sprintf("%d", reset))

				if !allowed && err == nil {
					log.Error(fmt.Sprintf("Byte rate limit exceeded for client %s", rateLimitKey))
					// Note: Response is already sent at this point, so we can't return 429
					// This is logged for monitoring purposes
				}
			}
		})
	}
}
