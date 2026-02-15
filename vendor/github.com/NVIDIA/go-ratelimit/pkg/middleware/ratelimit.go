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

// KeyBuilderFunc defines a function type for generating rate limit keys from HTTP requests
type KeyBuilderFunc func(*http.Request) string

// RateLimitMiddleware enforces a single global policy (limit/window) using Redis.
func RateLimitMiddleware(rl *limiter.RedisLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, remaining, reset, err := rl.Allow(r.Context(), r)
			if err != nil {
				// Fail-safe: treat errors as 500 (or allow, if you prefer soft-fail)
				http.Error(w, "rate limiter error", http.StatusInternalServerError)
				return
			}

			// RFC RateLimit fields (standardized response headers)
			// RateLimit-Limit: max requests allowed in the window
			// RateLimit-Remaining: remaining requests in the current window
			// RateLimit-Reset: seconds until the quota resets
			w.Header().Set("RateLimit-Limit", fmt.Sprintf("%d", rl.GetLimit()))
			w.Header().Set("RateLimit-Remaining", fmt.Sprintf("%d", remaining))
			w.Header().Set("RateLimit-Reset", fmt.Sprintf("%d", reset))

			if !allowed {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", reset))
				http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
