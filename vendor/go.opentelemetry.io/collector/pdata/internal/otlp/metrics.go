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

package otlp // import "go.opentelemetry.io/collector/pdata/internal/otlp"

import (
	otlpcommon "go.opentelemetry.io/collector/pdata/internal/data/protogen/common/v1"
	otlpmetrics "go.opentelemetry.io/collector/pdata/internal/data/protogen/metrics/v1"
)

// InstrumentationLibraryMetricsToScope implements the translation of resource metrics data
// following the v0.15.0 upgrade:
//      receivers SHOULD check if instrumentation_library_metrics is set
//      and scope_metrics is not set then the value in instrumentation_library_metrics
//      SHOULD be used instead by converting InstrumentationLibraryMetrics into ScopeMetrics.
//      If scope_metrics is set then instrumentation_library_metrics SHOULD be ignored.
// https://github.com/open-telemetry/opentelemetry-proto/blob/3c2915c01a9fb37abfc0415ec71247c4978386b0/opentelemetry/proto/metrics/v1/metrics.proto#L58
func InstrumentationLibraryMetricsToScope(rms []*otlpmetrics.ResourceMetrics) {
	for _, rm := range rms {
		if len(rm.ScopeMetrics) == 0 {
			for _, ilm := range rm.InstrumentationLibraryMetrics {
				scopeMetrics := otlpmetrics.ScopeMetrics{
					Scope: otlpcommon.InstrumentationScope{
						Name:    ilm.InstrumentationLibrary.Name,
						Version: ilm.InstrumentationLibrary.Version,
					},
					Metrics:   ilm.Metrics,
					SchemaUrl: ilm.SchemaUrl,
				}
				rm.ScopeMetrics = append(rm.ScopeMetrics, &scopeMetrics)
			}
		}
		rm.InstrumentationLibraryMetrics = nil
	}
}
