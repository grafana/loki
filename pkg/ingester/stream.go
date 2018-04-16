package ingester

import (
	"context"

	"github.com/grafana/logish/pkg/logproto"
)

const tmpMaxChunks = 3

type stream struct {
	// Newest chunk at chunks[0].
	// Not thread-safe; assume accesses to this are locked by caller.
	chunks []Chunk
}

func newStream() *stream {
	return &stream{}
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
