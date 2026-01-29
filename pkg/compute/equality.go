package compute

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Equals compares the two input datum. Equals returns an error if the input
// kinds are not identical, or if the datum types are not considered comparable.
//
// Special cases:
//
//   - If a null is found on either side, the result is null.
func Equals(alloc *memory.Allocator, left, right columnar.Datum) (columnar.Datum, error) {
	if left.Kind() != right.Kind() {
		return nil, fmt.Errorf("both inputs must be %s, got %s and %s", columnar.KindBool, left.Kind(), right.Kind())
	}

	switch left.Kind() {
	case columnar.KindNull:
		return dispatchNullEquality(alloc, left, right)
	case columnar.KindBool:
		return dispatchBoolEquality(alloc, boolEqualKernel, left, right)
	case columnar.KindInt64:
		return dispatchNumericEquality(alloc, int64EqualKernel, left, right)
	case columnar.KindUint64:
		return dispatchNumericEquality(alloc, uint64EqualKernel, left, right)
	case columnar.KindUTF8:
		return dispatchUTF8Equality(alloc, utf8EqualKernel, left, right)
	default:
		return nil, fmt.Errorf("datum of type %s is not comparable", left.Kind())
	}
}

// NotEquals compares the two input datum for inequality. NotEquals returns an error if the input
// kinds are not identical, or if the datum types are not considered comparable.
//
// Special cases:
//
//   - If a null is found on either side, the result is null.
func NotEquals(alloc *memory.Allocator, left, right columnar.Datum) (columnar.Datum, error) {
	if left.Kind() != right.Kind() {
		return nil, fmt.Errorf("both inputs must be the same kind, got %s and %s", left.Kind(), right.Kind())
	}

	switch left.Kind() {
	case columnar.KindNull:
		return dispatchNullEquality(alloc, left, right)
	case columnar.KindBool:
		return dispatchBoolEquality(alloc, boolNotEqualKernel, left, right)
	case columnar.KindInt64:
		return dispatchNumericEquality(alloc, int64NotEqualKernel, left, right)
	case columnar.KindUint64:
		return dispatchNumericEquality(alloc, uint64NotEqualKernel, left, right)
	case columnar.KindUTF8:
		return dispatchUTF8Equality(alloc, utf8NotEqualKernel, left, right)
	default:
		return nil, fmt.Errorf("datum of type %s is not comparable", left.Kind())
	}
}

// LessThan compares the two input datum for less-than ordering. LessThan returns an error if the input
// kinds are not identical, or if the datum types are not ordered.
//
// Special cases:
//
//   - If a null is found on either side, the result is null.
func LessThan(alloc *memory.Allocator, left, right columnar.Datum) (columnar.Datum, error) {
	if left.Kind() != right.Kind() {
		return nil, fmt.Errorf("both inputs must be the same kind, got %s and %s", left.Kind(), right.Kind())
	}

	switch left.Kind() {
	case columnar.KindNull:
		return dispatchNullEquality(alloc, left, right)
	case columnar.KindInt64:
		return dispatchNumericEquality(alloc, int64LTKernel, left, right)
	case columnar.KindUint64:
		return dispatchNumericEquality(alloc, uint64LTKernel, left, right)
	case columnar.KindUTF8:
		return dispatchUTF8Equality(alloc, utf8LTKernel, left, right)
	default:
		return nil, fmt.Errorf("datum of type %s is not ordered", left.Kind())
	}
}

// LessOrEqual compares the two input datum for less-than-or-equal ordering. LessOrEqual returns an error if the input
// kinds are not identical, or if the datum types are not ordered.
//
// Special cases:
//
//   - If a null is found on either side, the result is null.
func LessOrEqual(alloc *memory.Allocator, left, right columnar.Datum) (columnar.Datum, error) {
	if left.Kind() != right.Kind() {
		return nil, fmt.Errorf("both inputs must be the same kind, got %s and %s", left.Kind(), right.Kind())
	}

	switch left.Kind() {
	case columnar.KindNull:
		return dispatchNullEquality(alloc, left, right)
	case columnar.KindInt64:
		return dispatchNumericEquality(alloc, int64LTEKernel, left, right)
	case columnar.KindUint64:
		return dispatchNumericEquality(alloc, uint64LTEKernel, left, right)
	case columnar.KindUTF8:
		return dispatchUTF8Equality(alloc, utf8LTEKernel, left, right)
	default:
		return nil, fmt.Errorf("datum of type %s is not ordered", left.Kind())
	}
}

// GreaterThan compares the two input datum for greater-than ordering. GreaterThan returns an error if the input
// kinds are not identical, or if the datum types are not ordered.
//
// Special cases:
//
//   - If a null is found on either side, the result is null.
func GreaterThan(alloc *memory.Allocator, left, right columnar.Datum) (columnar.Datum, error) {
	if left.Kind() != right.Kind() {
		return nil, fmt.Errorf("both inputs must be the same kind, got %s and %s", left.Kind(), right.Kind())
	}

	switch left.Kind() {
	case columnar.KindNull:
		return dispatchNullEquality(alloc, left, right)
	case columnar.KindInt64:
		return dispatchNumericEquality(alloc, int64GTKernel, left, right)
	case columnar.KindUint64:
		return dispatchNumericEquality(alloc, uint64GTKernel, left, right)
	case columnar.KindUTF8:
		return dispatchUTF8Equality(alloc, utf8GTKernel, left, right)
	default:
		return nil, fmt.Errorf("datum of type %s is not ordered", left.Kind())
	}
}

// GreaterOrEqual compares the two input datum for greater-than-or-equal ordering. GreaterOrEqual returns an error if the input
// kinds are not identical, or if the datum types are not ordered.
//
// Special cases:
//
//   - If a null is found on either side, the result is null.
func GreaterOrEqual(alloc *memory.Allocator, left, right columnar.Datum) (columnar.Datum, error) {
	if left.Kind() != right.Kind() {
		return nil, fmt.Errorf("both inputs must be the same kind, got %s and %s", left.Kind(), right.Kind())
	}

	switch left.Kind() {
	case columnar.KindNull:
		return dispatchNullEquality(alloc, left, right)
	case columnar.KindInt64:
		return dispatchNumericEquality(alloc, int64GTEKernel, left, right)
	case columnar.KindUint64:
		return dispatchNumericEquality(alloc, uint64GTEKernel, left, right)
	case columnar.KindUTF8:
		return dispatchUTF8Equality(alloc, utf8GTEKernel, left, right)
	default:
		return nil, fmt.Errorf("datum of type %s is not ordered", left.Kind())
	}
}
