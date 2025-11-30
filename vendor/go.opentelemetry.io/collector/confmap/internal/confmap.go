// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/confmap/internal"

// Unmarshaler interface may be implemented by types to customize their behavior when being unmarshaled from a Conf.
// Only types with struct or pointer to struct kind are supported.
type Unmarshaler interface {
	// Unmarshal a Conf into the struct in a custom way.
	// The Conf for this specific component may be nil or empty if no config available.
	// This method should only be called by decoding hooks when calling Conf.Unmarshal.
	Unmarshal(component *Conf) error
}

// Marshaler defines an optional interface for custom configuration marshaling.
// A configuration struct can implement this interface to override the default
// marshaling.
type Marshaler interface {
	// Marshal the config into a Conf in a custom way.
	// The Conf will be empty and can be merged into.
	Marshal(component *Conf) error
}
