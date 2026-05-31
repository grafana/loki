// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package confmap // import "go.opentelemetry.io/collector/confmap"

import (
	"go.opentelemetry.io/collector/confmap/internal"
)

// Validator defines an optional interface for configuration structs to
// implement to check for validity before Collector startup.
type Validator = internal.Validator

// Validate validates a config struct by recursively checking if it or any of
// its fields implement the Validator interface and performing validation
// on each one that does. A struct failing validation will not cause
// validation to stop; all structs will be validated, and all errors
// will be returned with a single error created from [errors.Join].
func Validate(cfg any) error {
	return internal.Validate(cfg)
}
