// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package xconfmap // import "go.opentelemetry.io/collector/confmap/xconfmap"

import (
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/internal"
)

// Validator defines an optional interface for configurations to implement to do validation.
//
// Deprecated [v0.152.0]: Use `confmap.Validator“ instead.
type Validator = internal.Validator

// Validate validates a config, by doing this:
//   - Call Validate on the config itself if the config implements ConfigValidator.
//
// Deprecated [v0.152.0]: Use `confmap.Validate“ instead.
func Validate(cfg any) error {
	return internal.Validate(cfg)
}

// WithForceUnmarshaler sets an option to run a top-level Unmarshal method,
// even if the Conf being unmarshaled is already a parameter from an Unmarshal method.
// To avoid infinite recursion, this should only be used when unmarshaling into
// a different type from the current Unmarshaler.
// For instance, this should be used in wrapper types such as configoptional.Optional
// to ensure the inner type's Unmarshal method is called.
func WithForceUnmarshaler() confmap.UnmarshalOption {
	return internal.WithForceUnmarshaler()
}
