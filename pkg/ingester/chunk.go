package ingester

import (
	"github.com/grafana/logish/pkg/logproto"
	"github.com/grafana/logish/pkg/querier"
	"github.com/pkg/errors"
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
}

func newChunk() Chunk {
	return &dumbChunk{}
}

type dumbChunk struct {
	entries []logproto.Entry
}

func (c *dumbChunk) SpaceFor(_ *logproto.Entry) bool {
	return len(c.entries) == tmpNumEntries
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

func (c *dumbChunk) Iterator() querier.EntryIterator {
	// Take a copy of the entries to avoid locking
	return &dumbChunkIterator{
		i:       -1,
		entries: c.entries,
	}
}

type dumbChunkIterator struct {
	i       int
	entries []logproto.Entry
}

func (i *dumbChunkIterator) Next() bool {
	return i.i < len(i.entries)
}

func (i *dumbChunkIterator) Entry() logproto.Entry {
	return i.entries[i.i]
}

func (i *dumbChunkIterator) Labels() string {
	return ""
}

func (i *dumbChunkIterator) Error() error {
	return nil
}

func (i *dumbChunkIterator) Close() error {
	return nil
}
