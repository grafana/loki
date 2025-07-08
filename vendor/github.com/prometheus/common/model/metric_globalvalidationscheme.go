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

//go:build !localvalidationscheme

package model

// NameValidationScheme determines the global default method of the name
// validation to be used by all calls to IsValidMetricName() and LabelName
// IsValid().
//
// Deprecated: This variable should not be used and might be removed in the
// far future. If you wish to stick to the legacy name validation use
// `IsValidLegacyMetricName()` and `LabelName.IsValidLegacy()` methods
// instead. This variable is here as an escape hatch for emergency cases,
// given the recent change from `LegacyValidation` to `UTF8Validation`, e.g.,
// to delay UTF-8 migrations in time or aid in debugging unforeseen results of
// the change. In such a case, a temporary assignment to `LegacyValidation`
// value in the `init()` function in your main.go or so, could be considered.
//
// Historically we opted for a global variable for feature gating different
// validation schemes in operations that were not otherwise easily adjustable
// (e.g. Labels yaml unmarshaling). That could have been a mistake, a separate
// Labels structure or package might have been a better choice. Given the
// change was made and many upgraded the common already, we live this as-is
// with this warning and learning for the future.
var NameValidationScheme = UTF8Validation

// IsValidMetricName returns true iff name matches the pattern of MetricNameRE
// for legacy names, and iff it's valid UTF-8 if scheme is UTF8Validation.
func IsValidMetricName(n LabelValue) bool {
	return isValidMetricName(n, NameValidationScheme)
}
