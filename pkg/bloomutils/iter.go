package bloomutils

import (
	"io"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

// sortMergeIterator implements v1.Iterator
type sortMergeIterator[T any, C comparable, R any] struct {
	curr      *R
	heap      *v1.HeapIterator[v1.IndexedValue[C]]
	items     []T
	transform func(T, C, *R) *R
	err       error
}

func (it *sortMergeIterator[T, C, R]) Next() bool {
	ok := it.heap.Next()
	if !ok {
		it.err = io.EOF
		return false
	}

	group := it.heap.At()
	it.curr = it.transform(it.items[group.Index()], group.Value(), it.curr)

	return true
}

func (it *sortMergeIterator[T, C, R]) At() R {
	return *it.curr
}

func (it *sortMergeIterator[T, C, R]) Err() error {
	return it.err
}
