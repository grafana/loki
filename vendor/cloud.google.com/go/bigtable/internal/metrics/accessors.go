// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package internal

import "go.opentelemetry.io/otel/attribute"

// ClientAttributes returns the per-client OTel attribute set (project,
// instance, app_profile, client_uid, client_name) that the factory
// stamps on every recorded metric. Read-only accessor exposed so the
// top-level bigtable-package tests can assert on client identity
// without reflecting into unexported fields.
func (f *Factory) ClientAttributes() []attribute.KeyValue {
	return f.clientAttributes
}

// AdditionalAttrs exposes the per-metric additional label list. Read-only.
func (m metricInfo) AdditionalAttrs() []string {
	return m.additionalAttrs
}

// ToOtelMetricAttrs is the exported form of toOtelMetricAttrs, offered
// so external tests can drive the same attribute-derivation path the
// record* methods take internally.
func (mt *Tracer) ToOtelMetricAttrs(metricName string) (attribute.Set, error) {
	return mt.toOtelMetricAttrs(metricName)
}

// HasInstruments reports whether the factory has constructed all of
// its OTel instruments. Returns false on the disabled-metrics factory
// (NoopMetricsProvider or emulator mode) and true on a fully-wired
// factory. Read-only accessor for test assertions.
func (f *Factory) HasInstruments() bool {
	return f.operationLatencies != nil &&
		f.serverLatencies != nil &&
		f.attemptLatencies != nil &&
		f.attemptLatencies2 != nil &&
		f.appBlockingLatencies != nil &&
		f.firstRespLatencies != nil &&
		f.retryCount != nil &&
		f.connErrCount != nil
}

// ClientName returns the "go-bigtable/<version>" string this process
// stamps as the client_name attribute on every exported metric.
func ClientName() string {
	return clientName
}
