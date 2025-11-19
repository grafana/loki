// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package data // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/data"

import (
	"math"

	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/data/expo"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/putil/pslice"
)

// Aggregator performs an operation on two datapoints.
// Given [pmetric] types are mutable by nature, this logically works as follows:
//
//	*state = op(state, dp)
//
// See [Adder] for an implementation.
type Aggregator interface {
	Numbers(state, dp pmetric.NumberDataPoint) error
	Histograms(state, dp pmetric.HistogramDataPoint) error
	Exponential(state, dp pmetric.ExponentialHistogramDataPoint) error
}

var _ Aggregator = (*Adder)(nil)

// Adder adds (+) datapoints.
type Adder struct{}

var maxBuckets = 160

func (Adder) Numbers(state, dp pmetric.NumberDataPoint) error {
	switch dp.ValueType() {
	case pmetric.NumberDataPointValueTypeDouble:
		v := state.DoubleValue() + dp.DoubleValue()
		state.SetDoubleValue(v)
	case pmetric.NumberDataPointValueTypeInt:
		v := state.IntValue() + dp.IntValue()
		state.SetIntValue(v)
	}
	return nil
}

func (Adder) Histograms(state, dp pmetric.HistogramDataPoint) error {
	// bounds different: no way to merge, so reset observation to new boundaries
	if !pslice.Equal(state.ExplicitBounds(), dp.ExplicitBounds()) {
		dp.CopyTo(state)
		return nil
	}

	// spec requires len(BucketCounts) == len(ExplicitBounds)+1.
	// given we have limited error handling at this stage (and already verified boundaries are correct),
	// doing a best-effort add of whatever we have appears reasonable.
	n := min(state.BucketCounts().Len(), dp.BucketCounts().Len())
	for i := 0; i < n; i++ {
		sum := state.BucketCounts().At(i) + dp.BucketCounts().At(i)
		state.BucketCounts().SetAt(i, sum)
	}

	state.SetCount(state.Count() + dp.Count())

	if state.HasSum() && dp.HasSum() {
		state.SetSum(state.Sum() + dp.Sum())
	} else {
		state.RemoveSum()
	}

	if state.HasMin() && dp.HasMin() {
		state.SetMin(math.Min(state.Min(), dp.Min()))
	} else {
		state.RemoveMin()
	}

	if state.HasMax() && dp.HasMax() {
		state.SetMax(math.Max(state.Max(), dp.Max()))
	} else {
		state.RemoveMax()
	}

	return nil
}

func (Adder) Exponential(state, dp pmetric.ExponentialHistogramDataPoint) error {
	type H = pmetric.ExponentialHistogramDataPoint

	if state.Scale() != dp.Scale() {
		hi, lo := expo.HiLo(state, dp, H.Scale)
		from, to := expo.Scale(hi.Scale()), expo.Scale(lo.Scale())
		expo.Downscale(hi.Positive(), from, to)
		expo.Downscale(hi.Negative(), from, to)
		hi.SetScale(lo.Scale())
	}

	// Downscale if an expected number of buckets after the merge is too large.
	from := expo.Scale(state.Scale())
	to := min(
		expo.Limit(maxBuckets, from, state.Positive(), dp.Positive()),
		expo.Limit(maxBuckets, from, state.Negative(), dp.Negative()),
	)
	if from != to {
		expo.Downscale(state.Positive(), from, to)
		expo.Downscale(state.Negative(), from, to)
		expo.Downscale(dp.Positive(), from, to)
		expo.Downscale(dp.Negative(), from, to)
		state.SetScale(int32(to))
		dp.SetScale(int32(to))
	}

	if state.ZeroThreshold() != dp.ZeroThreshold() {
		hi, lo := expo.HiLo(state, dp, H.ZeroThreshold)
		expo.WidenZero(lo, hi.ZeroThreshold())
	}

	expo.Merge(state.Positive(), dp.Positive())
	expo.Merge(state.Negative(), dp.Negative())

	state.SetCount(state.Count() + dp.Count())
	state.SetZeroCount(state.ZeroCount() + dp.ZeroCount())

	if state.HasSum() && dp.HasSum() {
		state.SetSum(state.Sum() + dp.Sum())
	} else {
		state.RemoveSum()
	}

	if state.HasMin() && dp.HasMin() {
		state.SetMin(math.Min(state.Min(), dp.Min()))
	} else {
		state.RemoveMin()
	}

	if state.HasMax() && dp.HasMax() {
		state.SetMax(math.Max(state.Max(), dp.Max()))
	} else {
		state.RemoveMax()
	}

	return nil
}
