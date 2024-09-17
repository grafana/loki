package chunkenc

import (
	"context"
	"io"
	"sort"
	"time"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/util/filter"
)

const (
	tmpNumEntries = 1024
)

// NewDumbChunk returns a new chunk that isn't very good.
func NewDumbChunk() Chunk {
	return &dumbChunk{}
}

type dumbChunk struct {
	entries []logproto.Entry
}

func (c *dumbChunk) Bounds() (time.Time, time.Time) {
	if len(c.entries) == 0 {
		return time.Time{}, time.Time{}
	}
	return c.entries[0].Timestamp, c.entries[len(c.entries)-1].Timestamp
}

func (c *dumbChunk) SpaceFor(_ *logproto.Entry) bool {
	return len(c.entries) < tmpNumEntries
}

// The dumbChunk does not check for duplicates, and will always return false
func (c *dumbChunk) Append(entry *logproto.Entry) (bool, error) {
	if len(c.entries) == tmpNumEntries {
		return false, ErrChunkFull
	}

	if len(c.entries) > 0 && c.entries[len(c.entries)-1].Timestamp.After(entry.Timestamp) {
		return false, ErrOutOfOrder
	}

	c.entries = append(c.entries, *entry)
	return false, nil
}

func (c *dumbChunk) Size() int {
	return len(c.entries)
}

// UncompressedSize implements Chunk.
func (c *dumbChunk) UncompressedSize() int {
	return c.Size()
}

// CompressedSize implements Chunk.
func (c *dumbChunk) CompressedSize() int {
	return 0
}

// Utilization implements Chunk
func (c *dumbChunk) Utilization() float64 {
	return float64(len(c.entries)) / float64(tmpNumEntries)
}

func (c *dumbChunk) Encoding() Encoding { return EncNone }

// Returns an iterator that goes from _most_ recent to _least_ recent (ie,
// backwards).
func (c *dumbChunk) Iterator(_ context.Context, from, through time.Time, direction logproto.Direction, _ log.StreamPipeline) (iter.EntryIterator, error) {
	i := sort.Search(len(c.entries), func(i int) bool {
		return !from.After(c.entries[i].Timestamp)
	})
	j := sort.Search(len(c.entries), func(j int) bool {
		return !through.After(c.entries[j].Timestamp)
	})

	if from == through {
		return nil, nil
	}

	start := -1
	if direction == logproto.BACKWARD {
		start = j - i
	}

	// Take a copy of the entries to avoid locking
	return &dumbChunkIterator{
		direction: direction,
		i:         start,
		entries:   c.entries[i:j],
	}, nil
}

func (c *dumbChunk) SampleIterator(_ context.Context, _, _ time.Time, _ log.StreamSampleExtractor) iter.SampleIterator {
	return nil
}

func (c *dumbChunk) Bytes() ([]byte, error) {
	return nil, nil
}

func (c *dumbChunk) BytesWith(_ []byte) ([]byte, error) {
	return nil, nil
}

func (c *dumbChunk) WriteTo(_ io.Writer) (int64, error) { return 0, nil }

func (c *dumbChunk) Blocks(_ time.Time, _ time.Time) []Block {
	return nil
}

func (c *dumbChunk) BlockCount() int {
	return 0
}

func (c *dumbChunk) Close() error {
	return nil
}

func (c *dumbChunk) Rebound(_, _ time.Time, _ filter.Func) (Chunk, error) {
	return nil, nil
}

type dumbChunkIterator struct {
	direction logproto.Direction
	i         int
	entries   []logproto.Entry
}

func (i *dumbChunkIterator) Next() bool {
	switch i.direction {
	case logproto.BACKWARD:
		i.i--
		return i.i >= 0
	case logproto.FORWARD:
		i.i++
		return i.i < len(i.entries)
	default:
		panic(i.direction)
	}
}

func (i *dumbChunkIterator) At() logproto.Entry {
	return i.entries[i.i]
}

func (i *dumbChunkIterator) Labels() string {
	return ""
}

func (i *dumbChunkIterator) StreamHash() uint64 {
	return 0
}

func (i *dumbChunkIterator) Err() error {
	return nil
}

func (i *dumbChunkIterator) Close() error {
	return nil
}
