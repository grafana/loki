package dataset

import (
	"fmt"
	"iter"
	"unsafe"
)

// Predicate is an expression used to filter rows in a [RowReader].
type Predicate interface{ isPredicate() }

// Suppported predicates..
type (
	// An AndPredicate is a [Predicate] which asserts that a row may only be
	// included if both the Left and Right Predicate are true.
	AndPredicate struct{ Left, Right Predicate }

	// An OrPredicate is a [Predicate] which asserts that a row may only be
	// included if either the Left or Right Predicate are true.
	OrPredicate struct{ Left, Right Predicate }

	// A NotePredicate is a [Predicate] which asserts that a row may only be
	// included if the inner Predicate is false.
	NotPredicate struct{ Inner Predicate }

	// TruePredicate is a [Predicate] which always returns true.
	TruePredicate struct{}

	// FalsePredicate is a [Predicate] which always returns false.
	FalsePredicate struct{}

	// An EqualPredicate is a [Predicate] which asserts that a row may only be
	// included if the Value of the Column is equal to the Value.
	EqualPredicate struct {
		Column Column // Column to check.
		Value  Value  // Value to check equality for.
	}

	// An InPredicate is a [Predicate] which asserts that a row may only be
	// included if the Value of the Column is present in the provided Values.
	InPredicate struct {
		Column Column   // Column to check.
		Values ValueSet // Set of values to check.
	}

	// A GreaterThanPredicate is a [Predicate] which asserts that a row may only
	// be included if the Value of the Column is greater than the provided Value.
	GreaterThanPredicate struct {
		Column Column // Column to check.
		Value  Value  // Value for which rows in Column must be greater than.
	}

	// A LessThanPredicate is a [Predicate] which asserts that a row may only be
	// included if the Value of the Column is less than the provided Value.
	LessThanPredicate struct {
		Column Column // Column to check.
		Value  Value  // Value for which rows in Column must be less than.
	}

	// FuncPredicate is a [Predicate] which asserts that a row may only be
	// included if the Value of the Column passes the Keep function.
	//
	// Instances of FuncPredicate are ineligible for page filtering and should
	// only be used when there isn't a more explicit Predicate implementation.
	FuncPredicate struct {
		Column Column // Column to check.

		// Keep is invoked with the column and value pair to check. Keep is given
		// the Column instance to allow for reusing the same function across
		// multiple columns, if necessary.
		//
		// If Keep returns true, the row is kept.
		Keep func(column Column, value Value) bool
	}
)

// NewConstPredicate returns a [TruePredicate] when keep is true and a [FalsePredicate]
// otherwise.
func NewConstPredicate(keep bool) Predicate {
	if keep {
		return TruePredicate{}
	}
	return FalsePredicate{}
}

// IsConstPredicate reports whether p always evaluates the same way, and what it evaluates
// to. A nil predicate keeps every row.
func IsConstPredicate(p Predicate) (keep, ok bool) {
	switch p.(type) {
	case nil, TruePredicate:
		return true, true
	case FalsePredicate:
		return false, true
	}
	return false, false
}

func (AndPredicate) isPredicate()         {}
func (OrPredicate) isPredicate()          {}
func (NotPredicate) isPredicate()         {}
func (TruePredicate) isPredicate()        {}
func (FalsePredicate) isPredicate()       {}
func (EqualPredicate) isPredicate()       {}
func (InPredicate) isPredicate()          {}
func (GreaterThanPredicate) isPredicate() {}
func (LessThanPredicate) isPredicate()    {}
func (FuncPredicate) isPredicate()        {}

// WalkPredicate traverses a predicate in depth-first order: it starts by
// calling fn(p). If fn(p) returns true, WalkPredicate is invoked recursively
// with fn for each of the non-nil children of p, followed by a call of
// fn(nil).
func WalkPredicate(p Predicate, fn func(p Predicate) bool) {
	if p == nil || !fn(p) {
		return
	}

	switch p := p.(type) {
	case AndPredicate:
		WalkPredicate(p.Left, fn)
		WalkPredicate(p.Right, fn)

	case OrPredicate:
		WalkPredicate(p.Left, fn)
		WalkPredicate(p.Right, fn)

	case NotPredicate:
		WalkPredicate(p.Inner, fn)

	case TruePredicate: // No children.
	case FalsePredicate: // No children.
	case EqualPredicate: // No children.
	case InPredicate: // No children.
	case GreaterThanPredicate: // No children.
	case LessThanPredicate: // No children.
	case FuncPredicate: // No children.

	default:
		panic(fmt.Sprintf("dataset.WalkPredicate: unsupported predicate type %T", p))
	}

	fn(nil)
}

// ValueSet is a set of [Value]s of one physical type.
//
// The methods of a ValueSet are safe for concurrent use, unless the
// implementation says otherwise. [MemoizedInt64Set] is not.
type ValueSet interface {
	Contains(value Value) bool
	Iter() iter.Seq[Value]
	Size() int
}

type Int64Set struct {
	values map[int64]Value
}

func NewInt64ValueSet(values []Value) Int64Set {
	valuesMap := make(map[int64]Value, len(values))
	for _, v := range values {
		valuesMap[v.Int64()] = v
	}
	return Int64Set{
		values: valuesMap,
	}
}

// NewInt64ValueSetOf returns an [Int64Set] that holds the given members. It is
// [NewInt64ValueSet] for callers that have plain int64 values.
func NewInt64ValueSetOf(members ...int64) Int64Set {
	valuesMap := make(map[int64]Value, len(members))
	for _, m := range members {
		valuesMap[m] = Int64Value(m)
	}
	return Int64Set{
		values: valuesMap,
	}
}

func (s Int64Set) Contains(value Value) bool {
	_, ok := s.values[value.Int64()]
	return ok
}

func (s Int64Set) Iter() iter.Seq[Value] {
	return func(yield func(v Value) bool) {
		for _, v := range s.values {
			ok := yield(v)
			if !ok {
				return
			}
		}
	}
}

func (s Int64Set) Size() int {
	return len(s.values)
}

// MemoizedInt64Set is an int64 [ValueSet] that caches the result of its last
// Contains call.
//
// A lookup of the same value as the previous one skips the map access. This
// pays off when lookups come in runs, as in the stream ID column of a logs
// section, which keeps the rows of one stream together. A lookup that changes
// the value costs one extra comparison.
//
// Contains writes the cache without synchronization, so a MemoizedInt64Set is
// not safe for concurrent use. Use one set per reader.
type MemoizedInt64Set struct {
	set Int64Set

	// lastKey and lastResult hold the last answer: lastResult reports whether
	// set contains lastKey. The constructor seeds the pair, so it is a true
	// answer before the first lookup too.
	lastKey    int64
	lastResult bool
}

// NewMemoizedInt64ValueSet returns a [MemoizedInt64Set] that holds values. Use
// [NewInt64ValueSet] when the value set is accessed concurrently or lookups
// rarely repeat.
func NewMemoizedInt64ValueSet(values []Value) *MemoizedInt64Set {
	return newMemoizedInt64Set(NewInt64ValueSet(values))
}

// NewMemoizedInt64ValueSetOf returns a [MemoizedInt64Set] that holds the given
// members. It is [NewMemoizedInt64ValueSet] for callers that have plain int64
// values.
func NewMemoizedInt64ValueSetOf(members ...int64) *MemoizedInt64Set {
	return newMemoizedInt64Set(NewInt64ValueSetOf(members...))
}

func newMemoizedInt64Set(set Int64Set) *MemoizedInt64Set {
	_, seedResult := set.values[0]
	return &MemoizedInt64Set{set: set, lastResult: seedResult}
}

func (s *MemoizedInt64Set) Contains(value Value) bool {
	key := value.Int64()
	if key != s.lastKey {
		_, s.lastResult = s.set.values[key]
		s.lastKey = key
	}
	return s.lastResult
}

func (s *MemoizedInt64Set) Iter() iter.Seq[Value] {
	return s.set.Iter()
}

func (s *MemoizedInt64Set) Size() int {
	return s.set.Size()
}

type Uint64ValueSet struct {
	values map[uint64]Value
}

func NewUint64ValueSet(values []Value) Uint64ValueSet {
	valuesMap := make(map[uint64]Value, len(values))
	for _, v := range values {
		valuesMap[v.Uint64()] = v
	}
	return Uint64ValueSet{
		values: valuesMap,
	}
}

func (s Uint64ValueSet) Contains(value Value) bool {
	_, ok := s.values[value.Uint64()]
	return ok
}

func (s Uint64ValueSet) Iter() iter.Seq[Value] {
	return func(yield func(v Value) bool) {
		for _, v := range s.values {
			ok := yield(v)
			if !ok {
				return
			}
		}
	}
}

func (s Uint64ValueSet) Size() int {
	return len(s.values)
}

type BinaryValueSet struct {
	values map[string]Value
}

func NewBinaryValueSet(values []Value) BinaryValueSet {
	valuesMap := make(map[string]Value, len(values))
	for _, v := range values {
		valuesMap[unsafeString(v.Binary())] = v
	}
	return BinaryValueSet{
		values: valuesMap,
	}
}

func (s BinaryValueSet) Contains(value Value) bool {
	_, ok := s.values[unsafeString(value.Binary())]
	return ok
}

func (s BinaryValueSet) Iter() iter.Seq[Value] {
	return func(yield func(v Value) bool) {
		for _, v := range s.values {
			ok := yield(v)
			if !ok {
				return
			}
		}
	}
}

func (s BinaryValueSet) Size() int {
	return len(s.values)
}

func unsafeString(in []byte) string {
	return unsafe.String(unsafe.SliceData(in), len(in))
}
