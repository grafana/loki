package bloomgateway

import (
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

type IndexedValue[T any] struct {
	idx int
	val T
}

type IterWithIndex[T any] struct {
	v1.Iterator[T]
	zero  T // zero value of T
	cache IndexedValue[T]
}

func (it *IterWithIndex[T]) At() IndexedValue[T] {
	it.cache.val = it.Iterator.At()
	return it.cache
}

func NewIterWithIndex[T any](iter v1.Iterator[T], idx int) v1.Iterator[IndexedValue[T]] {
	return &IterWithIndex[T]{
		Iterator: iter,
		cache:    IndexedValue[T]{idx: idx},
	}
}

type SliceIterWithIndex[T any] struct {
	xs    []T // source slice
	pos   int // position within the slice
	zero  T   // zero value of T
	cache IndexedValue[T]
}

func (it *SliceIterWithIndex[T]) Next() bool {
	it.pos++
	return it.pos < len(it.xs)
}

func (it *SliceIterWithIndex[T]) Err() error {
	return nil
}

func (it *SliceIterWithIndex[T]) At() IndexedValue[T] {
	it.cache.val = it.xs[it.pos]
	return it.cache
}

func (it *SliceIterWithIndex[T]) Peek() (IndexedValue[T], bool) {
	if it.pos+1 >= len(it.xs) {
		it.cache.val = it.zero
		return it.cache, false
	}
	it.cache.val = it.xs[it.pos+1]
	return it.cache, true
}

func NewSliceIterWithIndex[T any](xs []T, idx int) v1.PeekingIterator[IndexedValue[T]] {
	return &SliceIterWithIndex[T]{
		xs:    xs,
		pos:   -1,
		cache: IndexedValue[T]{idx: idx},
	}
}

func getDayTime(ts model.Time) time.Time {
	return time.Date(ts.Time().Year(), ts.Time().Month(), ts.Time().Day(), 0, 0, 0, 0, time.UTC)
}

// TODO(chaudum): Fix Through time calculation
// getFromThrough assumes a list of ShortRefs sorted by From time
// However, it does also assume that the last item has the highest
// Through time, which might not be the case!
func getFromThrough(refs []*logproto.ShortRef) (model.Time, model.Time) {
	if len(refs) == 0 {
		return model.Earliest, model.Latest
	}
	return refs[0].From, refs[len(refs)-1].Through
}

func convertToSearches(filters []*logproto.LineFilterExpression) [][]byte {
	searches := make([][]byte, 0, len(filters))
	for _, f := range filters {
		searches = append(searches, []byte(f.Match))
	}
	return searches
}

// convertToShortRefs converts a v1.ChunkRefs into []*logproto.ShortRef
// TODO(chaudum): Avoid conversion by transferring v1.ChunkRefs in gRPC request.
func convertToShortRefs(refs v1.ChunkRefs) []*logproto.ShortRef {
	result := make([]*logproto.ShortRef, 0, len(refs))
	for _, ref := range refs {
		result = append(result, &logproto.ShortRef{From: ref.Start, Through: ref.End, Checksum: ref.Checksum})
	}
	return result
}

// convertToChunkRefs converts a []*logproto.ShortRef into v1.ChunkRefs
// TODO(chaudum): Avoid conversion by transferring v1.ChunkRefs in gRPC request.
func convertToChunkRefs(refs []*logproto.ShortRef) v1.ChunkRefs {
	result := make(v1.ChunkRefs, 0, len(refs))
	for _, ref := range refs {
		result = append(result, v1.ChunkRef{Start: ref.From, End: ref.Through, Checksum: ref.Checksum})
	}
	return result
}

// getFirstLast returns the first and last item of a fingerprint slice
// It assumes an ascending sorted list of fingerprints.
func getFirstLast[T any](s []T) (T, T) {
	var zero T
	if len(s) == 0 {
		return zero, zero
	}
	return s[0], s[len(s)-1]
}
