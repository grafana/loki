package ingester

import (
	"github.com/pkg/errors"

	"github.com/grafana/logish/pkg/logproto"
	"github.com/grafana/logish/pkg/querier"
)

const (
	tmpNumEntries = 1024
)

var (
	ErrChunkFull  = errors.New("Chunk full")
	ErrOutOfOrder = errors.New("Entry out of order")
)

type Chunk interface {
	SpaceFor(*logproto.Entry) bool
	Push(*logproto.Entry) error
	Iterator() querier.EntryIterator
	Size() int
}

func newChunk() Chunk {
	return &dumbChunk{}
}

type dumbChunk struct {
	entries []logproto.Entry
}

func (c *dumbChunk) SpaceFor(_ *logproto.Entry) bool {
	return len(c.entries) < tmpNumEntries
}

func (c *dumbChunk) Push(entry *logproto.Entry) error {
	if len(c.entries) == tmpNumEntries {
		return ErrChunkFull
	}

	if len(c.entries) > 0 && c.entries[len(c.entries)-1].Timestamp.After(entry.Timestamp) {
		return ErrOutOfOrder
	}

	c.entries = append(c.entries, *entry)
	return nil
}

func (c *dumbChunk) Size() int {
	return len(c.entries)
}

// Returns an iterator that goes from _most_ recent to _least_ recent (ie,
// backwards).
func (c *dumbChunk) Iterator() querier.EntryIterator {
	// Take a copy of the entries to avoid locking
	return &dumbChunkIterator{
		i:       len(c.entries),
		entries: c.entries,
	}
}

type dumbChunkIterator struct {
	i       int
	entries []logproto.Entry
}

func (i *dumbChunkIterator) Next() bool {
	i.i--
	return i.i >= 0
}

func (i *dumbChunkIterator) Entry() logproto.Entry {
	return i.entries[i.i]
}

func (i *dumbChunkIterator) Labels() string {
	panic("Labels() called on chunk iterator")
}

func (i *dumbChunkIterator) Error() error {
	return nil
}

func (i *dumbChunkIterator) Close() error {
	return nil
}
