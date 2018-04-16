package ingester

import (
	"github.com/grafana/logish/pkg/logproto"
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
}

func newChunk() Chunk {
	return &dumbChunk{}
}

type dumbChunk struct {
	entries []*logproto.Entry
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

	c.entries = append(c.entries, entry)
	return nil
}
