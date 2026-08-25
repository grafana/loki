package buffer

import (
	"context"
	"fmt"
	"slices"

	"github.com/grafana/loki/v3/pkg/memory"
)

// MemoryStore is a simple in-memory [Store] backed by a slice. Buffer IDs are
// not reused after deletion.
type MemoryStore struct {
	bufs []Data
}

var _ Store = (*MemoryStore)(nil)

// WriteBuffers clones each [Data] into the store and returns their IDs.
func (s *MemoryStore) WriteBuffers(_ context.Context, data []Data) ([]ID, error) {
	results := make([]ID, len(data))
	for i, d := range data {
		clone := slices.Clone(d)
		if clone == nil {
			clone = Data{} // Normalize nil to empty so Delete can distinguish live vs deleted.
		}
		s.bufs = append(s.bufs, clone)
		results[i] = ID(len(s.bufs))
	}
	return results, nil
}

// ReadBuffers returns the [Data] for the requested buffer IDs.
func (s *MemoryStore) ReadBuffers(_ context.Context, _ *memory.Allocator, bufs []ID) ([]Data, error) {
	res := make([]Data, len(bufs))
	for i, buf := range bufs {
		if err := s.validate(buf); err != nil {
			return nil, err
		}
		res[i] = s.bufs[buf-1]
	}
	return res, nil
}

func (s *MemoryStore) validate(id ID) error {
	if id < 1 || int(id) > len(s.bufs) {
		return fmt.Errorf("invalid buffer ID %d", id)
	} else if s.bufs[id-1] == nil {
		return fmt.Errorf("buffer %d has been deleted", id)
	}
	return nil
}

// BufferSizes returns the byte size of each requested buffer.
func (s *MemoryStore) BufferSizes(_ context.Context, _ *memory.Allocator, bufs []ID) ([]int64, error) {
	sizes := make([]int64, len(bufs))
	for i, buf := range bufs {
		if err := s.validate(buf); err != nil {
			return nil, err
		}
		sizes[i] = int64(len(s.bufs[buf-1]))
	}
	return sizes, nil
}

// Len returns the total number of buffers that have been written, including
// deleted ones.
func (s *MemoryStore) Len() int { return len(s.bufs) }

// Delete nils out the stored data for the given buffer IDs.
func (s *MemoryStore) Delete(_ context.Context, bufs []ID) error {
	for _, buf := range bufs {
		if err := s.validate(buf); err != nil {
			return err
		}
		s.bufs[buf-1] = nil
	}
	return nil
}
