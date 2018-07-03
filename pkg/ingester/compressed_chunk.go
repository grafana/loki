package ingester

import (
	"time"

	"github.com/grafana/logish/pkg/chunkenc"
	"github.com/grafana/logish/pkg/iter"
	"github.com/grafana/logish/pkg/logproto"
)

func newCompressedChunk() Chunk {
	return &compressedChunk{
		chk: chunkenc.NewMemChunk(chunkenc.EncGZIP),
	}
}

type compressedChunk struct {
	chk *chunkenc.MemChunk
}

func (c *compressedChunk) Bounds() (time.Time, time.Time) {
	mint, maxt := c.chk.Bounds()
	return time.Unix(0, mint), time.Unix(0, maxt)
}

func (c *compressedChunk) SpaceFor(entry *logproto.Entry) bool {
	return c.chk.SpaceFor(entry.Timestamp.UnixNano(), entry.Line)
}

func (c *compressedChunk) Push(entry *logproto.Entry) error {
	if !c.SpaceFor(entry) {
		return ErrChunkFull
	}

	if err := c.chk.Append(entry.Timestamp.UnixNano(), entry.Line); err != nil {
		if err == chunkenc.ErrOutOfOrder {
			return ErrOutOfOrder
		}

		return err
	}

	return nil
}

func (c *compressedChunk) Size() int {
	return c.chk.NumSamples()
}

// Returns an iterator that goes from _most_ recent to _least_ recent (ie,
// backwards).
func (c *compressedChunk) Iterator(from, through time.Time, direction logproto.Direction) iter.EntryIterator {
	it, err := c.chk.Iterator(from.UnixNano(), through.UnixNano())
	if err != nil {
		// TODO: Needs error handling.
		return nil
	}

	entryIt := &compressedChunkIteratorForward{it: it}
	if direction == logproto.FORWARD {
		return entryIt
	}

	entries := []logproto.Entry{}
	for entryIt.Next() {
		entries = append(entries, entryIt.Entry())
	}
	// TODO: Handle entryIt.Err()

	return &entryIteratorBackward{entries: entries}
}

type compressedChunkIteratorForward struct {
	it chunkenc.Iterator
}

func (i *compressedChunkIteratorForward) Next() bool {
	return i.it.Next()
}

func (i *compressedChunkIteratorForward) Entry() logproto.Entry {
	ts, line := i.it.At()
	return logproto.Entry{
		Timestamp: time.Unix(0, ts),
		Line:      line,
	}
}

func (i *compressedChunkIteratorForward) Labels() string {
	panic("Labels() called on chunk iterator")
}

func (i *compressedChunkIteratorForward) Error() error {
	return i.it.Err()
}

func (i *compressedChunkIteratorForward) Close() error {
	return nil
}

type entryIteratorBackward struct {
	cur     logproto.Entry
	entries []logproto.Entry
}

func (i *entryIteratorBackward) Next() bool {
	if len(i.entries) == 0 {
		return false
	}

	i.cur = i.entries[len(i.entries)-1]
	i.entries = i.entries[:len(i.entries)-1]

	return true
}

func (i *entryIteratorBackward) Entry() logproto.Entry {
	return i.cur
}

func (i *entryIteratorBackward) Close() error { return nil }

func (i *entryIteratorBackward) Error() error { return nil }

func (i *entryIteratorBackward) Labels() string {
	panic("Labels() called on chunk iterator")
}
