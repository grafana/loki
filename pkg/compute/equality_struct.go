package compute

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func structEquals(alloc *memory.Allocator, left, right *columnar.Struct, selection memory.Bitmap) (*columnar.Bool, error) {
	if left.Len() != right.Len() {
		return nil, fmt.Errorf("array length mismatch: %d != %d", left.Len(), right.Len())
	}
	if left.NumFields() != right.NumFields() {
		return nil, fmt.Errorf("field count mismatch: %d != %d", left.NumFields(), right.NumFields())
	}
	for i := range left.NumFields() {
		if want, got := left.Schema().Column(i).Name, right.Schema().Column(i).Name; want != got {
			return nil, fmt.Errorf("field %d name mismatch: %q != %q", i, want, got)
		}
	}

	n := left.Len()

	tmpAlloc := memory.NewAllocator(alloc)
	defer tmpAlloc.Free()

	// We start by saying all of the selected elements are equal.
	var result *columnar.Bool
	{
		values := memory.NewBitmap(tmpAlloc, n)
		if selection.Len() == n {
			values.AppendBitmap(selection)
		} else {
			values.AppendCount(true, n)
		}
		result = columnar.NewBool(values, memory.Bitmap{})
	}

	for i := range left.NumFields() {
		// Check the field. The current result is used as the selection mask for
		// the next field.
		fieldResult, err := Equals(tmpAlloc, left.Field(i), right.Field(i), result.Values())
		if err != nil {
			return nil, fmt.Errorf("comparing struct field %d: %w", i, err)
		}
		fieldEquals := fieldResult.(*columnar.Bool)

		andResult, err := And(tmpAlloc, result, fieldEquals, selection)
		if err != nil {
			return nil, fmt.Errorf("intersecting field %d result: %w", i, err)
		}
		result = andResult.(*columnar.Bool)

		if vals := result.Values(); vals.SetCount() == 0 {
			// No matches found, early exit.
			break
		}
	}

	// Compute the final validity by intersecting with the struct-level
	// validity. This allows NULLs from individual fields to propagate to the
	// result.
	var resValues, resValidity memory.Bitmap
	{
		// Clone the result with the real alloc, since the result is allocated
		// with the temporary allocator which will get freed at return.
		srcValues := result.Values()
		resValues = *srcValues.Clone(alloc)
	}
	structValidity, err := computeValidityAA(tmpAlloc, left.Validity(), right.Validity())
	if err != nil {
		return nil, err
	}
	resValidity, err = computeValidityAA(alloc, result.Validity(), structValidity)
	if err != nil {
		return nil, err
	}
	return columnar.NewBool(resValues, resValidity), nil
}

func structNotEquals(alloc *memory.Allocator, left, right *columnar.Struct, selection memory.Bitmap) (*columnar.Bool, error) {
	eq, err := structEquals(alloc, left, right, selection)
	if err != nil {
		return nil, err
	}

	negated, err := Not(alloc, eq, selection)
	if err != nil {
		return nil, err
	}
	return negated.(*columnar.Bool), nil
}
