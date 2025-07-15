// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package component // import "go.opentelemetry.io/collector/component"

// Config defines the configuration for a component.Component.
//
// Implementations and/or any sub-configs (other types embedded or included in the Config implementation)
// MUST implement xconfmap.Validator if any validation is required for that part of the configuration
// (e.g. check if a required field is present).
//
// A valid implementation MUST pass the check componenttest.CheckConfigStruct (return nil error).
type Config any
