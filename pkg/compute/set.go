package compute

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// IsMember checks if each item in datum is a member of the values set.
func IsMember(alloc *memory.Allocator, datum columnar.Datum, values *columnar.Set) (columnar.Datum, error) {
	if values.Kind() != datum.Kind() {
		return nil, fmt.Errorf("values set and datum must be the same kind, got %s and %s", values.Kind(), datum.Kind())
	}

	switch datum.Kind() {
	case columnar.KindUTF8:
		return isMemberUTF8(alloc, datum, values)
	case columnar.KindInt64:
		return isMemberNumber[int64](alloc, datum, values)
	case columnar.KindUint64:
		return isMemberNumber[uint64](alloc, datum, values)
	default:
		return nil, fmt.Errorf("unsupported datum type %s", datum.Kind())
	}
}

func isMemberUTF8(alloc *memory.Allocator, datum columnar.Datum, values *columnar.Set) (columnar.Datum, error) {
	_, isArray := datum.(columnar.Array)

	switch {
	case isArray:
		return isMemberUTF8A(alloc, datum.(*columnar.UTF8), values)
	case !isArray:
		return isMemberUTF8S(alloc, datum.(*columnar.UTF8Scalar), values)
	default:
		return nil, fmt.Errorf("unsupported datum type %s", datum.Kind())
	}
}

func isMemberUTF8A(alloc *memory.Allocator, datum *columnar.UTF8, values *columnar.Set) (columnar.Datum, error) {
	boolBuilder := columnar.NewBoolBuilder(alloc)
	boolBuilder.Grow(datum.Len())

	for i := range datum.Len() {
		if datum.IsNull(i) {
			boolBuilder.AppendNull()
			continue
		}

		found := values.Has(string(datum.Get(i)))
		boolBuilder.AppendValue(found)
	}

	return boolBuilder.Build(), nil
}

func isMemberUTF8S(_ *memory.Allocator, datum *columnar.UTF8Scalar, values *columnar.Set) (columnar.Datum, error) {
	if datum.IsNull() {
		return &columnar.BoolScalar{Null: true}, nil
	}

	found := values.Has(string(datum.Value))
	return &columnar.BoolScalar{Value: found}, nil
}

func isMemberNumber[T columnar.Numeric](alloc *memory.Allocator, datum columnar.Datum, values *columnar.Set) (columnar.Datum, error) {
	_, isArray := datum.(columnar.Array)

	switch {
	case isArray:
		return isMemberNumberA(alloc, datum.(*columnar.Number[T]), values)
	case !isArray:
		return isMemberNumberS(alloc, datum.(*columnar.NumberScalar[T]), values)
	default:
		return nil, fmt.Errorf("unsupported datum type %s", datum.Kind())
	}
}

func isMemberNumberA[T columnar.Numeric](alloc *memory.Allocator, datum *columnar.Number[T], values *columnar.Set) (columnar.Datum, error) {
	boolBuilder := columnar.NewBoolBuilder(alloc)
	boolBuilder.Grow(datum.Len())

	for i := range datum.Len() {
		if datum.IsNull(i) {
			boolBuilder.AppendNull()
			continue
		}

		found := values.Has(datum.Get(i))
		boolBuilder.AppendValue(found)
	}

	return boolBuilder.Build(), nil
}

func isMemberNumberS[T columnar.Numeric](_ *memory.Allocator, datum *columnar.NumberScalar[T], values *columnar.Set) (columnar.Datum, error) {
	if datum.IsNull() {
		return &columnar.BoolScalar{Null: true}, nil
	}
	found := values.Has(datum.Value)
	return &columnar.BoolScalar{Value: found}, nil
}
