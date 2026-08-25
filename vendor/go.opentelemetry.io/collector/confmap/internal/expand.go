// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/confmap/internal"

// ExpandedValue holds the YAML parsed value and original representation of a value.
// It keeps track of the original representation to be used by the 'useExpandValue' hook
// if the target field is a string. We need to keep both representations because we don't know
// what the target field type is until `Unmarshal` is called.
type ExpandedValue struct {
	// Value is the expanded value.
	Value any
	// Original is the original representation of the value.
	Original string
}
