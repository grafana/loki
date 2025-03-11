// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package expo // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/data/expo"

import (
	"fmt"
	"math"
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
func (scale Scale) Bounds(index int) (min, max float64) {
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
		// any attempt to do so would yield erronous data.
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
	size := counts.Len() / 2
	if counts.Len()%2 != 0 || bs.Offset()%2 != 0 {
		size++
	}

	// merging needs to happen in pairs aligned to i=0. if offset is non-even,
	// we need to shift the whole merging by one to make above condition true.
	shift := 0
	if bs.Offset()%2 != 0 {
		bs.SetOffset(bs.Offset() - 1)
		shift--
	}
	bs.SetOffset(bs.Offset() / 2)

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
	// 2. [pcommon.Uint64Slice] can not, in fact, be sliced, so getting rid
	//    of it would alloc ¯\_(ツ)_/¯
	for i := size; i < counts.Len(); i++ {
		counts.SetAt(i, 0)
	}
}
