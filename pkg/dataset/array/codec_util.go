package array

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// appendNulls is a utility function that appends null values from the array to
// a Writer.
//
// appendNulls returns an error if any of the following are true:
//   - w is nil and arr has null values.
//   - w is not nil and cannot accept bool values.
//   - arr.Validity() has a length that is not equal to values. (0 means everything is valid).
//
// Returns the number of null values appended and an error, if any.
func appendNulls(alloc *memory.Allocator, w Writer, arr columnar.Array, values int) (int, error) {
	nullCount := arr.Nulls()

	if w == nil {
		if nullCount > 0 {
			return 0, fmt.Errorf("cannot write null values to nil Writer")
		}
		return 0, nil
	}

	validity := arr.Validity()
	switch {
	case validity.Len() == 0:
		// All values are valid. We need to make a temporary bitmap to pass to
		// the validity writer.
		//
		// TODO(rfratto): Can we find a way to avoid this to avoid allocations
		// here?
		shortAlloc := memory.NewAllocator(alloc)
		defer shortAlloc.Free()

		validity = memory.NewBitmap(shortAlloc, values)
		validity.AppendCount(true, values)

		validityArr := columnar.NewBool(validity, memory.Bitmap{})
		return nullCount, w.Append(validityArr)

	case validity.Len() == values:
		// All values are valid. We can pass the existing bitmap directly.
		validityArr := columnar.NewBool(validity, memory.Bitmap{})
		return nullCount, w.Append(validityArr)

	default:
		return nullCount, fmt.Errorf("invalid validity bitmap length: %d (expected 0 or %d)", validity.Len(), values)
	}
}
