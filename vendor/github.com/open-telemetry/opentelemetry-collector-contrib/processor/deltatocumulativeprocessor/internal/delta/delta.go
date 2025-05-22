// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package delta // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/delta"

import (
	"fmt"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/data"
)

type ErrOlderStart struct {
	Start  pcommon.Timestamp
	Sample pcommon.Timestamp
}

func (e ErrOlderStart) Error() string {
	return fmt.Sprintf("dropped sample with start_time=%s, because series only starts at start_time=%s. consider checking for multiple processes sending the exact same series", e.Sample, e.Start)
}

type ErrOutOfOrder struct {
	Last   pcommon.Timestamp
	Sample pcommon.Timestamp
}

func (e ErrOutOfOrder) Error() string {
	return fmt.Sprintf("out of order: dropped sample from time=%s, because series is already at time=%s", e.Sample, e.Last)
}

type Type interface {
	pmetric.NumberDataPoint | pmetric.HistogramDataPoint | pmetric.ExponentialHistogramDataPoint

	StartTimestamp() pcommon.Timestamp
	Timestamp() pcommon.Timestamp
}

// AccumulateInto adds state and dp, storing the result in state
//
//	state = state + dp
func AccumulateInto[T Type](state, dp T) error {
	switch {
	case dp.StartTimestamp() < state.StartTimestamp():
		// belongs to older series
		return ErrOlderStart{Start: state.StartTimestamp(), Sample: dp.StartTimestamp()}
	case dp.Timestamp() <= state.Timestamp():
		// out of order
		return ErrOutOfOrder{Last: state.Timestamp(), Sample: dp.Timestamp()}
	}

	switch dp := any(dp).(type) {
	case pmetric.NumberDataPoint:
		state := any(state).(pmetric.NumberDataPoint)
		data.Number{NumberDataPoint: state}.Add(data.Number{NumberDataPoint: dp})
	case pmetric.HistogramDataPoint:
		state := any(state).(pmetric.HistogramDataPoint)
		data.Histogram{HistogramDataPoint: state}.Add(data.Histogram{HistogramDataPoint: dp})
	case pmetric.ExponentialHistogramDataPoint:
		state := any(state).(pmetric.ExponentialHistogramDataPoint)
		data.ExpHistogram{DataPoint: state}.Add(data.ExpHistogram{DataPoint: dp})
	}
	return nil
}
