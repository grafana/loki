// Copyright 2025 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build localvalidationscheme

package model

// IsValidMetricName returns true iff name matches the pattern of MetricNameRE
// for legacy names, and iff it's valid UTF-8 if scheme is UTF8Validation.
func IsValidMetricName(n LabelValue, scheme ValidationScheme) bool {
	return isValidMetricName(n, scheme)
}
