package columnar

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

func NewInt64Set(values ...int64) *Set {
	set := &Set{kind: KindInt64, lookup: make(map[any]struct{}, len(values))}
	for _, value := range values {
		set.lookup[value] = struct{}{}
	}
	return set
}

func NewUint64Set(values ...uint64) *Set {
	set := &Set{kind: KindUint64, lookup: make(map[any]struct{}, len(values))}
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
