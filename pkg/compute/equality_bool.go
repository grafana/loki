package compute

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func dispatchBoolEquality(alloc *memory.Allocator, kernel boolEqualityKernel, left, right columnar.Datum, selection memory.Bitmap) (columnar.Datum, error) {
	_, leftScalar := left.(columnar.Scalar)
	_, rightScalar := right.(columnar.Scalar)

	switch {
	case leftScalar && rightScalar:
		return boolEqualitySS(kernel, left.(*columnar.BoolScalar), right.(*columnar.BoolScalar)), nil
	case leftScalar && !rightScalar:
		out := boolEqualitySA(alloc, kernel, left.(*columnar.BoolScalar), right.(*columnar.Bool))
		return applySelectionToBoolArray(alloc, out, selection)
	case !leftScalar && rightScalar:
		out := boolEqualityAS(alloc, kernel, left.(*columnar.Bool), right.(*columnar.BoolScalar))
		return applySelectionToBoolArray(alloc, out, selection)
	case !leftScalar && !rightScalar:
		out, err := boolEqualityAA(alloc, kernel, left.(*columnar.Bool), right.(*columnar.Bool))
		if err != nil {
			return nil, err
		}
		return applySelectionToBoolArray(alloc, out, selection)
	}

	panic("unreachable")
}

func boolEqualitySS(kernel boolEqualityKernel, left, right *columnar.BoolScalar) *columnar.BoolScalar {
	return &columnar.BoolScalar{
		Value: kernel.DoSS(left.Value, right.Value),
		Null:  !computeValiditySS(left.Null, right.Null),
	}
}

func boolEqualitySA(alloc *memory.Allocator, kernel boolEqualityKernel, left *columnar.BoolScalar, right *columnar.Bool) *columnar.Bool {
	validity := computeValiditySA(alloc, left.Null, right.Validity())

	if left.Null {
		// When left is null, the result is all nulls (set to the length of
		// right).
		values := memory.NewBitmap(alloc, right.Len())
		values.AppendCount(false, right.Len()) // Append all false to avoid garbage data in results.

		return columnar.NewBool(values, validity)
	}

	values := memory.NewBitmap(alloc, right.Len())
	kernel.DoSA(&values, left.Value, right.Values())

	return columnar.NewBool(values, validity)
}

func boolEqualityAS(alloc *memory.Allocator, kernel boolEqualityKernel, left *columnar.Bool, right *columnar.BoolScalar) *columnar.Bool {
	validity := computeValidityAS(alloc, left.Validity(), right.Null)

	if right.Null {
		// When right is null, the result is all nulls (set to the length of
		// left).
		values := memory.NewBitmap(alloc, left.Len())
		values.AppendCount(false, left.Len()) // Append all false to avoid garbage data in results.

		return columnar.NewBool(values, validity)
	}

	values := memory.NewBitmap(alloc, left.Len())
	kernel.DoAS(&values, left.Values(), right.Value)

	return columnar.NewBool(values, validity)
}

func boolEqualityAA(alloc *memory.Allocator, kernel boolEqualityKernel, left, right *columnar.Bool) (*columnar.Bool, error) {
	if left.Len() != right.Len() {
		return nil, fmt.Errorf("array length mismatch: %d != %d", left.Len(), right.Len())
	}

	validity, err := computeValidityAA(alloc, left.Validity(), right.Validity())
	if err != nil {
		return nil, err
	}

	values := memory.NewBitmap(alloc, left.Len())
	kernel.DoAA(&values, left.Values(), right.Values())

	return columnar.NewBool(values, validity), nil
}
