package bloomgateway

import (
	"sync"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/prometheus/common/model"
)

// SliceIterWithIndex implements v1.PeekingIterator
type SliceIterWithIndex[T any] struct {
	xs   []T // source slice
	pos  int // position within the slice
	idx  int // the index that identifies the iterator
	zero T   // zero value of T
}

func (it *SliceIterWithIndex[T]) Next() bool {
	it.pos++
	return it.pos < len(it.xs)
}

func (it *SliceIterWithIndex[T]) Err() error {
	return nil
}

func (it *SliceIterWithIndex[T]) At() T {
	return it.xs[it.pos]
}

func (it *SliceIterWithIndex[T]) Peek() (T, bool) {
	if it.pos+1 >= len(it.xs) {
		return it.zero, false
	}
	return it.xs[it.pos+1], true
}

func (it *SliceIterWithIndex[T]) Index() int {
	return it.idx
}

func NewIterWithIndex[T any](i int, xs []T) *SliceIterWithIndex[T] {
	return &SliceIterWithIndex[T]{
		xs:  xs,
		pos: -1,
		idx: i,
	}
}

type RequestPool struct {
	sync.Pool
}

func (p *RequestPool) Get() []v1.Request {
	return p.Pool.Get().([]v1.Request)
}

func (p *RequestPool) Put(r []v1.Request) {
	p.Pool.Put(r[:0]) // nolint:staticcheck
}

func getDay(ts model.Time) int64 {
	return ts.Unix() / int64(24*time.Hour/time.Second)
}

func getDayTime(ts model.Time) time.Time {
	return time.Date(ts.Time().Year(), ts.Time().Month(), ts.Time().Day(), 0, 0, 0, 0, time.UTC)
}

func filterRequestForDay(r *logproto.FilterChunkRefRequest, day time.Time) *logproto.FilterChunkRefRequest {
	through := model.TimeFromUnix(day.Unix())
	from := model.TimeFromUnix(day.Add(24 * time.Hour).Unix())

	refs := make([]*logproto.GroupedChunkRefs, 0, len(r.Refs))
	for i := range r.Refs {
		groupedChunkRefs := &logproto.GroupedChunkRefs{
			Fingerprint: r.Refs[i].Fingerprint,
			Tenant:      r.Refs[i].Tenant,
			Refs:        make([]*logproto.ShortRef, 0, len(r.Refs[i].Refs)),
		}
		for j := range r.Refs[i].Refs {
			shortRef := r.Refs[i].Refs[j]
			fromDay := getDayTime(shortRef.From)
			if fromDay.After(day) {
				break
			}
			throughDay := getDayTime(shortRef.Through)
			if fromDay.Equal(day) || throughDay.Equal(day) {
				groupedChunkRefs.Refs = append(groupedChunkRefs.Refs, shortRef)
			}
		}

		// do not add empty groups to request
		if len(groupedChunkRefs.Refs) == 0 {
			continue
		}

		groupFrom, groupThrough := getFromThrough(groupedChunkRefs.Refs)
		if groupFrom.Before(from) {
			from = groupFrom
		}
		if groupThrough.After(through) {
			through = groupThrough
		}
		refs = append(refs, groupedChunkRefs)
	}

	// The initial value of `from` is the through time and vice versa.
	// This is, in order to determine min From and max Through.
	// In case no chunk refs match, we need to swap the initial value again.
	if len(refs) == 0 {
		from, through = through, from
	}

	return &logproto.FilterChunkRefRequest{
		From:    from,
		Through: through,
		Refs:    refs,
		Filters: r.Filters,
	}
}

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
