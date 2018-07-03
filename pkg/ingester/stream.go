package ingester

import (
	"context"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/logish/pkg/iter"
	"github.com/grafana/logish/pkg/logproto"
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

// Returns an iterator.
func (s *stream) Iterator(from, through time.Time, direction logproto.Direction) iter.EntryIterator {
	iterators := make([]iter.EntryIterator, 0, len(s.chunks))
	for _, c := range s.chunks {
		iter := c.Iterator(from, through, direction)
		if iter != nil {
			iterators = append(iterators, iter)
		}
	}

	if direction == logproto.FORWARD {
		for left, right := 0, len(iterators)-1; left < right; left, right = left+1, right-1 {
			iterators[left], iterators[right] = iterators[right], iterators[left]
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
	iterators []iter.EntryIterator
	curr      iter.EntryIterator
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
