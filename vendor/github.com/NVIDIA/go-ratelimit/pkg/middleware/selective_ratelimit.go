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
	"net/http"
	"regexp"
	"strings"

	"github.com/NVIDIA/go-ratelimit/pkg/limiter"
)

// PathMatcher defines a function to determine if a path should be rate limited
type PathMatcher func(path string) bool

// SelectiveRateLimitMiddleware applies rate limiting only to paths that match specific criteria
func SelectiveRateLimitMiddleware(dl *limiter.DynamicLimiter, matcher PathMatcher) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if this path should be rate limited
			if !matcher(r.URL.Path) {
				// Skip rate limiting for this path
				next.ServeHTTP(w, r)
				return
			}

			// Apply normal rate limiting
			dynamicHandler := DynamicRateLimitMiddleware(dl)(next)
			dynamicHandler.ServeHTTP(w, r)
		})
	}
}

// MatchPathPrefix returns a matcher that matches paths with a specific prefix
func MatchPathPrefix(prefix string) PathMatcher {
	return func(path string) bool {
		return strings.HasPrefix(path, prefix)
	}
}

// MatchPathExact returns a matcher that matches paths exactly
func MatchPathExact(exactPath string) PathMatcher {
	return func(path string) bool {
		return path == exactPath
	}
}

// MatchPathRegex returns a matcher that matches paths using a regular expression
func MatchPathRegex(pattern string) PathMatcher {
	regex := regexp.MustCompile(pattern)
	return func(path string) bool {
		return regex.MatchString(path)
	}
}

// MatchAny returns a matcher that matches if any of the provided matchers match
func MatchAny(matchers ...PathMatcher) PathMatcher {
	return func(path string) bool {
		for _, m := range matchers {
			if m(path) {
				return true
			}
		}
		return false
	}
}

// MatchNone returns a matcher that matches if none of the provided matchers match
func MatchNone(matchers ...PathMatcher) PathMatcher {
	return func(path string) bool {
		for _, m := range matchers {
			if m(path) {
				return false
			}
		}
		return true
	}
}
