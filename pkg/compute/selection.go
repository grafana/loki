package compute

import (
	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// applySelectionToBoolArray applies a selection bitmap to a boolean array,
// marking unselected rows as null in the result.
func applySelectionToBoolArray(alloc *memory.Allocator, arr *columnar.Bool, selection memory.Bitmap) (*columnar.Bool, error) {
	if selection.Len() == 0 {
		return arr, nil
	}
	validity, err := computeValidityAA(alloc, arr.Validity(), selection)
	if err != nil {
		return nil, err
	}
	return columnar.NewBool(arr.Values(), validity), nil
}
