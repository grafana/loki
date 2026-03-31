package compute

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// IsMember checks if each item in datum is a member of the values set.
// The selection parameter controls which rows are evaluated:
//   - If selection.Len(), all rows are evaluated
//   - Otherwise, only rows where selection bit is true are evaluated, non-selected rows result in null
func IsMember(alloc *memory.Allocator, datum columnar.Datum, values *columnar.Set, selection memory.Bitmap) (columnar.Datum, error) {
	if values.Kind() != datum.Kind() {
		return nil, fmt.Errorf("values set and datum must be the same kind, got %s and %s", values.Kind(), datum.Kind())
	}

	switch datum.Kind() {
	case columnar.KindUTF8:
		return isMemberUTF8(alloc, datum, values, selection)
	case columnar.KindInt64:
		return isMemberNumber[int64](alloc, datum, values, selection)
	case columnar.KindUint64:
		return isMemberNumber[uint64](alloc, datum, values, selection)
	default:
		return nil, fmt.Errorf("unsupported datum type %s", datum.Kind())
	}
}

func isMemberUTF8(alloc *memory.Allocator, datum columnar.Datum, values *columnar.Set, selection memory.Bitmap) (columnar.Datum, error) {
	_, isArray := datum.(columnar.Array)

	switch {
	case isArray:
		return isMemberUTF8A(alloc, datum.(*columnar.UTF8), values, selection)
	case !isArray:
		return isMemberUTF8S(alloc, datum.(*columnar.UTF8Scalar), values)
	default:
		return nil, fmt.Errorf("unsupported datum type %s", datum.Kind())
	}
}

func isMemberUTF8A(alloc *memory.Allocator, haystack *columnar.UTF8, set *columnar.Set, selection memory.Bitmap) (columnar.Datum, error) {
	validity, err := computeValidityAA(alloc, haystack.Validity(), selection)
	if err != nil {
		return nil, fmt.Errorf("apply selection to validity: %w", err)
	}

	values := memory.NewBitmap(alloc, haystack.Len())
	values.Resize(haystack.Len())

	for i := range iterTrue(validity, haystack.Len()) {
		found := set.Has(string(haystack.Get(i)))
		values.Set(i, found)
	}

	return columnar.NewBool(values, validity), nil
}

func isMemberUTF8S(_ *memory.Allocator, datum *columnar.UTF8Scalar, values *columnar.Set) (columnar.Datum, error) {
	if datum.IsNull() {
		return &columnar.BoolScalar{Null: true}, nil
	}

	found := values.Has(string(datum.Value))
	return &columnar.BoolScalar{Value: found}, nil
}

func isMemberNumber[T columnar.Numeric](alloc *memory.Allocator, datum columnar.Datum, values *columnar.Set, selection memory.Bitmap) (columnar.Datum, error) {
	_, isArray := datum.(columnar.Array)

	switch {
	case isArray:
		return isMemberNumberA(alloc, datum.(*columnar.Number[T]), values, selection)
	case !isArray:
		return isMemberNumberS(alloc, datum.(*columnar.NumberScalar[T]), values)
	default:
		return nil, fmt.Errorf("unsupported datum type %s", datum.Kind())
	}
}

func isMemberNumberA[T columnar.Numeric](alloc *memory.Allocator, haystack *columnar.Number[T], set *columnar.Set, selection memory.Bitmap) (columnar.Datum, error) {
	validity, err := computeValidityAA(alloc, haystack.Validity(), selection)
	if err != nil {
		return nil, fmt.Errorf("apply selection to validity: %w", err)
	}

	values := memory.NewBitmap(alloc, haystack.Len())
	values.Resize(haystack.Len())

	for i := range iterTrue(validity, haystack.Len()) {
		found := set.Has(haystack.Get(i))
		values.Set(i, found)
	}

	return columnar.NewBool(values, validity), nil
}

func isMemberNumberS[T columnar.Numeric](_ *memory.Allocator, datum *columnar.NumberScalar[T], values *columnar.Set) (columnar.Datum, error) {
	if datum.IsNull() {
		return &columnar.BoolScalar{Null: true}, nil
	}
	found := values.Has(datum.Value)
	return &columnar.BoolScalar{Value: found}, nil
}
