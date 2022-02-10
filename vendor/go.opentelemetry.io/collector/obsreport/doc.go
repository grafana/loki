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

// Package obsreport provides unified and consistent observability signals (
// metrics, tracing, etc) for components of the OpenTelemetry collector.
//
// The function Configure is used to control which signals are going to be
// generated. It provides functions for the typical operations of receivers,
// processors, and exporters.
//
// Receivers should use the respective start and end according to the data type
// being received, ie.:
//
// 	* Traces receive operations should use the pair:
// 		StartTracesOp/EndTracesOp
//
// 	* Metrics receive operations should use the pair:
// 		StartMetricsOp/EndMetricsOp
//
// 	* Logs receive operations should use the pair:
// 		StartLogsOp/EndLogsOp
//
// Similar for exporters:
//
// 	* Traces export operations should use the pair:
// 		StartTracesOp/EndTracesOp
//
// 	* Metrics export operations should use the pair:
// 		StartMetricsOp/EndMetricsOp
//
// 	* Metrics export operations should use the pair:
// 		StartLogsOp/EndLogsOp
//
// The package is capable of generating legacy metrics by using the
// observability package allowing a controlled transition from legacy to the
// new metrics. The goal is to eventually remove the legacy metrics and use only
// the new metrics.
//
// The main differences regarding the legacy metrics are:
//
// 1. "Amount of metric data" is measured as metric points (ie.: a single value
// in time), contrast it with number of time series used legacy. Number of
// metric data points is a more general concept regarding various metric
// formats.
//
// 2. Exporters measure the number of items, ie.: number of spans or metric
// points, that were sent and the ones for which the attempt to send failed.
// For more information about this see Notes below about reporting data loss.
//
// 3. All measurements of "amount of data" used in the new metrics for receivers
// and exporters should reflect their native formats, not the internal format
// used in the Collector. This is to facilitate reconciliation between Collector,
// client and backend. For instance: certain metric formats do not provide
// direct support for histograms and have predefined conventions to represent
// those, this conversion may end with a different number of time series and
// data points than the internal Collector format.
//
// Notes:
//
// * Data loss should be recorded only when the component itself remove the data
// from the pipeline. Legacy metrics for receivers used "dropped" in their names
// but these could be non-zero under normal operations and reflected no actual
// data loss when exporters with "sending_queue" are used. New metrics were renamed
// to avoid this misunderstanding. Here are the general recommendations to report data loss:
//
// 		* Receivers reporting errors to clients typically result in the client
//		  re-sending the same data so it is more correct to report "receive errors",
//		  not actual data loss.
//
//		* Exporters need to report individual failures to send data, but on
//		  typical production pipelines processors usually take care of retries,
//		  so these should be reported as "send errors".
//
//		* Data "filtered out" should have its own metrics and not be confused
//		  with dropped data.
//
// Naming Convention for New Metrics
//
// Common Metrics:
// Metrics shared by different components should follow the convention below:
//
// `<component>/<metric_name>`
//
// As a label the metric should have at least `{<component>="<name>"}` where
// `<name>` is the name used in the configuration for the instance of the
// component, eg.:
//
// `receiver/accepted_spans{receiver="otlp",...}`
// `exporter/sent_spans{exporter="otlp/prod",...}`
//
// Component Specific Metrics:
// These metrics are implemented by specific components, eg.: batch processor.
// The general pattern is the same as the common metrics but with the addition
// of the component type (as it appears in the configuration) before the actual
// metric:
//
// `<component>/<type>/<metric_name>`
//
// Even metrics exclusive to a single type should follow the conventions above
// and also include the type (as written in the configuration) as part of the
// metric name since there could be multiple instances of the same type in
// different pipelines, eg.:
//
// `processor/batch/batch_size_trigger_send{processor="batch/dev",...}`
//
package obsreport // import "go.opentelemetry.io/collector/obsreport"
