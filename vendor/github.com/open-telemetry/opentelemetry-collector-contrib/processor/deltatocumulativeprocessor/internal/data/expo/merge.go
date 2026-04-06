// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package expo // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/data/expo"

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
)

// Merge combines the counts of buckets a and b into a.
// Both buckets MUST be of same scale
func Merge(arel, brel Buckets) {
	if brel.BucketCounts().Len() == 0 {
		return
	}
	if arel.BucketCounts().Len() == 0 {
		brel.CopyTo(arel)
		return
	}

	a, b := Abs(arel), Abs(brel)

	lo := min(a.Lower(), b.Lower())
	up := max(a.Upper(), b.Upper())

	// Skip leading and trailing zeros to reduce number of buckets.
	// As we cap number of buckets this allows us to have higher scale.
	for lo < up && a.Abs(lo) == 0 && b.Abs(lo) == 0 {
		lo++
	}
	for lo < up-1 && a.Abs(up-1) == 0 && b.Abs(up-1) == 0 {
		up--
	}

	size := up - lo

	counts := pcommon.NewUInt64Slice()
	counts.Append(make([]uint64, size-counts.Len())...)

	for i := 0; i < counts.Len(); i++ {
		counts.SetAt(i, a.Abs(lo+i)+b.Abs(lo+i))
	}

	a.SetOffset(int32(lo))
	counts.MoveTo(a.BucketCounts())
}
