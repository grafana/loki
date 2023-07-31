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
	otlpmetrics "go.opentelemetry.io/collector/pdata/internal/data/protogen/metrics/v1"
)

// MigrateMetrics implements any translation needed due to deprecation in OTLP metrics protocol.
// Any pmetric.Unmarshaler implementation from OTLP (proto/json) MUST call this, and the gRPC Server implementation.
func MigrateMetrics(rms []*otlpmetrics.ResourceMetrics) {
	for _, rm := range rms {
		if len(rm.ScopeMetrics) == 0 {
			rm.ScopeMetrics = rm.DeprecatedScopeMetrics
		}
		rm.DeprecatedScopeMetrics = nil
	}
}
