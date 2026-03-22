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

type Type[Self any] interface {
	pmetric.NumberDataPoint | pmetric.HistogramDataPoint | pmetric.ExponentialHistogramDataPoint

	StartTimestamp() pcommon.Timestamp
	Timestamp() pcommon.Timestamp
	SetTimestamp(pcommon.Timestamp)
	CopyTo(Self)
}

type Aggregator struct {
	data.Aggregator
}

func Aggregate[T Type[T]](state, dp T, aggregate func(state, dp T) error) error {
	switch {
	case state.Timestamp() == 0:
		// first sample of series, no state to aggregate with
		dp.CopyTo(state)
		return nil
	case dp.StartTimestamp() < state.StartTimestamp():
		// belongs to older series
		return ErrOlderStart{Start: state.StartTimestamp(), Sample: dp.StartTimestamp()}
	case dp.Timestamp() <= state.Timestamp():
		// out of order
		return ErrOutOfOrder{Last: state.Timestamp(), Sample: dp.Timestamp()}
	}

	if err := aggregate(state, dp); err != nil {
		return err
	}

	state.SetTimestamp(dp.Timestamp())
	return nil
}

func (aggr Aggregator) Numbers(state, dp pmetric.NumberDataPoint) error {
	return Aggregate(state, dp, aggr.Aggregator.Numbers)
}

func (aggr Aggregator) Histograms(state, dp pmetric.HistogramDataPoint) error {
	return Aggregate(state, dp, aggr.Aggregator.Histograms)
}

func (aggr Aggregator) Exponential(state, dp pmetric.ExponentialHistogramDataPoint) error {
	return Aggregate(state, dp, aggr.Aggregator.Exponential)
}
