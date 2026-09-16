// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import (
	"go.opentelemetry.io/collector/pdata/internal"
)

// AggregationTemporality specifies the method of aggregating metric values,
// either DELTA (change since last report) or CUMULATIVE (total since a fixed
// start time).
//
// Deprecated: [v0.146.0] Type was removed without replacement in the Profiles signal.
type AggregationTemporality int32

const (
	// AggregationTemporalityUnspecified is the default AggregationTemporality, it MUST NOT be used.
	//
	// Deprecated: [v0.146.0] This is no longer supported by the Profiles signal.
	AggregationTemporalityUnspecified = AggregationTemporality(internal.AggregationTemporality_AGGREGATION_TEMPORALITY_UNSPECIFIED)
	// AggregationTemporalityDelta is a AggregationTemporality for a metric aggregator which reports changes since last report time.
	//
	// Deprecated: [v0.146.0] This is no longer supported by the Profiles signal.
	AggregationTemporalityDelta = AggregationTemporality(internal.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA)
	// AggregationTemporalityCumulative is a AggregationTemporality for a metric aggregator which reports changes since a fixed start time.
	//
	// Deprecated: [v0.146.0] This is no longer supported by the Profiles signal.
	AggregationTemporalityCumulative = AggregationTemporality(internal.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE)
)

// String returns the string representation of the AggregationTemporality.
//
// Deprecated: [v0.146.0] Type was removed without replacement in the Profiles signal.
func (at AggregationTemporality) String() string {
	switch at {
	case AggregationTemporalityUnspecified:
		return "Unspecified"
	case AggregationTemporalityDelta:
		return "Delta"
	case AggregationTemporalityCumulative:
		return "Cumulative"
	}
	return ""
}
