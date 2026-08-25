package columnar

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/memory"
)

type invalidMaskError struct {
	maskLength  int
	inputLength int
}

func (e *invalidMaskError) Error() string {
	return fmt.Sprintf("mask length %d does not match input length %d", e.maskLength, e.inputLength)
}

// AllSelected returns true if all elements in the array are selected by the
// mask, or if the mask is the zero value.
//
// If the mask is non-zero, it must have the same length as the array.
func AllSelected(arr Array, mask memory.Bitmap) (bool, error) {
	if mask.Len() == 0 {
		return true, nil
	}
	if mask.Len() != arr.Len() {
		return false, &invalidMaskError{maskLength: mask.Len(), inputLength: arr.Len()}
	}
	return mask.SetCount() == arr.Len(), nil
}
