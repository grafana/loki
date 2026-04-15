// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package expo implements various operations on exponential histograms and their bucket counts
package expo // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/data/expo"

import "go.opentelemetry.io/collector/pdata/pmetric"

type (
	DataPoint = pmetric.ExponentialHistogramDataPoint
	Buckets   = pmetric.ExponentialHistogramDataPointBuckets
)

// Abs returns a view into the buckets using an absolute scale
func Abs(bs Buckets) Absolute {
	return Absolute{buckets: bs}
}

type buckets = Buckets

// Absolute addresses bucket counts using an absolute scale, such that it is
// interoperable with [Scale].
//
// It spans from [[Absolute.Lower]:[Absolute.Upper]]
//
// NOTE: The zero-value is unusable, use [Abs] to construct
type Absolute struct {
	buckets
}

// Abs returns the value at absolute index 'at'
func (a Absolute) Abs(at int) uint64 {
	if i, ok := a.idx(at); ok {
		return a.BucketCounts().At(i)
	}
	return 0
}

// Upper returns the minimal index outside the set, such that every i < Upper
func (a Absolute) Upper() int {
	return a.BucketCounts().Len() + int(a.Offset())
}

// Lower returns the minimal index inside the set, such that every i >= Lower
func (a Absolute) Lower() int {
	return int(a.Offset())
}

func (a Absolute) idx(at int) (int, bool) {
	idx := at - a.Lower()
	return idx, idx >= 0 && idx < a.BucketCounts().Len()
}
