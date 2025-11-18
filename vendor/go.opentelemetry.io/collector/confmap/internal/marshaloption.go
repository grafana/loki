// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/confmap/internal"

type MarshalOption interface {
	apply(*MarshalOptions)
}

// MarshalOptions is used by (*Conf).Marshal to toggle unmarshaling settings.
// It is in the `internal` package so experimental options can be added in xconfmap.
type MarshalOptions struct{}

type MarshalOptionFunc func(*MarshalOptions)

func (fn MarshalOptionFunc) apply(set *MarshalOptions) {
	fn(set)
}
