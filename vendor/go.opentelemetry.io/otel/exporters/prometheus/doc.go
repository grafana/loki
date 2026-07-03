// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package prometheus provides a Prometheus Exporter that converts
// OTLP metrics into the Prometheus exposition format and implements
// prometheus.Collector to provide a handler for these metrics.
//
// The Prometheus exporter ignores metrics from the Prometheus bridge. To
// export these metrics, simply register them directly with the Prometheus
// Handler.
package prometheus // import "go.opentelemetry.io/otel/exporters/prometheus"
