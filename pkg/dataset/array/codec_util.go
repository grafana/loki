package array

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// validateNulls checks whether arr's null configuration is compatible with w,
// without performing any mutations. It is intended to be called before
// [appendNulls] so callers can reject invalid input before mutating their own
// state.
func validateNulls(w Writer, arr columnar.Array, values int) error {
	if w == nil && arr.Nulls() > 0 {
		return fmt.Errorf("cannot write null values to nil Writer")
	}

	validity := arr.Validity()
	if validity.Len() != 0 && validity.Len() != values {
		return fmt.Errorf("invalid validity bitmap length: %d (expected 0 or %d)", validity.Len(), values)
	}
	return nil
}

// appendNulls is a utility function that appends null values from the array to
// a Writer. Callers must have validated the input via [validateNulls] first;
// appendNulls assumes the null configuration is well-formed.
//
// Returns the number of null values appended and an error, if any.
func appendNulls(alloc *memory.Allocator, w Writer, arr columnar.Array, values int) (int, error) {
	nullCount := arr.Nulls()

	if w == nil {
		return 0, nil
	}

	validity := arr.Validity()
	if validity.Len() == 0 {
		// All values are valid. We need to make a temporary bitmap to pass to
		// the validity writer.
		//
		// TODO(rfratto): Can we find a way to avoid this to avoid allocations
		// here?
		shortAlloc := memory.NewAllocator(alloc)
		defer shortAlloc.Free()

		validity = memory.NewBitmap(shortAlloc, values)
		validity.AppendCount(true, values)
	}

	validityArr := columnar.NewBool(validity, memory.Bitmap{})
	return nullCount, w.Append(validityArr)
}
