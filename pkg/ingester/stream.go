package ingester

import (
	"context"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/logish/pkg/logproto"
	"github.com/grafana/logish/pkg/querier"
)

const tmpMaxChunks = 3

type stream struct {
	// Newest chunk at chunks[0].
	// Not thread-safe; assume accesses to this are locked by caller.
	chunks []Chunk
	labels labels.Labels
}

func newStream(labels labels.Labels) *stream {
	return &stream{
		labels: labels,
	}
}

func (s *stream) Push(ctx context.Context, entries []logproto.Entry) error {
	if len(s.chunks) == 0 {
		s.chunks = append(s.chunks, newChunk())
	}

	for i := range entries {
		if !s.chunks[0].SpaceFor(&entries[i]) {
			s.chunks = append([]Chunk{newChunk()}, s.chunks...)
		}
		if err := s.chunks[0].Push(&entries[i]); err != nil {
			return err
		}
	}

	// Temp; until we implement flushing, only keep N chunks in memory.
	if len(s.chunks) > tmpMaxChunks {
		s.chunks = s.chunks[:tmpMaxChunks]
	}
	return nil
}

// Returns an iterator that goes from _most_ recent to _least_ recent (ie,
// backwards).
func (s *stream) Iterator(from, through time.Time, direction logproto.Direction) querier.EntryIterator {
	iterators := make([]querier.EntryIterator, len(s.chunks))
	for i, c := range s.chunks {
		switch direction {
		case logproto.FORWARD:
			iterators[len(s.chunks)-i-1] = c.Iterator(from, through, direction)
		case logproto.BACKWARD:
			iterators[i] = c.Iterator(from, through, direction)
		default:
			panic(direction)
		}
	}

	return &nonOverlappingIterator{
		labels:    s.labels.String(),
		iterators: iterators,
	}
}

type nonOverlappingIterator struct {
	labels    string
	i         int
	iterators []querier.EntryIterator
	curr      querier.EntryIterator
}

func (i *nonOverlappingIterator) Next() bool {
	for i.curr == nil || !i.curr.Next() {
		if i.i >= len(i.iterators) {
			return false
		}

		i.curr = i.iterators[i.i]
		i.i++
	}

	return true
}

func (i *nonOverlappingIterator) Entry() logproto.Entry {
	return i.curr.Entry()
}

func (i *nonOverlappingIterator) Labels() string {
	return i.labels
}

func (i *nonOverlappingIterator) Error() error {
	return nil
}

func (i *nonOverlappingIterator) Close() error {
	return nil
}
