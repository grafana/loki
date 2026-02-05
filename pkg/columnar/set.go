package columnar

import "fmt"

type Set struct {
	kind   Kind
	lookup map[any]struct{}
}

func NewUTF8Set(values ...string) *Set {
	set := &Set{kind: KindUTF8, lookup: make(map[any]struct{}, len(values))}
	for _, value := range values {
		set.lookup[value] = struct{}{}
	}
	return set
}

func NewNumberSet[T Numeric](values ...T) *Set {
	var kind Kind
	var zero T
	switch any(zero).(type) {
	case int64:
		kind = KindInt64
	case uint64:
		kind = KindUint64
	default:
		panic(fmt.Sprintf("unsupported type %T", zero))
	}

	set := &Set{kind: kind, lookup: make(map[any]struct{}, len(values))}
	for _, value := range values {
		set.lookup[value] = struct{}{}
	}
	return set
}

func (s *Set) Kind() Kind {
	return s.kind
}

func (s *Set) Has(value any) bool {
	_, ok := s.lookup[value]
	return ok
}
