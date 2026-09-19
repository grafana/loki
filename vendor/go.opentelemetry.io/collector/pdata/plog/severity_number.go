// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package plog // import "go.opentelemetry.io/collector/pdata/plog"

import (
	"go.opentelemetry.io/collector/pdata/internal"
)

// SeverityNumber represents severity number of a log record.
type SeverityNumber int32

const (
	SeverityNumberUnspecified = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_UNSPECIFIED)
	SeverityNumberTrace       = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_TRACE)
	SeverityNumberTrace2      = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_TRACE2)
	SeverityNumberTrace3      = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_TRACE3)
	SeverityNumberTrace4      = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_TRACE4)
	SeverityNumberDebug       = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_DEBUG)
	SeverityNumberDebug2      = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_DEBUG2)
	SeverityNumberDebug3      = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_DEBUG3)
	SeverityNumberDebug4      = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_DEBUG4)
	SeverityNumberInfo        = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_INFO)
	SeverityNumberInfo2       = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_INFO2)
	SeverityNumberInfo3       = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_INFO3)
	SeverityNumberInfo4       = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_INFO4)
	SeverityNumberWarn        = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_WARN)
	SeverityNumberWarn2       = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_WARN2)
	SeverityNumberWarn3       = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_WARN3)
	SeverityNumberWarn4       = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_WARN4)
	SeverityNumberError       = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_ERROR)
	SeverityNumberError2      = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_ERROR2)
	SeverityNumberError3      = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_ERROR3)
	SeverityNumberError4      = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_ERROR4)
	SeverityNumberFatal       = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_FATAL)
	SeverityNumberFatal2      = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_FATAL2)
	SeverityNumberFatal3      = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_FATAL3)
	SeverityNumberFatal4      = SeverityNumber(internal.SeverityNumber_SEVERITY_NUMBER_FATAL4)
)

// String returns the string representation of the SeverityNumber.
func (sn SeverityNumber) String() string {
	switch sn {
	case SeverityNumberUnspecified:
		return "Unspecified"
	case SeverityNumberTrace:
		return "Trace"
	case SeverityNumberTrace2:
		return "Trace2"
	case SeverityNumberTrace3:
		return "Trace3"
	case SeverityNumberTrace4:
		return "Trace4"
	case SeverityNumberDebug:
		return "Debug"
	case SeverityNumberDebug2:
		return "Debug2"
	case SeverityNumberDebug3:
		return "Debug3"
	case SeverityNumberDebug4:
		return "Debug4"
	case SeverityNumberInfo:
		return "Info"
	case SeverityNumberInfo2:
		return "Info2"
	case SeverityNumberInfo3:
		return "Info3"
	case SeverityNumberInfo4:
		return "Info4"
	case SeverityNumberWarn:
		return "Warn"
	case SeverityNumberWarn2:
		return "Warn2"
	case SeverityNumberWarn3:
		return "Warn3"
	case SeverityNumberWarn4:
		return "Warn4"
	case SeverityNumberError:
		return "Error"
	case SeverityNumberError2:
		return "Error2"
	case SeverityNumberError3:
		return "Error3"
	case SeverityNumberError4:
		return "Error4"
	case SeverityNumberFatal:
		return "Fatal"
	case SeverityNumberFatal2:
		return "Fatal2"
	case SeverityNumberFatal3:
		return "Fatal3"
	case SeverityNumberFatal4:
		return "Fatal4"
	}
	return ""
}
