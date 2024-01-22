package interval

import (
	"fmt"
)

// Insert inserts the given val with the given start and end as the interval key.
// If there's already an interval key entry with the given start and end interval,
// it will be updated with the given val.
//
// Insert returns an InvalidIntervalError if the given end is less than or equal to the given start value.
func (st *SearchTree[V, T]) Insert(start, end T, val V) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	intervl := interval[V, T]{
		start:      start,
		end:        end,
		val:        val,
		allowPoint: st.config.allowIntervalPoint,
	}

	if intervl.isInvalid(st.cmp) {
		return newInvalidIntervalError(intervl)
	}

	st.root = upsert(st.root, intervl, st.cmp)
	st.root.color = black

	return nil
}

func upsert[V, T any](n *node[V, T], intervl interval[V, T], cmp CmpFunc[T]) *node[V, T] {
	if n == nil {
		return newNode(intervl, red)
	}

	switch {
	case intervl.equal(n.interval.start, n.interval.end, cmp):
		n.interval = intervl
	case intervl.less(n.interval.start, n.interval.end, cmp):
		n.left = upsert(n.left, intervl, cmp)
	default:
		n.right = upsert(n.right, intervl, cmp)
	}

	if cmp.gt(intervl.end, n.maxEnd) {
		n.maxEnd = intervl.end
	}

	updateSize(n)

	return balanceNode(n, cmp)
}

// EmptyValueListError is a description of an invalid list of values.
type EmptyValueListError string

// Error returns a string representation of the EmptyValueListError error.
func (e EmptyValueListError) Error() string {
	return string(e)
}

func newEmptyValueListError[V, T any](it interval[V, T], action string) error {
	s := fmt.Sprintf("multi value interval search tree: cannot %s empty value list for interval (%v, %v)", action, it.start, it.end)
	return EmptyValueListError(s)
}

// Insert inserts the given vals with the given start and end as the interval key.
// If there's already an interval key entry with the given start and end interval,
// Insert will append the given vals to the exiting interval key.
//
// Insert returns an InvalidIntervalError if the given end is less than or equal to the given start value,
// or an EmptyValueListError if vals is an empty list.
func (st *MultiValueSearchTree[V, T]) Insert(start, end T, vals ...V) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	intervl := interval[V, T]{
		start:      start,
		end:        end,
		vals:       vals,
		allowPoint: st.config.allowIntervalPoint,
	}

	if intervl.isInvalid(st.cmp) {
		return newInvalidIntervalError(intervl)
	}

	if len(vals) == 0 {
		return newEmptyValueListError(intervl, "insert")
	}

	st.root = insert(st.root, intervl, st.cmp)
	st.root.color = black

	return nil
}

func insert[V, T any](n *node[V, T], intervl interval[V, T], cmp CmpFunc[T]) *node[V, T] {
	if n == nil {
		return newNode(intervl, red)
	}

	switch {
	case intervl.equal(n.interval.start, n.interval.end, cmp):
		n.interval.vals = append(n.interval.vals, intervl.vals...)
	case intervl.less(n.interval.start, n.interval.end, cmp):
		n.left = insert(n.left, intervl, cmp)
	default:
		n.right = insert(n.right, intervl, cmp)
	}

	if cmp.gt(intervl.end, n.maxEnd) {
		n.maxEnd = intervl.end
	}

	updateSize(n)

	return balanceNode(n, cmp)
}

// Upsert inserts the given vals with the given start and end as the interval key.
// If there's already an interval key entry with the given start and end interval,
// it will be updated with the given vals.
//
// Insert returns an InvalidIntervalError if the given end is less than or equal to the given start value,
// or an EmptyValueListError if vals is an empty list.
func (st *MultiValueSearchTree[V, T]) Upsert(start, end T, vals ...V) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	intervl := interval[V, T]{
		start:      start,
		end:        end,
		vals:       vals,
		allowPoint: st.config.allowIntervalPoint,
	}

	if intervl.isInvalid(st.cmp) {
		return newInvalidIntervalError(intervl)
	}

	if len(vals) == 0 {
		return newEmptyValueListError(intervl, "upsert")
	}

	st.root = upsert(st.root, intervl, st.cmp)
	st.root.color = black

	return nil
}
