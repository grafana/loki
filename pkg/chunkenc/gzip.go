package chunkenc

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/pkg/errors"
)

// MemChunk implements compressed log chunks.
type MemChunk struct {
	// The number of uncompressed bytes per block.
	blockSize int

	// The finished blocks.
	blocks     []block
	sync.Mutex // Acquire the lock before modifying blocks.

	// Current in-mem block being appended to.
	memBlock *block

	app *memAppender

	encoding Encoding
	cw       func(w io.Writer) CompressionWriter
	cr       func(r io.Reader) (CompressionReader, error)
}

type block struct {
	// This is compressed bytes.
	b  []byte
	ts []int64

	// This is the list of raw entries.
	// This is cleared in finished blocks.
	entries    []entry
	numEntries int
	size       int // size of uncompressed bytes.

	mint, maxt int64
}

type entry struct {
	t int64
	s string
}

// NewMemChunk returns a new in-mem chunk.
func NewMemChunk(enc Encoding) *MemChunk {
	c := &MemChunk{
		blockSize: 5 * 1024 * 1024, // The blockSize in bytes.
		blocks:    []block{},

		memBlock: &block{
			mint: math.MaxInt64,
			maxt: math.MinInt64,
		},

		encoding: enc,
	}

	switch enc {
	case EncGZIP:
		c.cw = func(w io.Writer) CompressionWriter { return gzip.NewWriter(w) }
		c.cr = func(r io.Reader) (CompressionReader, error) { return gzip.NewReader(r) }
	default:
		panic("unknown encoding")
	}

	c.app = newMemAppender(c)

	return c
}

// Bytes implements Chunk.
func (c *MemChunk) Bytes() []byte {
	if c.app != nil {
		// When generating the bytes, we need to flush the data held in-buffer.
		c.app.cut()
	}

	c.Lock()
	defer c.Unlock()

	l := 0
	for _, b := range c.blocks {
		l += len(b.b)
	}

	totBytes := make([]byte, l)
	off := 0
	for _, b := range c.blocks {
		n := copy(totBytes[off:], b.b)
		off += n
	}

	return totBytes[:off]
}

// Encoding implements Chunk.
func (c *MemChunk) Encoding() Encoding {
	return c.encoding
}

// Appender implements Chunk.
func (c *MemChunk) Appender() (Appender, error) {
	return c.app, nil
}

// NumSamples implements Chunk.
func (c *MemChunk) NumSamples() int {
	c.Lock()
	defer c.Unlock()

	ne := 0
	for _, blk := range c.blocks {
		ne += blk.numEntries
	}

	ne += c.memBlock.numEntries

	return ne
}

// Close implements Chunk.
// TODO: Fix this to check edge cases.
func (c *MemChunk) Close() error {
	return c.app.Close()
}

type memAppender struct {
	// chunk's in-mem block.
	block *block

	chunk *MemChunk

	writer CompressionWriter
	buffer *bytes.Buffer

	encBuf []byte
}

func newMemAppender(chunk *MemChunk) *memAppender {
	buf := bytes.NewBuffer(chunk.memBlock.b)
	return &memAppender{
		block: chunk.memBlock,
		chunk: chunk,

		buffer: buf,
		writer: chunk.cw(buf),

		encBuf: make([]byte, binary.MaxVarintLen64),
	}
}

func (a *memAppender) Append(t int64, s string) error {
	if t < a.block.maxt {
		return errors.New("out of order sample")
	}

	_, err := a.writer.Write([]byte(s + "\n"))
	if err != nil {
		return errors.Wrap(err, "appending line")
	}
	a.block.ts = append(a.block.ts, t)
	a.block.size += len(s)

	a.block.entries = append(a.block.entries, entry{t, s})

	if a.block.mint > t {
		a.block.mint = t
	}
	a.block.maxt = t

	a.block.numEntries++

	if a.block.size > a.chunk.blockSize {
		return a.cut()
	}

	return nil
}

// cut a new block and add it to finished blocks.
func (a *memAppender) cut() error {
	a.chunk.Lock()
	defer a.chunk.Unlock()

	err := a.writer.Close()
	if err != nil {
		return errors.Wrap(err, "closing writer")
	}
	a.block.b = a.buffer.Bytes()

	b := make([]byte, len(a.block.b))
	copy(b, a.block.b)
	ts := make([]int64, len(a.block.ts))
	copy(ts, a.block.ts)

	a.chunk.blocks = append(a.chunk.blocks, block{
		b:          b,
		ts:         ts,
		numEntries: a.block.numEntries,
		mint:       a.block.mint,
		maxt:       a.block.maxt,
		size:       a.block.size,
	})

	// Reset the block.
	a.block.entries = a.block.entries[:0]
	a.block.b = a.block.b[:0]
	a.block.ts = a.block.ts[:0]
	a.block.mint = a.block.maxt
	a.block.numEntries = 0
	a.block.size = 0

	a.buffer = bytes.NewBuffer(a.block.b)
	a.writer = a.chunk.cw(a.buffer)

	return nil
}

func (a *memAppender) Close() error {
	a.chunk.Lock()
	defer a.chunk.Unlock()

	a.writer.Close()

	a.block.entries = nil
	a.block.b = a.buffer.Bytes()

	b := make([]byte, len(a.block.b))
	copy(b, a.block.b)
	a.chunk.blocks = append(a.chunk.blocks, block{
		b:          b,
		numEntries: a.block.numEntries,
		mint:       a.block.mint,
		maxt:       a.block.maxt,
		size:       a.block.size,
	})

	return nil
}

// Iterator implements Chunk.
// TODO: Recheck this.
func (c *MemChunk) Iterator() Iterator {
	return newMemIterator(append(c.blocks, *c.memBlock), c.cr)
}

type memIterator struct {
	blocks []block

	it  Iterator
	cur entry

	cr func(io.Reader) (CompressionReader, error)

	err error
}

func newMemIterator(blocks []block, cr func(io.Reader) (CompressionReader, error)) *memIterator {
	// TODO: Handle nil blocks.
	it, err := newBlockIterator(blocks[0], cr)
	if err != nil {
		fmt.Println("err", err)
	}
	return &memIterator{
		blocks: blocks[1:],
		it:     it,

		cr: cr,
	}
}

func (gi *memIterator) Seek(int64) bool {
	return false
}

func (gi *memIterator) Next() bool {
	if gi.it.Next() {
		gi.cur.t, gi.cur.s = gi.it.At()
		return true
	}

	if len(gi.blocks) == 0 {
		return false
	}

	var err error
	gi.it, err = newBlockIterator(gi.blocks[0], gi.cr)
	if err != nil {
		gi.err = err
	}
	gi.blocks = gi.blocks[1:]

	return gi.Next()
}

func (gi *memIterator) At() (int64, string) {
	return gi.cur.t, gi.cur.s
}

func (gi *memIterator) Err() error {
	return gi.err
}

func newBlockIterator(b block, cr func(io.Reader) (CompressionReader, error)) (Iterator, error) {
	if len(b.entries) > 0 {
		// This means the block is an in-mem block and we can use the entries.

		// TODO: Race!!!
		return &listIterator{
			entries: b.entries[:],
		}, nil
	}

	r, err := cr(bytes.NewBuffer(b.b))
	if err != nil {
		return nil, err
	}

	s := bufio.NewScanner(r)
	return newScanIterator(s, b.ts), nil
}

type listIterator struct {
	entries []entry

	cur entry
}

func (li *listIterator) Seek(int64) bool {
	return false
}

func (li *listIterator) Next() bool {
	if len(li.entries) > 0 {
		li.cur = li.entries[0]
		li.entries = li.entries[1:]

		return true
	}

	return false
}

func (li *listIterator) At() (int64, string) {
	return li.cur.t, li.cur.s
}

func (li *listIterator) Err() error {
	return nil
}

type scanIterator struct {
	s  *bufio.Scanner
	ts []int64

	curT int64
}

func newScanIterator(s *bufio.Scanner, ts []int64) *scanIterator {
	return &scanIterator{
		s:  s,
		ts: ts,
	}
}

func (si *scanIterator) Seek(int64) bool {
	return false
}

func (si *scanIterator) Next() bool {
	ok := si.s.Scan()
	if ok {
		si.curT = si.ts[0]
		si.ts = si.ts[1:]
	}

	return ok
}

func (si *scanIterator) At() (int64, string) {
	return si.curT, si.s.Text()
}

func (si *scanIterator) Err() error {
	return si.s.Err()
}

type noopFlushingWriter struct {
	io.WriteCloser
}

func (noopFlushingWriter) Flush() error {
	return nil
}
