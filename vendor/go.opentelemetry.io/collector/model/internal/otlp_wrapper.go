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

package internal // import "go.opentelemetry.io/collector/model/internal"

import (
	otlplogs "go.opentelemetry.io/collector/model/internal/data/protogen/logs/v1"
	otlpmetrics "go.opentelemetry.io/collector/model/internal/data/protogen/metrics/v1"
	otlptrace "go.opentelemetry.io/collector/model/internal/data/protogen/trace/v1"
)

// MetricsWrapper is an intermediary struct that is declared in an internal package
// as a way to prevent certain functions of pdata.Metrics data type to be callable by
// any code outside of this module.
type MetricsWrapper struct {
	req *otlpmetrics.MetricsData
}

// MetricsToOtlp internal helper to convert MetricsWrapper to protobuf representation.
func MetricsToOtlp(mw MetricsWrapper) *otlpmetrics.MetricsData {
	return mw.req
}

// MetricsFromOtlp internal helper to convert protobuf representation to MetricsWrapper.
func MetricsFromOtlp(req *otlpmetrics.MetricsData) MetricsWrapper {
	return MetricsWrapper{req: req}
}

// TracesWrapper is an intermediary struct that is declared in an internal package
// as a way to prevent certain functions of pdata.Traces data type to be callable by
// any code outside of this module.
type TracesWrapper struct {
	req *otlptrace.TracesData
}

// TracesToOtlp internal helper to convert TracesWrapper to protobuf representation.
func TracesToOtlp(mw TracesWrapper) *otlptrace.TracesData {
	return mw.req
}

// TracesFromOtlp internal helper to convert protobuf representation to TracesWrapper.
func TracesFromOtlp(req *otlptrace.TracesData) TracesWrapper {
	return TracesWrapper{req: req}
}

// LogsWrapper is an intermediary struct that is declared in an internal package
// as a way to prevent certain functions of pdata.Logs data type to be callable by
// any code outside of this module.
type LogsWrapper struct {
	req *otlplogs.LogsData
}

// LogsToOtlp internal helper to convert LogsWrapper to protobuf representation.
func LogsToOtlp(l LogsWrapper) *otlplogs.LogsData {
	return l.req
}

// LogsFromOtlp internal helper to convert protobuf representation to LogsWrapper.
func LogsFromOtlp(req *otlplogs.LogsData) LogsWrapper {
	return LogsWrapper{req: req}
}
