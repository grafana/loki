package compute

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func dispatchNullEquality(alloc *memory.Allocator, left, right columnar.Datum) (columnar.Datum, error) {
	_, leftScalar := left.(columnar.Scalar)
	_, rightScalar := right.(columnar.Scalar)

	switch {
	case leftScalar && rightScalar:
		return nullEqualitySS(left.(*columnar.NullScalar), right.(*columnar.NullScalar)), nil
	case leftScalar && !rightScalar:
		return nullEqualitySA(alloc, left.(*columnar.NullScalar), right.(*columnar.Null)), nil
	case !leftScalar && rightScalar:
		return nullEqualityAS(alloc, left.(*columnar.Null), right.(*columnar.NullScalar)), nil
	case !leftScalar && !rightScalar:
		return nullEqualityAA(alloc, left.(*columnar.Null), right.(*columnar.Null))
	}

	panic("unreachable")
}

func nullEqualitySS(_, _ *columnar.NullScalar) *columnar.BoolScalar {
	return &columnar.BoolScalar{Null: true}
}

func nullEqualitySA(alloc *memory.Allocator, _ *columnar.NullScalar, right *columnar.Null) *columnar.Bool {
	validity := computeValiditySA(alloc, true, right.Validity())

	values := memory.NewBitmap(alloc, right.Len())
	values.AppendCount(false, right.Len())

	return columnar.NewBool(values, validity)
}

func nullEqualityAS(alloc *memory.Allocator, left *columnar.Null, _ *columnar.NullScalar) *columnar.Bool {
	validity := computeValidityAS(alloc, left.Validity(), true)

	values := memory.NewBitmap(alloc, left.Len())
	values.AppendCount(false, left.Len())

	return columnar.NewBool(values, validity)
}

func nullEqualityAA(alloc *memory.Allocator, left, right *columnar.Null) (*columnar.Bool, error) {
	if left.Len() != right.Len() {
		return nil, fmt.Errorf("array length mismatch: %d != %d", left.Len(), right.Len())
	}

	validity, err := computeValidityAA(alloc, left.Validity(), right.Validity())
	if err != nil {
		return nil, err
	}

	values := memory.NewBitmap(alloc, left.Len())
	values.AppendCount(false, left.Len())

	return columnar.NewBool(values, validity), nil
}
