package compute

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Filter selects rows from the input datum where the corresponding bit in mask
// is set, returning a new compacted array containing only the selected rows.
//
// The input must be an [columnar.Array]; Filter returns an error if a
// [columnar.Scalar] is provided.
//
// If mask is empty (Len == 0), all rows are selected and the input is returned
// unchanged. Filter returns an error if the mask length does not match the
// input array length.
func Filter(alloc *memory.Allocator, input columnar.Datum, mask memory.Bitmap) (columnar.Datum, error) {
	arr, ok := input.(columnar.Array)
	if !ok {
		return nil, fmt.Errorf("Filter requires an Array input, got %T", input)
	}

	if ok, err := columnar.AllSelected(arr, mask); ok {
		return arr, nil
	} else if err != nil {
		return nil, err
	}

	switch src := arr.(type) {
	case *columnar.Bool:
		return filterBool(alloc, src, mask), nil
	case *columnar.Number[int32]:
		return filterNumber(alloc, src, mask), nil
	case *columnar.Number[int64]:
		return filterNumber(alloc, src, mask), nil
	case *columnar.Number[uint32]:
		return filterNumber(alloc, src, mask), nil
	case *columnar.Number[uint64]:
		return filterNumber(alloc, src, mask), nil
	case *columnar.UTF8:
		return filterUTF8(alloc, src, mask), nil
	case *columnar.Null:
		return filterNull(alloc, mask), nil
	case *columnar.Struct:
		return filterStruct(alloc, src, mask)
	default:
		return nil, fmt.Errorf("Filter: unsupported array type %T", input)
	}
}

func filterBool(alloc *memory.Allocator, src *columnar.Bool, mask memory.Bitmap) *columnar.Bool {
	builder := columnar.NewBoolBuilder(alloc)
	builder.Grow(mask.SetCount())
	for i := range mask.IterValues(true) {
		if src.IsNull(i) {
			builder.AppendNull()
		} else {
			builder.AppendValue(src.Get(i))
		}
	}
	return builder.Build()
}

func filterNumber[T columnar.Numeric](alloc *memory.Allocator, src *columnar.Number[T], mask memory.Bitmap) *columnar.Number[T] {
	builder := columnar.NewNumberBuilder[T](alloc)
	builder.Grow(mask.SetCount())
	for i := range mask.IterValues(true) {
		if src.IsNull(i) {
			builder.AppendNull()
		} else {
			builder.AppendValue(src.Get(i))
		}
	}
	return builder.Build()
}

func filterUTF8(alloc *memory.Allocator, src *columnar.UTF8, mask memory.Bitmap) *columnar.UTF8 {
	n := mask.SetCount()
	builder := columnar.NewUTF8Builder(alloc)
	builder.Grow(n)
	// Estimate data bytes from the source average. A two-pass approach to
	// compute the exact size is ~30% slower due to the extra iteration over
	// variable-length values; the estimate avoids most reallocations without
	// that cost.
	builder.GrowData(src.Size() * n / max(src.Len(), 1))
	for i := range mask.IterValues(true) {
		if src.IsNull(i) {
			builder.AppendNull()
		} else {
			builder.AppendValue(src.Get(i))
		}
	}
	return builder.Build()
}

func filterStruct(alloc *memory.Allocator, src *columnar.Struct, mask memory.Bitmap) (*columnar.Struct, error) {
	fields := make([]columnar.Array, src.NumFields())
	for i := range fields {
		filtered, err := Filter(alloc, src.Field(i), mask)
		if err != nil {
			return nil, fmt.Errorf("filtering struct field %d: %w", i, err)
		}
		fields[i] = filtered.(columnar.Array)
	}

	var validity memory.Bitmap
	if srcValidity := src.Validity(); srcValidity.Len() > 0 {
		vArr := filterBool(alloc, columnar.NewBool(srcValidity, memory.Bitmap{}), mask)
		validity = vArr.Values()
	}
	return columnar.NewStruct(src.Schema(), fields, mask.SetCount(), validity), nil
}

func filterNull(alloc *memory.Allocator, mask memory.Bitmap) *columnar.Null {
	n := mask.SetCount()
	validity := memory.NewBitmap(alloc, n)
	validity.AppendCount(false, n)
	return columnar.NewNull(validity)
}
