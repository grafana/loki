// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package xconfmap // import "go.opentelemetry.io/collector/confmap/xconfmap"

import (
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/internal"
)

// WithForceUnmarshaler sets an option to run a top-level Unmarshal method,
// even if the Conf being unmarshaled is already a parameter from an Unmarshal method.
// To avoid infinite recursion, this should only be used when unmarshaling into
// a different type from the current Unmarshaler.
// For instance, this should be used in wrapper types such as configoptional.Optional
// to ensure the inner type's Unmarshal method is called.
//
// Deprecated [v0.157.0]: Use [confmap.WithForceUnmarshaler] instead.
func WithForceUnmarshaler() confmap.UnmarshalOption {
	return internal.WithForceUnmarshaler()
}
