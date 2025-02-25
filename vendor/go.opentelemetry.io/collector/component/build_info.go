// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package component // import "go.opentelemetry.io/collector/component"

// BuildInfo is the information that is logged at the application start and
// passed into each component. This information can be overridden in custom build.
type BuildInfo struct {
	// Command is the executable file name, e.g. "otelcol".
	Command string

	// Description is the full name of the collector, e.g. "OpenTelemetry Collector".
	Description string

	// Version string.
	Version string
}

// NewDefaultBuildInfo returns a default BuildInfo.
func NewDefaultBuildInfo() BuildInfo {
	return BuildInfo{
		Command:     "otelcol",
		Description: "OpenTelemetry Collector",
		Version:     "latest",
	}
}
