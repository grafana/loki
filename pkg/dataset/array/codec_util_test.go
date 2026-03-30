package array_test

import (
	"context"
	"fmt"
	"slices"

	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/memory"
)

// inMemoryStore holds buffer data in memory, implementing both [array.Source]
// and [array.Sink].
type inMemoryStore struct {
	bufs []array.BufferData
}

var (
	_ array.Source = (*inMemoryStore)(nil)
	_ array.Sink   = (*inMemoryStore)(nil)
)

func (s *inMemoryStore) ReadBuffers(_ context.Context, _ *memory.Allocator, bufs []array.Buffer) ([]array.BufferData, error) {
	res := make([]array.BufferData, len(bufs))
	for i, buf := range bufs {
		if buf.ID < 0 || buf.ID >= int64(len(s.bufs)) {
			return nil, fmt.Errorf("invalid buffer ID: %d", buf.ID)
		}
		res[i] = s.bufs[buf.ID]
	}
	return res, nil
}

func (s *inMemoryStore) WriteBuffers(ctx context.Context, data []array.BufferData) ([]array.Buffer, error) {
	results := make([]array.Buffer, len(data))
	for i, d := range data {
		// We need to make copies of the buffers since it's invalid to retain
		// them after WriteBuffers.
		s.bufs = append(s.bufs, slices.Clone(d))
		results[i].ID = int64(len(s.bufs) - 1)
	}
	return results, nil
}
