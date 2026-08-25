// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ptrace // import "go.opentelemetry.io/collector/pdata/ptrace"

import (
	"go.opentelemetry.io/collector/pdata/internal"
)

// StatusCode mirrors the codes defined at
// https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/trace/api.md#set-status
type StatusCode int32

const (
	StatusCodeUnset = StatusCode(internal.StatusCode_STATUS_CODE_UNSET)
	StatusCodeOk    = StatusCode(internal.StatusCode_STATUS_CODE_OK)
	StatusCodeError = StatusCode(internal.StatusCode_STATUS_CODE_ERROR)
)

// String returns the string representation of the StatusCode.
func (sc StatusCode) String() string {
	switch sc {
	case StatusCodeUnset:
		return "Unset"
	case StatusCodeOk:
		return "Ok"
	case StatusCodeError:
		return "Error"
	}
	return ""
}
