package compute

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func dispatchUTF8Equality(alloc *memory.Allocator, kernel utf8EqualityKernel, left, right columnar.Datum) (columnar.Datum, error) {
	_, leftScalar := left.(columnar.Scalar)
	_, rightScalar := right.(columnar.Scalar)

	switch {
	case leftScalar && rightScalar:
		return utf8EqualitySS(kernel, left.(*columnar.UTF8Scalar), right.(*columnar.UTF8Scalar)), nil
	case leftScalar && !rightScalar:
		return utf8EqualitySA(alloc, kernel, left.(*columnar.UTF8Scalar), right.(*columnar.UTF8)), nil
	case !leftScalar && rightScalar:
		return utf8EqualityAS(alloc, kernel, left.(*columnar.UTF8), right.(*columnar.UTF8Scalar)), nil
	case !leftScalar && !rightScalar:
		return utf8EqualityAA(alloc, kernel, left.(*columnar.UTF8), right.(*columnar.UTF8))
	}

	panic("unreachable")
}

func utf8EqualitySS(kernel utf8EqualityKernel, left, right *columnar.UTF8Scalar) *columnar.BoolScalar {
	return &columnar.BoolScalar{
		Value: kernel.DoSS(left.Value, right.Value),
		Null:  !computeValiditySS(left.Null, right.Null),
	}
}

func utf8EqualitySA(alloc *memory.Allocator, kernel utf8EqualityKernel, left *columnar.UTF8Scalar, right *columnar.UTF8) *columnar.Bool {
	validity := computeValiditySA(alloc, left.Null, right.Validity())

	if left.Null {
		// When left is null, the result is all nulls (set to the length of
		// right).
		values := memory.NewBitmap(alloc, right.Len())
		values.AppendCount(false, right.Len()) // Append all false to avoid garbage data in results.

		return columnar.NewBool(values, validity)
	}

	values := memory.NewBitmap(alloc, right.Len())
	kernel.DoSA(&values, left.Value, right)

	return columnar.NewBool(values, validity)
}

func utf8EqualityAS(alloc *memory.Allocator, kernel utf8EqualityKernel, left *columnar.UTF8, right *columnar.UTF8Scalar) *columnar.Bool {
	validity := computeValidityAS(alloc, left.Validity(), right.Null)

	if right.Null {
		// When right is null, the result is all nulls (set to the length of
		// left).
		values := memory.NewBitmap(alloc, left.Len())
		values.AppendCount(false, left.Len()) // Append all false to avoid garbage data in results.

		return columnar.NewBool(values, validity)
	}

	values := memory.NewBitmap(alloc, left.Len())
	kernel.DoAS(&values, left, right.Value)

	return columnar.NewBool(values, validity)
}

func utf8EqualityAA(alloc *memory.Allocator, kernel utf8EqualityKernel, left, right *columnar.UTF8) (*columnar.Bool, error) {
	if left.Len() != right.Len() {
		return nil, fmt.Errorf("array length mismatch: %d != %d", left.Len(), right.Len())
	}

	validity, err := computeValidityAA(alloc, left.Validity(), right.Validity())
	if err != nil {
		return nil, err
	}

	values := memory.NewBitmap(alloc, left.Len())
	kernel.DoAA(&values, left, right)

	return columnar.NewBool(values, validity), nil
}
