// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package exported

import "time"

// ExpectContinueMode is the mode for applying the HTTP "Expect: 100-continue" header to requests with a body.
type ExpectContinueMode int

const (
	// ExpectContinueModeApplyOnThrottle indicates that Expect-Continue will not be applied until
	// specific errors are encountered from the service, at which point it will be applied for a
	// fixed window of time after the last triggering error.
	//
	// Response codes that trigger this behavior are 429, 500, and 503. This is the default behavior.
	ExpectContinueModeApplyOnThrottle ExpectContinueMode = 0

	// ExpectContinueModeOn indicates Expect-Continue will be applied regardless of recent error status.
	// The ContentLengthThreshold option still applies.
	ExpectContinueModeOn ExpectContinueMode = 1

	// ExpectContinueModeOff indicates Expect-Continue will never be applied.
	ExpectContinueModeOff ExpectContinueMode = 2
)

// ExpectContinueOptions configures the behavior for applying the HTTP "Expect: 100-continue"
// header to operations that include a request body.
type ExpectContinueOptions struct {
	// Mode controls when the Expect: 100-continue header is applied. The default value
	// (ExpectContinueModeApplyOnThrottle) applies the header for a fixed time window after
	// the service responds with a throttle/server-error status (429, 500, or 503).
	Mode ExpectContinueMode

	// ContentLengthThreshold is the minimum value of HTTP request Content-Length for applying
	// the Expect: 100-continue header. The default is zero, meaning any request that has a
	// non-empty body and a computable content length is eligible.
	ContentLengthThreshold int64

	// ThrottleInterval is used in ExpectContinueModeApplyOnThrottle mode to set the time window
	// during which the Expect: 100-continue header is applied after a triggering error response
	// (429, 500, or 503) is observed. A zero value uses the default of one minute.
	ThrottleInterval time.Duration
}
