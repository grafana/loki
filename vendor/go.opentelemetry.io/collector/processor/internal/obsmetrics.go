// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/processor/internal"

const (
	MetricNameSep = "_"

	// ProcessorKey is the key used to identify processors in metrics and traces.
	ProcessorKey = "processor"

	ProcessorMetricPrefix = ProcessorKey + MetricNameSep
)
