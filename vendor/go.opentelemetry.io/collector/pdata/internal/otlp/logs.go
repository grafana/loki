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
	otlplogs "go.opentelemetry.io/collector/pdata/internal/data/protogen/logs/v1"
)

// InstrumentationLibraryLogsToScope implements the translation of resource logs data
// following the v0.15.0 upgrade:
//      receivers SHOULD check if instrumentation_library_logs is set
//      and scope_logs is not set then the value in instrumentation_library_logs
//      SHOULD be used instead by converting InstrumentationLibraryLogs into ScopeLogs.
//      If scope_logs is set then instrumentation_library_logs SHOULD be ignored.
// https://github.com/open-telemetry/opentelemetry-proto/blob/3c2915c01a9fb37abfc0415ec71247c4978386b0/opentelemetry/proto/logs/v1/logs.proto#L58
func InstrumentationLibraryLogsToScope(rls []*otlplogs.ResourceLogs) {
	for _, rl := range rls {
		if len(rl.ScopeLogs) == 0 {
			for _, ill := range rl.InstrumentationLibraryLogs {
				scopeLogs := otlplogs.ScopeLogs{
					Scope: otlpcommon.InstrumentationScope{
						Name:    ill.InstrumentationLibrary.Name,
						Version: ill.InstrumentationLibrary.Version,
					},
					LogRecords: ill.LogRecords,
					SchemaUrl:  ill.SchemaUrl,
				}
				rl.ScopeLogs = append(rl.ScopeLogs, &scopeLogs)
			}
		}
		rl.InstrumentationLibraryLogs = nil
	}
}
