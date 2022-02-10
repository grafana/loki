// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package component // import "go.opentelemetry.io/collector/component"

import (
	"context"

	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/consumer"
)

// Exporter exports telemetry data from the collector to a destination.
type Exporter interface {
	Component
}

// TracesExporter is an Exporter that can consume traces.
type TracesExporter interface {
	Exporter
	consumer.Traces
}

// MetricsExporter is an Exporter that can consume metrics.
type MetricsExporter interface {
	Exporter
	consumer.Metrics
}

// LogsExporter is an Exporter that can consume logs.
type LogsExporter interface {
	Exporter
	consumer.Logs
}

// ExporterCreateSettings configures Exporter creators.
type ExporterCreateSettings struct {
	TelemetrySettings

	// BuildInfo can be used by components for informational purposes
	BuildInfo BuildInfo
}

// ExporterFactory can create MetricsExporter, TracesExporter and
// LogsExporter. This is the new preferred factory type to create exporters.
//
// This interface cannot be directly implemented. Implementations must
// use the exporterhelper.NewFactory to implement it.
type ExporterFactory interface {
	Factory

	// CreateDefaultConfig creates the default configuration for the Exporter.
	// This method can be called multiple times depending on the pipeline
	// configuration and should not cause side-effects that prevent the creation
	// of multiple instances of the Exporter.
	// The object returned by this method needs to pass the checks implemented by
	// 'configtest.CheckConfigStruct'. It is recommended to have these checks in the
	// tests of any implementation of the Factory interface.
	CreateDefaultConfig() config.Exporter

	// CreateTracesExporter creates a trace exporter based on this config.
	// If the exporter type does not support tracing or if the config is not valid,
	// an error will be returned instead.
	CreateTracesExporter(ctx context.Context, set ExporterCreateSettings,
		cfg config.Exporter) (TracesExporter, error)

	// CreateMetricsExporter creates a metrics exporter based on this config.
	// If the exporter type does not support metrics or if the config is not valid,
	// an error will be returned instead.
	CreateMetricsExporter(ctx context.Context, set ExporterCreateSettings,
		cfg config.Exporter) (MetricsExporter, error)

	// CreateLogsExporter creates an exporter based on the config.
	// If the exporter type does not support logs or if the config is not valid,
	// an error will be returned instead.
	CreateLogsExporter(ctx context.Context, set ExporterCreateSettings,
		cfg config.Exporter) (LogsExporter, error)
}
