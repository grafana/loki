package compute

import (
	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// ApplySelectionToBoolArray applies a selection bitmap to a boolean array,
// marking unselected rows as null in the result.
func ApplySelectionToBoolArray(alloc *memory.Allocator, arr *columnar.Bool, selection memory.Bitmap) (*columnar.Bool, error) {
	if selection.Len() == 0 {
		return arr, nil
	}
	validity, err := computeValidityAA(alloc, arr.Validity(), selection)
	if err != nil {
		return nil, err
	}
	return columnar.NewBool(arr.Values(), validity), nil
}

// ApplySelectionToNumberArray applies a selection bitmap to a numeric array,
// marking unselected rows as null in the result.
func ApplySelectionToNumberArray[T columnar.Numeric](alloc *memory.Allocator, arr *columnar.Number[T], selection memory.Bitmap) (*columnar.Number[T], error) {
	if selection.Len() == 0 {
		return arr, nil
	}
	validity, err := computeValidityAA(alloc, arr.Validity(), selection)
	if err != nil {
		return nil, err
	}
	return columnar.NewNumber(arr.Values(), validity), nil
}

// ApplySelectionToUTF8Array applies a selection bitmap to a UTF8 array,
// marking unselected rows as null in the result.
func ApplySelectionToUTF8Array(alloc *memory.Allocator, arr *columnar.UTF8, selection memory.Bitmap) (*columnar.UTF8, error) {
	if selection.Len() == 0 {
		return arr, nil
	}
	validity, err := computeValidityAA(alloc, arr.Validity(), selection)
	if err != nil {
		return nil, err
	}
	return columnar.NewUTF8(arr.Data(), arr.Offsets(), validity), nil
}
