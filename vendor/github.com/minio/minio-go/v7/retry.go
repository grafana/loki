/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2017 MinIO, Inc.
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

package minio

import (
	"context"
	"crypto/x509"
	"errors"
	"iter"
	"math"
	"net/http"
	"net/url"
	"time"
)

// MaxRetry is the maximum number of retries before stopping.
var MaxRetry = 10

// MaxJitter will randomize over the full exponential backoff time
const MaxJitter = 1.0

// NoJitter disables the use of jitter for randomizing the exponential backoff time
const NoJitter = 0.0

// DefaultRetryUnit - default unit multiplicative per retry.
// defaults to 200 * time.Millisecond
var DefaultRetryUnit = 200 * time.Millisecond

// DefaultRetryCap - Each retry attempt never waits no longer than
// this maximum time duration.
var DefaultRetryCap = time.Second

// newRetryTimer creates a timer with exponentially increasing
// delays until the maximum retry attempts are reached.
func (c *Client) newRetryTimer(ctx context.Context, maxRetry int, baseSleep, maxSleep time.Duration, jitter float64) iter.Seq[int] {
	// computes the exponential backoff duration according to
	// https://www.awsarchitectureblog.com/2015/03/backoff.html
	exponentialBackoffWait := func(attempt int) time.Duration {
		// normalize jitter to the range [0, 1.0]
		if jitter < NoJitter {
			jitter = NoJitter
		}
		if jitter > MaxJitter {
			jitter = MaxJitter
		}

		// sleep = random_between(0, min(maxSleep, base * 2 ** attempt))
		sleep := baseSleep * time.Duration(1<<uint(attempt))
		if sleep > maxSleep {
			sleep = maxSleep
		}
		if math.Abs(jitter-NoJitter) > 1e-9 {
			sleep -= time.Duration(c.random.Float64() * float64(sleep) * jitter)
		}
		return sleep
	}

	return func(yield func(int) bool) {
		// if context is already canceled, skip yield
		select {
		case <-ctx.Done():
			return
		default:
		}

		for i := range maxRetry {
			if !yield(i) {
				return
			}

			select {
			case <-time.After(exponentialBackoffWait(i)):
			case <-ctx.Done():
				return
			}
		}
	}
}

// List of AWS S3 error codes which are retryable.
var retryableS3Codes = map[string]struct{}{
	"RequestError":          {},
	"RequestTimeout":        {},
	"Throttling":            {},
	"ThrottlingException":   {},
	"RequestLimitExceeded":  {},
	"RequestThrottled":      {},
	"InternalError":         {},
	"ExpiredToken":          {},
	"ExpiredTokenException": {},
	"SlowDown":              {},
	// Add more AWS S3 codes here.
}

// isS3CodeRetryable - is s3 error code retryable.
func isS3CodeRetryable(s3Code string) (ok bool) {
	_, ok = retryableS3Codes[s3Code]
	return ok
}

// List of HTTP status codes which are retryable.
var retryableHTTPStatusCodes = map[int]struct{}{
	http.StatusRequestTimeout:      {},
	429:                            {}, // http.StatusTooManyRequests is not part of the Go 1.5 library, yet
	499:                            {}, // client closed request, retry. A non-standard status code introduced by nginx.
	http.StatusInternalServerError: {},
	http.StatusBadGateway:          {},
	http.StatusServiceUnavailable:  {},
	http.StatusGatewayTimeout:      {},
	520:                            {}, // It is used by Cloudflare as a catch-all response for when the origin server sends something unexpected.
	// Add more HTTP status codes here.
}

// isHTTPStatusRetryable - is HTTP error code retryable.
func isHTTPStatusRetryable(httpStatusCode int) (ok bool) {
	_, ok = retryableHTTPStatusCodes[httpStatusCode]
	return ok
}

// For now, all http Do() requests are retriable except some well defined errors
func isRequestErrorRetryable(ctx context.Context, err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// Retry if internal timeout in the HTTP call.
		return ctx.Err() == nil
	}
	if ue, ok := err.(*url.Error); ok {
		e := ue.Unwrap()
		switch e.(type) {
		// x509: certificate signed by unknown authority
		case x509.UnknownAuthorityError:
			return false
		}
		switch e.Error() {
		case "http: server gave HTTP response to HTTPS client":
			return false
		}
	}
	return true
}
