// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otlp // import "go.opentelemetry.io/collector/pdata/internal/otlp"

import (
	"go.opentelemetry.io/collector/pdata/internal"
)

// MigrateTraces implements any translation needed due to deprecation in OTLP traces protocol.
// Any ptrace.Unmarshaler implementation from OTLP (proto/json) MUST call this, and the gRPC Server implementation.
func MigrateTraces(rss []*internal.ResourceSpans) {
	for _, rs := range rss {
		if len(rs.ScopeSpans) == 0 {
			rs.ScopeSpans = rs.DeprecatedScopeSpans
		}
		rs.DeprecatedScopeSpans = nil
	}
}
