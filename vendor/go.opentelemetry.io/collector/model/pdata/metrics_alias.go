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

package pdata // import "go.opentelemetry.io/collector/model/pdata"

// This file contains aliases for metric data structures.

import (
	"go.opentelemetry.io/collector/model/internal/pdata"
)

// MetricsMarshaler is an alias for pdata.MetricsMarshaler interface.
type MetricsMarshaler = pdata.MetricsMarshaler

// MetricsUnmarshaler is an alias for pdata.MetricsUnmarshaler interface.
type MetricsUnmarshaler = pdata.MetricsUnmarshaler

// MetricsSizer is an alias for pdata.MetricsSizer interface.
type MetricsSizer = pdata.MetricsSizer

// Metrics is an alias for pdata.Metrics structure.
type Metrics = pdata.Metrics

// NewMetrics is an alias for a function to create new Metrics.
var NewMetrics = pdata.NewMetrics

// MetricDataType is an alias for pdata.MetricDataType type.
type MetricDataType = pdata.MetricDataType

const (
	MetricDataTypeNone                 = pdata.MetricDataTypeNone
	MetricDataTypeGauge                = pdata.MetricDataTypeGauge
	MetricDataTypeSum                  = pdata.MetricDataTypeSum
	MetricDataTypeHistogram            = pdata.MetricDataTypeHistogram
	MetricDataTypeExponentialHistogram = pdata.MetricDataTypeExponentialHistogram
	MetricDataTypeSummary              = pdata.MetricDataTypeSummary
)

// MetricAggregationTemporality is an alias for pdata.MetricAggregationTemporality type.
type MetricAggregationTemporality = pdata.MetricAggregationTemporality

const (
	MetricAggregationTemporalityUnspecified = pdata.MetricAggregationTemporalityUnspecified
	MetricAggregationTemporalityDelta       = pdata.MetricAggregationTemporalityDelta
	MetricAggregationTemporalityCumulative  = pdata.MetricAggregationTemporalityCumulative
)

// MetricDataPointFlags is an alias for pdata.MetricDataPointFlags type.
type MetricDataPointFlags = pdata.MetricDataPointFlags

const (
	MetricDataPointFlagsNone = pdata.MetricDataPointFlagsNone
)

// NewMetricDataPointFlags is an alias for a function to create new MetricDataPointFlags.
var NewMetricDataPointFlags = pdata.NewMetricDataPointFlags

// MetricDataPointFlag is an alias for pdata.MetricDataPointFlag type.
type MetricDataPointFlag = pdata.MetricDataPointFlag

const (
	MetricDataPointFlagNoRecordedValue = pdata.MetricDataPointFlagNoRecordedValue
)

// MetricValueType is an alias for pdata.MetricValueType type.
type MetricValueType = pdata.MetricValueType

const (
	MetricValueTypeNone   = pdata.MetricValueTypeNone
	MetricValueTypeInt    = pdata.MetricValueTypeInt
	MetricValueTypeDouble = pdata.MetricValueTypeDouble
)

// Deprecated: [v0.48.0] Use ScopeMetricsSlice instead.
type InstrumentationLibraryMetricsSlice = pdata.ScopeMetricsSlice

// Deprecated: [v0.48.0] Use NewScopeMetricsSlice instead.
var NewInstrumentationLibraryMetricsSlice = pdata.NewScopeMetricsSlice

// Deprecated: [v0.48.0] Use ScopeMetrics instead.
type InstrumentationLibraryMetrics = pdata.ScopeMetrics

// Deprecated: [v0.48.0] Use NewScopeMetrics instead.
var NewInstrumentationLibraryMetrics = pdata.NewScopeMetrics
