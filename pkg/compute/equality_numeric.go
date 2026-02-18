package compute

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func dispatchNumericEquality[T columnar.Numeric](alloc *memory.Allocator, kernel numericEqualityKernel[T], left, right columnar.Datum, selection memory.Bitmap) (columnar.Datum, error) {
	_, leftScalar := left.(columnar.Scalar)
	_, rightScalar := right.(columnar.Scalar)

	switch {
	case leftScalar && rightScalar:
		return numericEqualitySS(kernel, left.(*columnar.NumberScalar[T]), right.(*columnar.NumberScalar[T])), nil
	case leftScalar && !rightScalar:
		out := numericEqualitySA(alloc, kernel, left.(*columnar.NumberScalar[T]), right.(*columnar.Number[T]))
		return applySelectionToBoolArray(alloc, out, selection)
	case !leftScalar && rightScalar:
		out := numericEqualityAS(alloc, kernel, left.(*columnar.Number[T]), right.(*columnar.NumberScalar[T]))
		return applySelectionToBoolArray(alloc, out, selection)
	case !leftScalar && !rightScalar:
		out, err := numericEqualityAA(alloc, kernel, left.(*columnar.Number[T]), right.(*columnar.Number[T]))
		if err != nil {
			return nil, err
		}
		return applySelectionToBoolArray(alloc, out, selection)
	}

	panic("unreachable")
}

func numericEqualitySS[T columnar.Numeric](kernel numericEqualityKernel[T], left, right *columnar.NumberScalar[T]) *columnar.BoolScalar {
	return &columnar.BoolScalar{
		Value: kernel.DoSS(left.Value, right.Value),
		Null:  !computeValiditySS(left.Null, right.Null),
	}
}

func numericEqualitySA[T columnar.Numeric](alloc *memory.Allocator, kernel numericEqualityKernel[T], left *columnar.NumberScalar[T], right *columnar.Number[T]) *columnar.Bool {
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

func numericEqualityAS[T columnar.Numeric](alloc *memory.Allocator, kernel numericEqualityKernel[T], left *columnar.Number[T], right *columnar.NumberScalar[T]) *columnar.Bool {
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

func numericEqualityAA[T columnar.Numeric](alloc *memory.Allocator, kernel numericEqualityKernel[T], left, right *columnar.Number[T]) (*columnar.Bool, error) {
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
