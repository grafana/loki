package compute

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// IsMember checks if any of the values in the valueSet Datum are present in the searchData Datum.
func IsMember(alloc *memory.Allocator, datum columnar.Datum, valueSet map[any]struct{}) (columnar.Datum, error) {
	// Inspect the first key in the valueSet to determine the type of the valueSet. This function will panic if the valueSet is not all the same type.
	for k := range valueSet {
		switch k.(type) {
		case string:
			if datum.Kind() != columnar.KindUTF8 {
				return nil, fmt.Errorf("both inputs must be the same type, got %T and %s", k, datum.Kind())
			}
		case int64:
			if datum.Kind() != columnar.KindInt64 {
				return nil, fmt.Errorf("both inputs must be the same type, got %T and %s", k, datum.Kind())
			}
		case uint64:
			if datum.Kind() != columnar.KindUint64 {
				return nil, fmt.Errorf("both inputs must be the same type, got %T and %s", k, datum.Kind())
			}
		default:
			panic("unreachable")
		}
		break
	}

	switch datum.Kind() {
	case columnar.KindUTF8:
		return isMemberUTF8(alloc, datum, valueSet)
	case columnar.KindInt64:
		return isMemberInt64(alloc, datum, valueSet)
	case columnar.KindUint64:
		return isMemberUint64(alloc, datum, valueSet)
	default:
		return nil, fmt.Errorf("unsupported datum type %s", datum.Kind())
	}
}

func isMemberUTF8(alloc *memory.Allocator, datum columnar.Datum, valuesSet map[any]struct{}) (columnar.Datum, error) {
	_, isArray := datum.(columnar.Array)

	switch {
	case isArray:
		return isMemberUTF8AA(alloc, datum.(*columnar.UTF8), valuesSet)
	case !isArray:
		return isMemberUTF8SA(alloc, datum.(*columnar.UTF8Scalar), valuesSet)
	default:
		return nil, fmt.Errorf("unsupported datum type %s", datum.Kind())
	}
}

func isMemberUTF8AA(alloc *memory.Allocator, datum *columnar.UTF8, valueSet map[any]struct{}) (columnar.Datum, error) {
	boolBuilder := columnar.NewBoolBuilder(alloc)
	boolBuilder.Grow(datum.Len())

	for i := range datum.Len() {
		if datum.IsNull(i) {
			boolBuilder.AppendNull()
			continue
		}

		_, found := valueSet[string(datum.Get(i))]
		boolBuilder.AppendValue(found)
	}

	return boolBuilder.Build(), nil
}

func isMemberUTF8SA(_ *memory.Allocator, datum *columnar.UTF8Scalar, valueSet map[any]struct{}) (columnar.Datum, error) {
	if datum.IsNull() {
		return &columnar.BoolScalar{Null: true}, nil
	}

	_, found := valueSet[string(datum.Value)]
	return &columnar.BoolScalar{Value: found}, nil
}

func isMemberInt64(alloc *memory.Allocator, datum columnar.Datum, valueSet map[any]struct{}) (columnar.Datum, error) {
	_, isArray := datum.(columnar.Array)

	switch {
	case isArray:
		return isMemberInt64AA(alloc, datum.(*columnar.Number[int64]), valueSet)
	case !isArray:
		return isMemberInt64SA(alloc, datum.(*columnar.NumberScalar[int64]), valueSet)
	default:
		return nil, fmt.Errorf("unsupported datum type %s", datum.Kind())
	}
}

func isMemberInt64AA(alloc *memory.Allocator, datum *columnar.Number[int64], valueSet map[any]struct{}) (columnar.Datum, error) {
	boolBuilder := columnar.NewBoolBuilder(alloc)
	boolBuilder.Grow(datum.Len())

	for i := range datum.Len() {
		if datum.IsNull(i) {
			boolBuilder.AppendNull()
			continue
		}

		_, found := valueSet[datum.Get(i)]
		boolBuilder.AppendValue(found)
	}

	return boolBuilder.Build(), nil
}

func isMemberInt64SA(_ *memory.Allocator, datum *columnar.NumberScalar[int64], valueSet map[any]struct{}) (columnar.Datum, error) {
	if datum.IsNull() {
		return &columnar.BoolScalar{Null: true}, nil
	}
	_, found := valueSet[datum.Value]
	return &columnar.BoolScalar{Value: found}, nil
}

func isMemberUint64(alloc *memory.Allocator, datum columnar.Datum, valueSet map[any]struct{}) (columnar.Datum, error) {
	_, isArray := datum.(columnar.Array)

	switch {
	case isArray:
		return isMemberUint64AA(alloc, datum.(*columnar.Number[uint64]), valueSet)
	case !isArray:
		return isMemberUint64SA(alloc, datum.(*columnar.NumberScalar[uint64]), valueSet)
	default:
		return nil, fmt.Errorf("unsupported datum type %s", datum.Kind())
	}
}

func isMemberUint64AA(alloc *memory.Allocator, datum *columnar.Number[uint64], valueSet map[any]struct{}) (columnar.Datum, error) {
	boolBuilder := columnar.NewBoolBuilder(alloc)
	boolBuilder.Grow(datum.Len())

	for i := range datum.Len() {
		if datum.IsNull(i) {
			boolBuilder.AppendNull()
			continue
		}

		_, found := valueSet[datum.Get(i)]
		boolBuilder.AppendValue(found)
	}

	return boolBuilder.Build(), nil
}

func isMemberUint64SA(_ *memory.Allocator, datum *columnar.NumberScalar[uint64], valueSet map[any]struct{}) (columnar.Datum, error) {
	if datum.IsNull() {
		return &columnar.BoolScalar{Null: true}, nil
	}
	_, found := valueSet[datum.Value]
	return &columnar.BoolScalar{Value: found}, nil
}
