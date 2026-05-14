package layout

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

// datumToMask converts a boolean Datum (either a Bool array or a BoolScalar)
// into a memory.Bitmap of the given length.
//
// For a Bool array, the values bitmap is returned, masked by validity so that
// null rows do not pass. For a BoolScalar, the scalar value is broadcast to
// fill the entire length.
func datumToMask(alloc *memory.Allocator, datum columnar.Datum, length int) (memory.Bitmap, error) {
	switch v := datum.(type) {
	case *columnar.Bool:
		// Compute kernels write a result bit for every row, including nulls
		// (backed by a zero value). Intersect with validity so null rows are
		// excluded from the mask regardless of the underlying values bit.
		validity := v.Validity()
		if validity.Len() == 0 {
			return v.Values(), nil
		}
		return intersectMasks(alloc, v.Values(), validity), nil
	case *columnar.BoolScalar:
		mask := memory.NewBitmap(alloc, length)
		mask.AppendCount(!v.Null && v.Value, length)
		return mask, nil
	default:
		return memory.Bitmap{}, fmt.Errorf("expected Bool datum, got %T", datum)
	}
}

// intersectMasks returns a new mask where only bits set in both a and b are
// set.
func intersectMasks(alloc *memory.Allocator, a, b memory.Bitmap) memory.Bitmap {
	// We wrap the bitmaps as Bool arrays to delegate to compute.And, which
	// correctly handles byte-aligned offsets from sliced bitmaps.
	var (
		aArr = columnar.NewBool(a, memory.Bitmap{})
		bArr = columnar.NewBool(b, memory.Bitmap{})
	)
	result, err := compute.And(alloc, aArr, bArr, memory.Bitmap{})
	if err != nil {
		panic("intersectMasks: " + err.Error())
	}
	return result.(*columnar.Bool).Values()
}
