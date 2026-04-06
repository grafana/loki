// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package expo // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/data/expo"

import (
	"fmt"
	"math"

	"go.opentelemetry.io/collector/pdata/pmetric"
)

type Scale int32

// Idx gives the bucket index v belongs into
func (scale Scale) Idx(v float64) int {
	// from: https://opentelemetry.io/docs/specs/otel/metrics/data-model/#all-scales-use-the-logarithm-function

	// Special case for power-of-two values.
	if frac, exp := math.Frexp(v); frac == 0.5 {
		return ((exp - 1) << scale) - 1
	}

	scaleFactor := math.Ldexp(math.Log2E, int(scale))
	// Note: math.Floor(value) equals math.Ceil(value)-1 when value
	// is not a power of two, which is checked above.
	return int(math.Floor(math.Log(v) * scaleFactor))
}

// Bounds returns the half-open interval (min,max] of the bucket at index.
// This means a value min < v <= max belongs to this bucket.
//
// NOTE: this is different from Go slice intervals, which are [a,b)
func (scale Scale) Bounds(index int) (minVal, maxVal float64) {
	// from: https://opentelemetry.io/docs/specs/otel/metrics/data-model/#all-scales-use-the-logarithm-function
	lower := func(index int) float64 {
		inverseFactor := math.Ldexp(math.Ln2, int(-scale))
		return math.Exp(float64(index) * inverseFactor)
	}

	return lower(index), lower(index + 1)
}

// Downscale collapses the buckets of bs until scale 'to' is reached
func Downscale(bs Buckets, from, to Scale) {
	switch {
	case from == to:
		return
	case from < to:
		// because even distribution within the buckets cannot be assumed, it is
		// not possible to correctly upscale (split) buckets.
		// any attempt to do so would yield erroneous data.
		panic(fmt.Sprintf("cannot upscale without introducing error (%d -> %d)", from, to))
	}

	for at := from; at > to; at-- {
		Collapse(bs)
	}
}

// Collapse merges adjacent buckets and zeros the remaining area:
//
//	before:	1 1 1 1 1 1 1 1 1 1 1 1
//	after:	 2   2   2   2   2   2   0   0   0   0   0   0
//
// Due to the "perfect subsetting" property of exponential histograms, this
// gives the same observation as before, but recorded at scale-1. See
// https://opentelemetry.io/docs/specs/otel/metrics/data-model/#exponential-scale.
//
// Because every bucket now spans twice as much range, half of the allocated
// counts slice is technically no longer required. It is zeroed but left in
// place to avoid future allocations, because observations may happen in that
// area at a later time.
func Collapse(bs Buckets) {
	counts := bs.BucketCounts()

	offsetWasOdd := bs.Offset()%2 != 0
	shift := 0
	if offsetWasOdd {
		shift--
	}

	size := counts.Len() / 2
	if counts.Len()%2 != 0 || offsetWasOdd {
		size++
	}

	if offsetWasOdd {
		bs.SetOffset(bs.Offset() - 1)
	}
	bs.SetOffset(bs.Offset() / 2)

	if counts.Len() == 0 {
		return
	}

	for i := 0; i < size; i++ {
		// size is ~half of len. we add two buckets per iteration.
		// k jumps in steps of 2, shifted if offset makes this necessary.
		k := i*2 + shift

		// special case: we just started and had to shift. the left half of the
		// new bucket is not actually stored, so only use counts[0].
		if i == 0 && k == -1 {
			counts.SetAt(i, counts.At(k+1))
			continue
		}

		// new[k] = old[k]+old[k+1]
		counts.SetAt(i, counts.At(k))
		if k+1 < counts.Len() {
			counts.SetAt(i, counts.At(k)+counts.At(k+1))
		}
	}

	// zero the excess area. its not needed to represent the observation
	// anymore, but kept for two reasons:
	// 1. future observations may need it, no need to re-alloc then if kept
	// 2. [pcommon.Uint64Slice] cannot, in fact, be sliced, so getting rid
	//    of it would alloc ¯\_(ツ)_/¯
	for i := size; i < counts.Len(); i++ {
		counts.SetAt(i, 0)
	}
}

// Limit returns a target Scale that when be downscaled to,
// the total bucket count after [Merge] never exceeds maxBuckets.
func Limit(maxBuckets int, scale Scale, arel, brel pmetric.ExponentialHistogramDataPointBuckets) Scale {
	a, b := Abs(arel), Abs(brel)

	lo := min(a.Lower(), b.Lower())
	up := max(a.Upper(), b.Upper())

	// Skip leading and trailing zeros.
	for lo < up && a.Abs(lo) == 0 && b.Abs(lo) == 0 {
		lo++
	}
	for lo < up-1 && a.Abs(up-1) == 0 && b.Abs(up-1) == 0 {
		up--
	}

	// Keep downscaling until the number of buckets is within the limit.
	for up-lo > maxBuckets {
		lo /= 2
		up /= 2
		scale--
	}

	return scale
}
