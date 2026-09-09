// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

// Equal checks equality with another Link
func (ms Link) Equal(val Link) bool {
	return ms.TraceID() == val.TraceID() &&
		ms.SpanID() == val.SpanID()
}
