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
// Provenance-includes-location: https://github.com/prometheus/prometheus/blob/93e991ef7ed19cc997a9360c8016cac3767b8057/storage/remote/otlptranslator/prometheus/metric_name_builder.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Copyright The Prometheus Authors
// Provenance-includes-location: https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/95e8f8fdc2a9dc87230406c9a3cf02be4fd68bea/pkg/translator/prometheus/normalize_name.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Copyright The OpenTelemetry Authors.

package otlptranslator

import (
	"slices"
	"strings"
	"unicode"

	"github.com/grafana/regexp"
)

// The map to translate OTLP units to Prometheus units
// OTLP metrics use the c/s notation as specified at https://ucum.org/ucum.html
// (See also https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/metrics/semantic_conventions/README.md#instrument-units)
// Prometheus best practices for units: https://prometheus.io/docs/practices/naming/#base-units
// OpenMetrics specification for units: https://github.com/prometheus/OpenMetrics/blob/v1.0.0/specification/OpenMetrics.md#units-and-base-units
var unitMap = map[string]string{
	// Time
	"d":   "days",
	"h":   "hours",
	"min": "minutes",
	"s":   "seconds",
	"ms":  "milliseconds",
	"us":  "microseconds",
	"ns":  "nanoseconds",

	// Bytes
	"By":   "bytes",
	"KiBy": "kibibytes",
	"MiBy": "mebibytes",
	"GiBy": "gibibytes",
	"TiBy": "tibibytes",
	"KBy":  "kilobytes",
	"MBy":  "megabytes",
	"GBy":  "gigabytes",
	"TBy":  "terabytes",

	// SI
	"m": "meters",
	"V": "volts",
	"A": "amperes",
	"J": "joules",
	"W": "watts",
	"g": "grams",

	// Misc
	"Cel": "celsius",
	"Hz":  "hertz",
	"1":   "",
	"%":   "percent",
}

// The map that translates the "per" unit.
// Example: s => per second (singular).
var perUnitMap = map[string]string{
	"s":  "second",
	"m":  "minute",
	"h":  "hour",
	"d":  "day",
	"w":  "week",
	"mo": "month",
	"y":  "year",
}

// MetricNamer is a helper struct to build metric names.
type MetricNamer struct {
	Namespace          string
	WithMetricSuffixes bool
	UTF8Allowed        bool
}

// Metric is a helper struct that holds information about a metric.
type Metric struct {
	Name string
	Unit string
	Type MetricType
}

// Build builds a metric name for the specified metric.
//
// If UTF8Allowed is true, the metric name is returned as is, only with the addition of type/unit suffixes and namespace preffix if required.
// Otherwise the metric name is normalized to be Prometheus-compliant.
// See rules at https://prometheus.io/docs/concepts/data_model/#metric-names-and-labels,
// https://prometheus.io/docs/practices/naming/#metric-and-label-naming
func (mn *MetricNamer) Build(metric Metric) string {
	if mn.UTF8Allowed {
		return mn.buildMetricName(metric.Name, metric.Unit, metric.Type)
	}
	return mn.buildCompliantMetricName(metric.Name, metric.Unit, metric.Type)
}

func (mn *MetricNamer) buildCompliantMetricName(name, unit string, metricType MetricType) string {
	// Full normalization following standard Prometheus naming conventions
	if mn.WithMetricSuffixes {
		return normalizeName(name, unit, metricType, mn.Namespace)
	}

	// Simple case (no full normalization, no units, etc.).
	metricName := strings.Join(strings.FieldsFunc(name, func(r rune) bool {
		return invalidMetricCharRE.MatchString(string(r))
	}), "_")

	// Namespace?
	if mn.Namespace != "" {
		return mn.Namespace + "_" + metricName
	}

	// Metric name starts with a digit? Prefix it with an underscore.
	if metricName != "" && unicode.IsDigit(rune(metricName[0])) {
		metricName = "_" + metricName
	}

	return metricName
}

var (
	// Regexp for metric name characters that should be replaced with _.
	invalidMetricCharRE   = regexp.MustCompile(`[^a-zA-Z0-9:_]`)
	multipleUnderscoresRE = regexp.MustCompile(`__+`)
)

// isValidCompliantMetricChar checks if a rune is a valid metric name character (a-z, A-Z, 0-9, :).
func isValidCompliantMetricChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == ':'
}

// replaceInvalidMetricChar replaces invalid metric name characters with underscore.
func replaceInvalidMetricChar(r rune) rune {
	if isValidCompliantMetricChar(r) {
		return r
	}
	return '_'
}

// Build a normalized name for the specified metric.
func normalizeName(name, unit string, metricType MetricType, namespace string) string {
	// Split metric name into "tokens" (of supported metric name runes).
	// Note that this has the side effect of replacing multiple consecutive underscores with a single underscore.
	// This is part of the OTel to Prometheus specification: https://github.com/open-telemetry/opentelemetry-specification/blob/v1.38.0/specification/compatibility/prometheus_and_openmetrics.md#otlp-metric-points-to-prometheus.
	nameTokens := strings.FieldsFunc(
		name,
		func(r rune) bool { return !isValidCompliantMetricChar(r) },
	)

	mainUnitSuffix, perUnitSuffix := buildUnitSuffixes(unit)
	nameTokens = addUnitTokens(nameTokens, cleanUpUnit(mainUnitSuffix), cleanUpUnit(perUnitSuffix))

	// Append _total for Counters
	if metricType == MetricTypeMonotonicCounter {
		nameTokens = append(removeItem(nameTokens, "total"), "total")
	}

	// Append _ratio for metrics with unit "1"
	// Some OTel receivers improperly use unit "1" for counters of objects
	// See https://github.com/open-telemetry/opentelemetry-collector-contrib/issues?q=is%3Aissue+some+metric+units+don%27t+follow+otel+semantic+conventions
	// Until these issues have been fixed, we're appending `_ratio` for gauges ONLY
	// Theoretically, counters could be ratios as well, but it's absurd (for mathematical reasons)
	if unit == "1" && metricType == MetricTypeGauge {
		nameTokens = append(removeItem(nameTokens, "ratio"), "ratio")
	}

	// Namespace?
	if namespace != "" {
		nameTokens = append([]string{namespace}, nameTokens...)
	}

	// Build the string from the tokens, separated with underscores
	normalizedName := strings.Join(nameTokens, "_")

	// Metric name cannot start with a digit, so prefix it with "_" in this case
	if normalizedName != "" && unicode.IsDigit(rune(normalizedName[0])) {
		normalizedName = "_" + normalizedName
	}

	return normalizedName
}

// addUnitTokens will add the suffixes to the nameTokens if they are not already present.
// It will also remove trailing underscores from the main suffix to avoid double underscores
// when joining the tokens.
//
// If the 'per' unit ends with underscore, the underscore will be removed. If the per unit is just
// 'per_', it will be entirely removed.
func addUnitTokens(nameTokens []string, mainUnitSuffix, perUnitSuffix string) []string {
	if slices.Contains(nameTokens, mainUnitSuffix) {
		mainUnitSuffix = ""
	}

	if perUnitSuffix == "per_" {
		perUnitSuffix = ""
	} else {
		perUnitSuffix = strings.TrimSuffix(perUnitSuffix, "_")
		if slices.Contains(nameTokens, perUnitSuffix) {
			perUnitSuffix = ""
		}
	}

	if perUnitSuffix != "" {
		mainUnitSuffix = strings.TrimSuffix(mainUnitSuffix, "_")
	}

	if mainUnitSuffix != "" {
		nameTokens = append(nameTokens, mainUnitSuffix)
	}
	if perUnitSuffix != "" {
		nameTokens = append(nameTokens, perUnitSuffix)
	}
	return nameTokens
}

// Remove the specified value from the slice.
func removeItem(slice []string, value string) []string {
	newSlice := make([]string, 0, len(slice))
	for _, sliceEntry := range slice {
		if sliceEntry != value {
			newSlice = append(newSlice, sliceEntry)
		}
	}
	return newSlice
}

func (mn *MetricNamer) buildMetricName(name, unit string, metricType MetricType) string {
	if mn.Namespace != "" {
		name = mn.Namespace + "_" + name
	}

	if mn.WithMetricSuffixes {
		mainUnitSuffix, perUnitSuffix := buildUnitSuffixes(unit)
		if mainUnitSuffix != "" {
			name = name + "_" + mainUnitSuffix
		}
		if perUnitSuffix != "" {
			name = name + "_" + perUnitSuffix
		}

		// Append _total for Counters
		if metricType == MetricTypeMonotonicCounter {
			name += "_total"
		}

		// Append _ratio for metrics with unit "1"
		// Some OTel receivers improperly use unit "1" for counters of objects
		// See https://github.com/open-telemetry/opentelemetry-collector-contrib/issues?q=is%3Aissue+some+metric+units+don%27t+follow+otel+semantic+conventions
		// Until these issues have been fixed, we're appending `_ratio` for gauges ONLY
		// Theoretically, counters could be ratios as well, but it's absurd (for mathematical reasons)
		if unit == "1" && metricType == MetricTypeGauge {
			name += "_ratio"
		}
	}
	return name
}
