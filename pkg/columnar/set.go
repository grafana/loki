package columnar

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar/types"
)

// Set represents a unique set of values of a given kind.
// A Set must be constructed using the New functions in order to ensure that the set is properly initialized and all values in the set are of the correct Kind.
type Set struct {
	kind   types.Kind
	lookup map[any]struct{}
}

// NewUTF8Set creates a new Set of Kind UTF8 initialized with the provided strings.
func NewUTF8Set(values ...string) *Set {
	set := &Set{kind: types.KindUTF8, lookup: make(map[any]struct{}, len(values))}
	for _, value := range values {
		set.lookup[value] = struct{}{}
	}
	return set
}

// NewNumberSet creates a new Set of a numeric Kind T initialized with the provided values.
// The Numeric type T will map to the following Kinds:
//   - int32 -> KindInt32
//   - int64 -> KindInt64
//   - uint32 -> KindUint32
//   - uint64 -> KindUint64
func NewNumberSet[T Numeric](values ...T) *Set {
	var kind types.Kind
	var zero T
	switch any(zero).(type) {
	case int32:
		kind = types.KindInt32
	case int64:
		kind = types.KindInt64
	case uint32:
		kind = types.KindUint32
	case uint64:
		kind = types.KindUint64
	default:
		panic(fmt.Sprintf("unsupported type %T", zero))
	}

	set := &Set{kind: kind, lookup: make(map[any]struct{}, len(values))}
	for _, value := range values {
		set.lookup[value] = struct{}{}
	}
	return set
}

// Kind returns the kind of the values stored in the set.
func (s *Set) Kind() types.Kind {
	return s.kind
}

// Has returns true if the set contains the given value.
// The value must be in the original form provided to the New functions e.g. a string rather than a []byte.
func (s *Set) Has(value any) bool {
	_, ok := s.lookup[value]
	return ok
}
