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

import "go.opentelemetry.io/collector/pdata/pmetric"

// MetricsMarshaler is an alias for pmetric.Marshaler interface.
// Deprecated: [v0.49.0] Use pmetric.Marshaler instead.
type MetricsMarshaler = pmetric.Marshaler

// MetricsUnmarshaler is an alias for pmetric.Unmarshaler interface.
// Deprecated: [v0.49.0] Use pmetric.Unmarshaler instead.
type MetricsUnmarshaler = pmetric.Unmarshaler

// MetricsSizer is an alias for pmetric.Sizer interface.
// Deprecated: [v0.49.0] Use pmetric.Sizer instead.
type MetricsSizer = pmetric.Sizer

// Metrics is an alias for pmetric.Metrics structure.
// Deprecated: [v0.49.0] Use pmetric.Metrics instead.
type Metrics = pmetric.Metrics

// NewMetrics is an alias for a function to create new Metrics.
// Deprecated: [v0.49.0] Use pmetric.NewMetrics instead.
var NewMetrics = pmetric.NewMetrics

// MetricDataType is an alias for pmetric.MetricDataType type.
// Deprecated: [v0.49.0] Use pmetric.MetricDataType instead.
type MetricDataType = pmetric.MetricDataType

const (

	// Deprecated: [v0.49.0] Use pmetric.MetricDataTypeNone instead.
	MetricDataTypeNone = pmetric.MetricDataTypeNone

	// Deprecated: [v0.49.0] Use pmetric.MetricDataTypeGauge instead.
	MetricDataTypeGauge = pmetric.MetricDataTypeGauge

	// Deprecated: [v0.49.0] Use pmetric.MetricDataTypeSum instead.
	MetricDataTypeSum = pmetric.MetricDataTypeSum

	// Deprecated: [v0.49.0] Use pmetric.MetricDataTypeHistogram instead.
	MetricDataTypeHistogram = pmetric.MetricDataTypeHistogram

	// Deprecated: [v0.49.0] Use pmetric.MetricDataTypeExponentialHistogram instead.
	MetricDataTypeExponentialHistogram = pmetric.MetricDataTypeExponentialHistogram

	// Deprecated: [v0.49.0] Use pmetric.MetricDataTypeSummary instead.
	MetricDataTypeSummary = pmetric.MetricDataTypeSummary
)

// MetricAggregationTemporality is an alias for pmetric.MetricAggregationTemporality type.
// Deprecated: [v0.49.0] Use pmetric.MetricAggregationTemporality instead.
type MetricAggregationTemporality = pmetric.MetricAggregationTemporality

const (

	// Deprecated: [v0.49.0] Use pmetric.MetricAggregationTemporalityUnspecified instead.
	MetricAggregationTemporalityUnspecified = pmetric.MetricAggregationTemporalityUnspecified

	// Deprecated: [v0.49.0] Use pmetric.MetricAggregationTemporalityDelta instead.
	MetricAggregationTemporalityDelta = pmetric.MetricAggregationTemporalityDelta

	// Deprecated: [v0.49.0] Use pmetric.MetricAggregationTemporalityCumulative instead.
	MetricAggregationTemporalityCumulative = pmetric.MetricAggregationTemporalityCumulative
)

// MetricDataPointFlags is an alias for pmetric.MetricDataPointFlags type.
// Deprecated: [v0.49.0] Use pmetric.MetricDataPointFlags instead.
type MetricDataPointFlags = pmetric.MetricDataPointFlags

const (
	// Deprecated: [v0.49.0] Use pmetric.MetricDataPointFlagsNone instead.
	MetricDataPointFlagsNone = pmetric.MetricDataPointFlagsNone
)

// NewMetricDataPointFlags is an alias for a function to create new MetricDataPointFlags.
// Deprecated: [v0.49.0] Use pmetric.NewMetricDataPointFlags instead.
var NewMetricDataPointFlags = pmetric.NewMetricDataPointFlags

// MetricDataPointFlag is an alias for pmetric.MetricDataPointFlag type.
// Deprecated: [v0.49.0] Use pmetric.MetricDataPointFlag instead.
type MetricDataPointFlag = pmetric.MetricDataPointFlag

const (
	// Deprecated: [v0.49.0] Use pmetric.MetricDataPointFlagNoRecordedValue instead.
	MetricDataPointFlagNoRecordedValue = pmetric.MetricDataPointFlagNoRecordedValue
)

// MetricValueType is an alias for pmetric.MetricValueType type.
// Deprecated: [v0.49.0] Use pmetric.MetricValueType instead.
type MetricValueType = pmetric.MetricValueType //nolint:staticcheck

const (

	// Deprecated: [v0.49.0] Use pmetric.MetricValueTypeNone instead.
	MetricValueTypeNone = pmetric.MetricValueTypeNone //nolint:staticcheck

	// Deprecated: [v0.49.0] Use pmetric.MetricValueTypeInt instead.
	MetricValueTypeInt = pmetric.MetricValueTypeInt //nolint:staticcheck

	// Deprecated: [v0.49.0] Use pmetric.MetricValueTypeDouble instead.
	MetricValueTypeDouble = pmetric.MetricValueTypeDouble //nolint:staticcheck
)
