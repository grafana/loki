// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package base

import (
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/exported"
)

const (
	// expectContinueHeader is the name of the HTTP "Expect" header.
	expectContinueHeader = "Expect"

	// expectContinueHeaderValue is the value used when applying the "Expect: 100-continue" header.
	expectContinueHeaderValue = "100-continue"

	// DisableExpectContinueHeaderEnvVar is the name of the environment variable used to disable
	// the automatic application of the "Expect: 100-continue" header. When the value parses to
	// true (e.g. "1", "true", "TRUE"), no Expect: 100-continue policy will be added to the pipeline.
	DisableExpectContinueHeaderEnvVar = "AZURE_STORAGE_DISABLE_EXPECT_CONTINUE_HEADER"

	// defaultThrottleInterval is the time window during which Expect: 100-continue will be applied
	// after a triggering response status code (429, 500, or 503) has been observed.
	defaultThrottleInterval = time.Minute
)

// envCheckExpectContinueDisabled lazily reads DisableExpectContinueHeaderEnvVar exactly once
// per process and caches the result. Reassigning this variable (see newExpectContinueEnvCheck)
// resets the cache; this is used by tests that mutate the environment.
var envCheckExpectContinueDisabled = newExpectContinueEnvCheck()

// newExpectContinueEnvCheck returns a memoized check for the disable env variable. The
// returned function reads os.Environ on first invocation and reuses the cached result on
// subsequent invocations.
func newExpectContinueEnvCheck() func() bool {
	return sync.OnceValue(func() bool {
		v, ok := os.LookupEnv(DisableExpectContinueHeaderEnvVar)
		if !ok {
			return false
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			return false
		}
		return b
	})
}

// ResetExpectContinueEnvCacheForTest replaces the cached env-var lookup with a fresh
// sync.OnceValue so the next call to NewExpectContinuePolicy re-reads the current environment.
// It returns a restore function that swaps the previous cache back; callers should pass the
// restore function to t.Cleanup.
//
// This helper is intended for tests within the azblob module only.
func ResetExpectContinueEnvCacheForTest() (restore func()) {
	prev := envCheckExpectContinueDisabled
	envCheckExpectContinueDisabled = newExpectContinueEnvCheck()
	return func() { envCheckExpectContinueDisabled = prev }
}

// NewExpectContinuePolicy returns a per-retry policy that applies the "Expect: 100-continue" HTTP
// header to outgoing requests according to the provided options.
//
// The zero value of opts selects ExpectContinueModeApplyOnThrottle with a one-minute window.
//
// When the environment variable AZURE_STORAGE_DISABLE_EXPECT_CONTINUE_HEADER is set to a truthy
// value, the returned policy is nil regardless of the supplied options.
//
// Returns nil when no policy should be added to the pipeline (mode is Off or env disables it).
func NewExpectContinuePolicy(opts exported.ExpectContinueOptions) policy.Policy {
	if envCheckExpectContinueDisabled() {
		return nil
	}
	switch opts.Mode {
	case exported.ExpectContinueModeOff:
		return nil
	case exported.ExpectContinueModeOn:
		return &expectContinuePolicy{contentLengthThreshold: opts.ContentLengthThreshold}
	case exported.ExpectContinueModeApplyOnThrottle:
		fallthrough
	default:
		interval := opts.ThrottleInterval
		if interval == 0 {
			interval = defaultThrottleInterval
		}
		return &expectContinueOnThrottlePolicy{
			contentLengthThreshold: opts.ContentLengthThreshold,
			throttleInterval:       interval,
		}
	}
}

// expectContinuePolicy unconditionally sets the "Expect: 100-continue" header on requests whose
// body length is at least contentLengthThreshold.
type expectContinuePolicy struct {
	contentLengthThreshold int64
}

func (p *expectContinuePolicy) Do(req *policy.Request) (*http.Response, error) {
	if shouldApplyExpectContinue(req, p.contentLengthThreshold) {
		req.Raw().Header.Set(expectContinueHeader, expectContinueHeaderValue)
	}
	return req.Next()
}

// expectContinueOnThrottlePolicy sets the "Expect: 100-continue" header on requests for a fixed
// time window after the service responds with status code 429, 500, or 503.
type expectContinueOnThrottlePolicy struct {
	contentLengthThreshold int64
	throttleInterval       time.Duration
	// lastThrottle stores the time of the most recent triggering response.
	lastThrottle atomic.Pointer[time.Time]
}

func (p *expectContinueOnThrottlePolicy) Do(req *policy.Request) (*http.Response, error) {
	if shouldApplyExpectContinue(req, p.contentLengthThreshold) {
		last := p.lastThrottle.Load()
		if last != nil && time.Since(*last) < p.throttleInterval {
			req.Raw().Header.Set(expectContinueHeader, expectContinueHeaderValue)
		} else {
			req.Raw().Header.Del(expectContinueHeader)
		}
	}
	resp, err := req.Next()
	if resp != nil {
		switch resp.StatusCode {
		case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable:
			now := time.Now()
			p.lastThrottle.Store(&now)
		}
	}
	return resp, err
}

// shouldApplyExpectContinue reports whether the request is eligible for the "Expect: 100-continue"
// header: it must have a non-nil body, and the request's known Content-Length must be at least
// contentLengthThreshold. Requests with an unknown content length (chunked) are not eligible.
func shouldApplyExpectContinue(req *policy.Request, contentLengthThreshold int64) bool {
	if req == nil || req.Body() == nil {
		return false
	}
	raw := req.Raw()
	if raw == nil {
		return false
	}
	if raw.ContentLength <= 0 {
		return false
	}
	return raw.ContentLength >= contentLengthThreshold
}
