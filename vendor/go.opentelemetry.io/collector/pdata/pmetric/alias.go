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

package pmetric // import "go.opentelemetry.io/collector/pdata/pmetric"

import "go.opentelemetry.io/collector/pdata/internal" // This file contains aliases for metric data structures.

// Metrics is the top-level struct that is propagated through the metrics pipeline.
// Use NewMetrics to create new instance, zero-initialized instance is not valid for use.
type Metrics = internal.Metrics

// NewMetrics creates a new Metrics struct.
var NewMetrics = internal.NewMetrics

// MetricDataType specifies the type of data in a Metric.
type MetricDataType = internal.MetricDataType

const (
	MetricDataTypeNone                 = internal.MetricDataTypeNone
	MetricDataTypeGauge                = internal.MetricDataTypeGauge
	MetricDataTypeSum                  = internal.MetricDataTypeSum
	MetricDataTypeHistogram            = internal.MetricDataTypeHistogram
	MetricDataTypeExponentialHistogram = internal.MetricDataTypeExponentialHistogram
	MetricDataTypeSummary              = internal.MetricDataTypeSummary
)

// MetricAggregationTemporality defines how a metric aggregator reports aggregated values.
// It describes how those values relate to the time interval over which they are aggregated.
type MetricAggregationTemporality = internal.MetricAggregationTemporality

const (
	// MetricAggregationTemporalityUnspecified is the default MetricAggregationTemporality, it MUST NOT be used.
	MetricAggregationTemporalityUnspecified = internal.MetricAggregationTemporalityUnspecified

	// MetricAggregationTemporalityDelta is a MetricAggregationTemporality for a metric aggregator which reports changes since last report time.
	MetricAggregationTemporalityDelta = internal.MetricAggregationTemporalityDelta

	// MetricAggregationTemporalityCumulative is a MetricAggregationTemporality for a metric aggregator which reports changes since a fixed start time.
	MetricAggregationTemporalityCumulative = internal.MetricAggregationTemporalityCumulative
)

// MetricDataPointFlags defines how a metric aggregator reports aggregated values.
// It describes how those values relate to the time interval over which they are aggregated.
type MetricDataPointFlags = internal.MetricDataPointFlags

const (
	// MetricDataPointFlagsNone is the default MetricDataPointFlags.
	MetricDataPointFlagsNone = internal.MetricDataPointFlagsNone
)

// NewMetricDataPointFlags returns a new MetricDataPointFlags combining the flags passed
// in as parameters.
var NewMetricDataPointFlags = internal.NewMetricDataPointFlags

// MetricDataPointFlag allow users to configure DataPointFlags. This is achieved via NewMetricDataPointFlags.
// The separation between MetricDataPointFlags and MetricDataPointFlag exists to prevent users accidentally
// comparing the value of individual flags with MetricDataPointFlags. Instead, users must use the HasFlag method.
type MetricDataPointFlag = internal.MetricDataPointFlag

const (
	// MetricDataPointFlagNoRecordedValue is flag for a metric aggregator which reports changes since last report time.
	MetricDataPointFlagNoRecordedValue = internal.MetricDataPointFlagNoRecordedValue
)

// MetricValueType specifies the type of NumberDataPoint.
// Deprecated: [v0.50.0] Use NumberDataPointValueType or ExemplarValueType instead.
type MetricValueType = internal.NumberDataPointValueType

const (
	// Deprecated: [v0.50.0] Use NumberDataPointValueTypeNone instead.
	MetricValueTypeNone = internal.NumberDataPointValueTypeNone

	// Deprecated: [v0.50.0] Use NumberDataPointValueTypeInt.
	MetricValueTypeInt = internal.NumberDataPointValueTypeInt

	// Deprecated: [v0.50.0] Use NumberDataPointValueTypeDouble instead.
	MetricValueTypeDouble = internal.NumberDataPointValueTypeDouble
)

// NumberDataPointValueType specifies the type of NumberDataPoint value.
type NumberDataPointValueType = internal.NumberDataPointValueType

const (
	NumberDataPointValueTypeNone   = internal.NumberDataPointValueTypeNone
	NumberDataPointValueTypeInt    = internal.NumberDataPointValueTypeInt
	NumberDataPointValueTypeDouble = internal.NumberDataPointValueTypeDouble
)

// ExemplarValueType specifies the type of Exemplar measurement value.
type ExemplarValueType = internal.ExemplarValueType

const (
	ExemplarValueTypeNone   = internal.ExemplarValueTypeNone
	ExemplarValueTypeInt    = internal.ExemplarValueTypeInt
	ExemplarValueTypeDouble = internal.ExemplarValueTypeDouble
)
