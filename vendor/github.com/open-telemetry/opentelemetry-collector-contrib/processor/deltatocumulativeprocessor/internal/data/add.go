// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package data // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/data"

import (
	"math"

	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/data/expo"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/putil/pslice"
)

func (dp Number) Add(in Number) Number {
	switch in.ValueType() {
	case pmetric.NumberDataPointValueTypeDouble:
		v := dp.DoubleValue() + in.DoubleValue()
		dp.SetDoubleValue(v)
	case pmetric.NumberDataPointValueTypeInt:
		v := dp.IntValue() + in.IntValue()
		dp.SetIntValue(v)
	}
	dp.SetTimestamp(in.Timestamp())
	return dp
}

func (dp Histogram) Add(in Histogram) Histogram {
	// bounds different: no way to merge, so reset observation to new boundaries
	if !pslice.Equal(dp.ExplicitBounds(), in.ExplicitBounds()) {
		in.MoveTo(dp.HistogramDataPoint)
		return dp
	}

	// spec requires len(BucketCounts) == len(ExplicitBounds)+1.
	// given we have limited error handling at this stage (and already verified boundaries are correct),
	// doing a best-effort add of whatever we have appears reasonable.
	n := min(dp.BucketCounts().Len(), in.BucketCounts().Len())
	for i := 0; i < n; i++ {
		sum := dp.BucketCounts().At(i) + in.BucketCounts().At(i)
		dp.BucketCounts().SetAt(i, sum)
	}

	dp.SetTimestamp(in.Timestamp())
	dp.SetCount(dp.Count() + in.Count())

	if dp.HasSum() && in.HasSum() {
		dp.SetSum(dp.Sum() + in.Sum())
	} else {
		dp.RemoveSum()
	}

	if dp.HasMin() && in.HasMin() {
		dp.SetMin(math.Min(dp.Min(), in.Min()))
	} else {
		dp.RemoveMin()
	}

	if dp.HasMax() && in.HasMax() {
		dp.SetMax(math.Max(dp.Max(), in.Max()))
	} else {
		dp.RemoveMax()
	}

	return dp
}

func (dp ExpHistogram) Add(in ExpHistogram) ExpHistogram {
	type H = ExpHistogram

	if dp.Scale() != in.Scale() {
		hi, lo := expo.HiLo(dp, in, H.Scale)
		from, to := expo.Scale(hi.Scale()), expo.Scale(lo.Scale())
		expo.Downscale(hi.Positive(), from, to)
		expo.Downscale(hi.Negative(), from, to)
		hi.SetScale(lo.Scale())
	}

	if dp.ZeroThreshold() != in.ZeroThreshold() {
		hi, lo := expo.HiLo(dp, in, H.ZeroThreshold)
		expo.WidenZero(lo.DataPoint, hi.ZeroThreshold())
	}

	expo.Merge(dp.Positive(), in.Positive())
	expo.Merge(dp.Negative(), in.Negative())

	dp.SetTimestamp(in.Timestamp())
	dp.SetCount(dp.Count() + in.Count())
	dp.SetZeroCount(dp.ZeroCount() + in.ZeroCount())

	if dp.HasSum() && in.HasSum() {
		dp.SetSum(dp.Sum() + in.Sum())
	} else {
		dp.RemoveSum()
	}

	if dp.HasMin() && in.HasMin() {
		dp.SetMin(math.Min(dp.Min(), in.Min()))
	} else {
		dp.RemoveMin()
	}

	if dp.HasMax() && in.HasMax() {
		dp.SetMax(math.Max(dp.Max(), in.Max()))
	} else {
		dp.RemoveMax()
	}

	return dp
}

func (dp Summary) Add(Summary) Summary {
	panic("todo")
}
