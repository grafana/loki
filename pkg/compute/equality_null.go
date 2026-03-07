package compute

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func dispatchNullEquality(alloc *memory.Allocator, left, right columnar.Datum, selection memory.Bitmap) (columnar.Datum, error) {
	_, leftScalar := left.(columnar.Scalar)
	_, rightScalar := right.(columnar.Scalar)

	switch {
	case leftScalar && rightScalar:
		return nullEqualitySS(left.(*columnar.NullScalar), right.(*columnar.NullScalar)), nil
	case leftScalar && !rightScalar:
		return nullEqualitySAA(alloc, left.(*columnar.NullScalar), right.(*columnar.Null), selection)
	case !leftScalar && rightScalar:
		return nullEqualityASA(alloc, left.(*columnar.Null), right.(*columnar.NullScalar), selection)
	case !leftScalar && !rightScalar:
		return nullEqualityAAA(alloc, left.(*columnar.Null), right.(*columnar.Null), selection)
	}

	panic("unreachable")
}

func nullEqualitySS(_, _ *columnar.NullScalar) *columnar.BoolScalar {
	return &columnar.BoolScalar{Null: true}
}

func nullEqualitySAA(alloc *memory.Allocator, _ *columnar.NullScalar, right *columnar.Null, selection memory.Bitmap) (*columnar.Bool, error) {
	validity, err := computeValiditySAA(alloc, true, right.Validity(), selection)
	if err != nil {
		return nil, err
	}

	values := memory.NewBitmap(alloc, right.Len())
	values.AppendCount(false, right.Len())

	return columnar.NewBool(values, validity), nil
}

func nullEqualityASA(alloc *memory.Allocator, left *columnar.Null, _ *columnar.NullScalar, selection memory.Bitmap) (*columnar.Bool, error) {
	validity, err := computeValidityASA(alloc, left.Validity(), true, selection)
	if err != nil {
		return nil, err
	}

	values := memory.NewBitmap(alloc, left.Len())
	values.AppendCount(false, left.Len())

	return columnar.NewBool(values, validity), nil
}

func nullEqualityAAA(alloc *memory.Allocator, left, right *columnar.Null, selection memory.Bitmap) (*columnar.Bool, error) {
	if left.Len() != right.Len() {
		return nil, fmt.Errorf("array length mismatch: %d != %d", left.Len(), right.Len())
	}

	validity, err := computeValidityAAA(alloc, left.Validity(), right.Validity(), selection)
	if err != nil {
		return nil, err
	}

	values := memory.NewBitmap(alloc, left.Len())
	values.AppendCount(false, left.Len())

	return columnar.NewBool(values, validity), nil
}
