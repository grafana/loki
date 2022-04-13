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

package pdata // import "go.opentelemetry.io/collector/model/internal/pdata"

import (
	otlplogs "go.opentelemetry.io/collector/model/internal/data/protogen/logs/v1"
	otlpmetrics "go.opentelemetry.io/collector/model/internal/data/protogen/metrics/v1"
	otlptrace "go.opentelemetry.io/collector/model/internal/data/protogen/trace/v1"
)

// MetricsToOtlp internal helper to convert Metrics to protobuf representation.
func MetricsToOtlp(mw Metrics) *otlpmetrics.MetricsData {
	return mw.orig
}

// MetricsFromOtlp internal helper to convert protobuf representation to Metrics.
func MetricsFromOtlp(orig *otlpmetrics.MetricsData) Metrics {
	return Metrics{orig: orig}
}

// TracesToOtlp internal helper to convert Traces to protobuf representation.
func TracesToOtlp(mw Traces) *otlptrace.TracesData {
	return mw.orig
}

// TracesFromOtlp internal helper to convert protobuf representation to Traces.
func TracesFromOtlp(orig *otlptrace.TracesData) Traces {
	return Traces{orig: orig}
}

// LogsToOtlp internal helper to convert Logs to protobuf representation.
func LogsToOtlp(l Logs) *otlplogs.LogsData {
	return l.orig
}

// LogsFromOtlp internal helper to convert protobuf representation to Logs.
func LogsFromOtlp(orig *otlplogs.LogsData) Logs {
	return Logs{orig: orig}
}
