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
	otlptrace "go.opentelemetry.io/collector/pdata/internal/data/protogen/trace/v1"
)

// InstrumentationLibraryToScope implements the translation of resource span data
// following the v0.15.0 upgrade:
//      receivers SHOULD check if instrumentation_library_spans is set
//      and scope_spans is not set then the value in instrumentation_library_spans
//      SHOULD be used instead by converting InstrumentationLibrarySpans into ScopeSpans.
//      If scope_spans is set then instrumentation_library_spans SHOULD be ignored.
// https://github.com/open-telemetry/opentelemetry-proto/blob/3c2915c01a9fb37abfc0415ec71247c4978386b0/opentelemetry/proto/trace/v1/trace.proto#L58
func InstrumentationLibrarySpansToScope(rss []*otlptrace.ResourceSpans) {
	for _, rs := range rss {
		if len(rs.ScopeSpans) == 0 {
			for _, ils := range rs.InstrumentationLibrarySpans {
				scopeSpans := otlptrace.ScopeSpans{
					Scope: otlpcommon.InstrumentationScope{
						Name:    ils.InstrumentationLibrary.Name,
						Version: ils.InstrumentationLibrary.Version,
					},
					Spans:     ils.Spans,
					SchemaUrl: ils.SchemaUrl,
				}
				rs.ScopeSpans = append(rs.ScopeSpans, &scopeSpans)
			}
		}
		rs.InstrumentationLibrarySpans = nil
	}
}
