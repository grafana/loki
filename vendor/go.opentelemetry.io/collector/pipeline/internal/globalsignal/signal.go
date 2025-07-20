// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package globalsignal // import "go.opentelemetry.io/collector/pipeline/internal/globalsignal"

import (
	"encoding"
	"fmt"
)

var (
	SignalProfiles = Signal{name: "profiles"}
	SignalTraces   = Signal{name: "traces"}
	SignalMetrics  = Signal{name: "metrics"}
	SignalLogs     = Signal{name: "logs"}

	_ encoding.TextMarshaler   = (*Signal)(nil)
	_ encoding.TextUnmarshaler = (*Signal)(nil)
)

// Signal represents the signals supported by the collector.
type Signal struct {
	name string
}

// String returns the string representation of the signal.
func (s Signal) String() string {
	return s.name
}

// MarshalText marshals the Signal.
func (s Signal) MarshalText() ([]byte, error) {
	return []byte(s.name), nil
}

// UnmarshalText marshals the Signal.
func (s *Signal) UnmarshalText(text []byte) error {
	switch string(text) {
	case SignalProfiles.name:
		*s = SignalProfiles
	case SignalTraces.name:
		*s = SignalTraces
	case SignalMetrics.name:
		*s = SignalMetrics
	case SignalLogs.name:
		*s = SignalLogs
	default:
		return fmt.Errorf("unknown pipeline signal: %q", string(text))
	}
	return nil
}
