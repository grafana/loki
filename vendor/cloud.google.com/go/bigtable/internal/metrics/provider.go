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

// MetricsProvider is the sentinel type ClientConfig.MetricsProvider
// accepts. The two concrete implementations are DefaultMetricsProvider
// (enable built-in metrics with the Cloud Monitoring exporter) and
// NoopMetricsProvider (disable them). A nil MetricsProvider is treated
// as DefaultMetricsProvider for backward compatibility. Re-exported
// from the bigtable package as bigtable.MetricsProvider /
// bigtable.DefaultMetricsProvider / bigtable.NoopMetricsProvider via
// type alias.
type MetricsProvider interface {
	isMetricsProvider()
}

// DefaultMetricsProvider enables the built-in Cloud Monitoring metrics
// exporter. This is the explicit form of the default behavior — leaving
// ClientConfig.MetricsProvider unset (nil) has the same effect.
type DefaultMetricsProvider struct{}

// isMetricsProvider marks DefaultMetricsProvider as a MetricsProvider.
func (DefaultMetricsProvider) isMetricsProvider() {}

// NoopMetricsProvider disables the built-in metrics.
type NoopMetricsProvider struct{}

// isMetricsProvider marks NoopMetricsProvider as a MetricsProvider.
func (NoopMetricsProvider) isMetricsProvider() {}
