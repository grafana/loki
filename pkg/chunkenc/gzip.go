package chunkenc

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"math"

	"github.com/pkg/errors"
)

var (
	magicNumber = uint32(0x12EE56A)

	chunkFormatV1 = byte(1)

	errInvalidSize     = fmt.Errorf("invalid size")
	errInvalidFlag     = fmt.Errorf("invalid flag")
	errInvalidChecksum = fmt.Errorf("invalid checksum")
)

// The table gets initialized with sync.Once but may still cause a race
// with any other use of the crc32 package anywhere. Thus we initialize it
// before.
var castagnoliTable *crc32.Table

func init() {
	castagnoliTable = crc32.MakeTable(crc32.Castagnoli)
}

// newCRC32 initializes a CRC32 hash with a preconfigured polynomial, so the
// polynomial may be easily changed in one location at a later time, if necessary.
func newCRC32() hash.Hash32 {
	return crc32.New(castagnoliTable)
}

// MemChunk implements compressed log chunks.
type MemChunk struct {
	// The number of uncompressed bytes per block.
	blockSize int
	// The max number of blocks in a chunk.
	maxBlocks int

	// The finished blocks.
	blocks []block

	// Current in-mem block being appended to.
	memBlock *headBlock

	app *memAppender

	encoding Encoding
	cw       func(w io.Writer) CompressionWriter
	cr       func(r io.Reader) (CompressionReader, error)
}

type block struct {
	// This is compressed bytes.
	b          []byte
	numEntries int

	mint, maxt int64

	offset int // The offset of the block in the chunk.
}

// This block holds the un-compressed entries. Once it has enough data, this is
// emptied into a block with only compressed entries.
type headBlock struct {
	// This is compressed bytes.
	b []byte

	// This is the list of raw entries.
	// This is cleared in finished blocks.
	entries    []entry
	numEntries int
	size       int // size of uncompressed bytes.

	mint, maxt int64
}

func (hb *headBlock) isEmpty() bool {
	return hb == nil || len(hb.entries) == 0
}

type entry struct {
	t int64
	s string
}

// NewMemChunk returns a new in-mem chunk.
func NewMemChunk(enc Encoding) *MemChunk {
	c := &MemChunk{
		blockSize: 256 * 1024, // The blockSize in bytes.
		blocks:    []block{},

		memBlock: &headBlock{
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

// NewByteChunk returns a MemChunk on the passed bytes.
func NewByteChunk(b []byte) (*MemChunk, error) {
	bc := &MemChunk{
		cr: func(r io.Reader) (CompressionReader, error) { return gzip.NewReader(r) },
	}

	// Verify the meta.
	if len(b) < 5 {
		return nil, errors.Wrap(errInvalidSize, "chunk header")
	}
	if m := binary.BigEndian.Uint32(b[0:4]); m != magicNumber {
		return nil, errors.Errorf("invalid magic number %x", m)
	}

	version := int(b[4])
	if version != 1 {
		return nil, errors.Errorf("invalid version %d", version)
	}

	metasOffset := binary.BigEndian.Uint64(b[len(b)-8:])
	mb := b[metasOffset:]
	// Read the number of blocks.
	num, n := binary.Uvarint(mb)
	mb = mb[n:]

	for i := uint64(0); i < num; i++ {
		blk := block{}
		// Read #entries.
		entries, n := binary.Uvarint(mb)
		mb = mb[n:]
		blk.numEntries = int(entries)

		// Read mint, maxt.
		ts, n := binary.Varint(mb)
		mb = mb[n:]
		blk.mint = ts
		ts, n = binary.Varint(mb)
		mb = mb[n:]
		blk.maxt = ts

		// Read offset and length.
		l, n := binary.Uvarint(mb)
		mb = mb[n:]
		blk.offset = int(l)
		l, n = binary.Uvarint(mb)
		blk.b = b[blk.offset : blk.offset+int(l)]

		// Verify checksums.
		expCRC := binary.BigEndian.Uint32(b[blk.offset+int(l):])
		if expCRC != crc32.Checksum(blk.b, castagnoliTable) {
			return bc, errInvalidChecksum
		}

		mb = mb[n:]

		bc.blocks = append(bc.blocks, blk)
	}

	return bc, nil
}

// Bytes implements Chunk.
func (c *MemChunk) Bytes() ([]byte, error) {
	if c.app != nil {
		// When generating the bytes, we need to flush the data held in-buffer.
		c.app.cut()
	}
	crc32Hash := newCRC32()

	buf := bytes.NewBuffer(nil)
	encBuf := [binary.MaxVarintLen64]byte{}
	offset := 0

	// Write the magicNumber.
	binary.BigEndian.PutUint32(encBuf[:], uint32(magicNumber))
	n, err := buf.Write(encBuf[:4])
	if err != nil {
		return buf.Bytes(), errors.Wrap(err, "write magic number")
	}
	offset += n

	// Write the version.
	n, err = buf.Write([]byte{chunkFormatV1})
	if err != nil {
		return buf.Bytes(), errors.Wrap(err, "write version")
	}
	offset += n

	// Write Blocks.
	for i, b := range c.blocks {
		c.blocks[i].offset = offset

		crc32Hash.Reset()
		_, err := crc32Hash.Write(b.b)
		if err != nil {
			panic(err) // crc32 doesn't error.
		}
		b.b = crc32Hash.Sum(b.b)

		n, err = buf.Write(b.b)
		if err != nil {
			return buf.Bytes(), errors.Wrap(err, "write block")
		}
		offset += n
	}

	metasOffset := offset
	// Write the number of blocks.
	n = binary.PutUvarint(encBuf[:], uint64(len(c.blocks)))
	n, err = buf.Write(encBuf[:n])
	if err != nil {
		return buf.Bytes(), errors.Wrap(err, "write #blocks")
	}
	offset += n

	// Write BlockMetas.
	for _, b := range c.blocks {
		n = binary.PutUvarint(encBuf[:], uint64(b.numEntries))
		n, err = buf.Write(encBuf[:n])
		if err != nil {
			return buf.Bytes(), errors.Wrap(err, "write blockMeta #entries")
		}

		n = binary.PutVarint(encBuf[:], b.mint)
		n, err = buf.Write(encBuf[:n])
		if err != nil {
			return buf.Bytes(), errors.Wrap(err, "write blockMeta mint")
		}

		n = binary.PutVarint(encBuf[:], b.maxt)
		n, err = buf.Write(encBuf[:n])
		if err != nil {
			return buf.Bytes(), errors.Wrap(err, "write blockMeta maxt")
		}

		n = binary.PutUvarint(encBuf[:], uint64(b.offset))
		n, err = buf.Write(encBuf[:n])
		if err != nil {
			return buf.Bytes(), errors.Wrap(err, "write blockMeta offset")
		}

		n = binary.PutUvarint(encBuf[:], uint64(len(b.b)))
		n, err = buf.Write(encBuf[:n])
		if err != nil {
			return buf.Bytes(), errors.Wrap(err, "write blockMeta len")
		}
	}

	// Write the metasOffset.
	binary.BigEndian.PutUint64(encBuf[:], uint64(metasOffset))
	n, err = buf.Write(encBuf[:8])
	if err != nil {
		return buf.Bytes(), errors.Wrap(err, "write metasOffset")
	}

	return buf.Bytes(), nil
}

// Encoding implements Chunk.
func (c *MemChunk) Encoding() Encoding {
	return c.encoding
}

// NumSamples implements Chunk.
func (c *MemChunk) NumSamples() int {
	ne := 0
	for _, blk := range c.blocks {
		ne += blk.numEntries
	}

	ne += c.memBlock.numEntries

	return ne
}

// SpaceFor implements Chunk.
func (c *MemChunk) SpaceFor(ts int64, log string) bool {
	return len(c.blocks) < 10
}

// Append implements Chunk.
func (c *MemChunk) Append(ts int64, log string) error {
	return c.app.Append(ts, log)
}

// Close implements Chunk.
// TODO: Fix this to check edge cases.
func (c *MemChunk) Close() error {
	return c.app.Close()
}

type memAppender struct {
	// chunk's in-mem block.
	block *headBlock

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

	n := binary.PutVarint(a.encBuf, t)
	_, err := a.writer.Write(a.encBuf[:n])
	if err != nil {
		return errors.Wrap(err, "appending entry")
	}

	n = binary.PutUvarint(a.encBuf, uint64(len(s)))
	_, err = a.writer.Write(a.encBuf[:n])
	if err != nil {
		return errors.Wrap(err, "appending entry")
	}

	_, err = a.writer.Write([]byte(s))
	if err != nil {
		return errors.Wrap(err, "appending entry")
	}
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
	if a.block.isEmpty() {
		return nil
	}

	err := a.writer.Close()
	if err != nil {
		return errors.Wrap(err, "closing writer")
	}
	a.block.b = a.buffer.Bytes()

	a.chunk.blocks = append(a.chunk.blocks, block{
		b:          a.block.b[:],
		numEntries: a.block.numEntries,
		mint:       a.block.mint,
		maxt:       a.block.maxt,
	})

	// Reset the block.
	a.block.entries = a.block.entries[:0]
	a.block.b = make([]byte, 0, len(a.block.b)) // TODO(goutham): Use pool.
	a.block.mint = a.block.maxt
	a.block.numEntries = 0
	a.block.size = 0

	a.buffer = bytes.NewBuffer(a.block.b)
	a.writer.Reset(a.buffer)

	return nil
}

func (a *memAppender) Close() error {
	return a.cut()
}

// Bounds implements Chunk.
func (c *MemChunk) Bounds() (from, to int64) {
	if len(c.blocks) > 0 {
		from = c.blocks[0].mint
		to = c.blocks[len(c.blocks)-1].maxt
	}

	if c.memBlock != nil {
		if from > c.memBlock.mint {
			from = c.memBlock.mint
		}

		if to < c.memBlock.maxt {
			to = c.memBlock.maxt
		}
	}

	return
}

// Iterator implements Chunk.
func (c *MemChunk) Iterator(mint, maxt int64) (Iterator, error) {
	its := make([]Iterator, 0, len(c.blocks))

	for _, b := range c.blocks {
		if maxt > b.mint && b.maxt > mint {
			it, err := b.iterator(c.cr)
			if err != nil {
				return nil, err
			}

			its = append(its, it)
		}
	}

	its = append(its, c.memBlock.iterator(mint, maxt))

	return newChainedIterator(its, mint, maxt), nil
}

func (b block) iterator(cr func(io.Reader) (CompressionReader, error)) (Iterator, error) {
	if len(b.b) == 0 {
		return emptyIterator, nil
	}

	r, err := cr(bytes.NewBuffer(b.b))
	if err != nil {
		return nil, err
	}

	s := bufio.NewReader(r)
	return newBufferedIterator(s), nil
}

func (hb *headBlock) iterator(mint, maxt int64) Iterator {
	if hb.isEmpty() || (maxt < hb.mint || hb.maxt < mint) {
		return emptyIterator
	}

	// We are doing a copy everytime, this is because b.entries could change completely,
	// the alternate would be that we allocate a new b.entries everytime we cut a block,
	// but the tradeoff is that queries to near-realtime data would be much lower than
	// cutting of blocks.

	entries := make([]entry, len(hb.entries))
	copy(entries, hb.entries)

	return &listIterator{
		entries: entries,
	}

}

var emptyIterator = &listIterator{}

type listIterator struct {
	entries []entry

	cur entry
}

// chainedIterator chains several blocks together for iterating.
type chainedIterator struct {
	its []Iterator

	curIt Iterator
	cur   entry

	mint, maxt int64

	err error
}

func newChainedIterator(its []Iterator, mint, maxt int64) *chainedIterator {
	return &chainedIterator{
		its:   its[1:],
		curIt: its[0],

		mint: mint,
		maxt: maxt,
	}
}

func (gi *chainedIterator) Seek(int64) bool {
	return false
}

func (gi *chainedIterator) Next() bool {
	for gi.curIt.Next() {
		gi.cur.t, gi.cur.s = gi.curIt.At()
		if gi.cur.t < gi.mint {
			continue
		}

		if gi.cur.t > gi.maxt {
			return false
		}

		return true
	}

	if len(gi.its) == 0 {
		return false
	}

	gi.curIt = gi.its[0]
	gi.its = gi.its[1:]

	return gi.Next()
}

func (gi *chainedIterator) At() (int64, string) {
	return gi.cur.t, gi.cur.s
}

func (gi *chainedIterator) Err() error {
	if gi.err != nil {
		return gi.err
	}

	return gi.curIt.Err()
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

type bufferedReader struct {
	s *bufio.Reader

	curT   int64
	curLog string

	err error

	buf    []byte // The buffer a single entry.
	decBuf []byte // The buffer for decoding the lengths.
}

func newBufferedIterator(s *bufio.Reader) *bufferedReader {
	return &bufferedReader{
		s:      s,
		buf:    make([]byte, 1024),
		decBuf: make([]byte, binary.MaxVarintLen64),
	}
}

func (si *bufferedReader) Seek(int64) bool {
	return false
}

func (si *bufferedReader) Next() bool {
	ts, err := binary.ReadVarint(si.s)
	if err != nil {
		if err != io.EOF {
			si.err = err
		}

		return false
	}

	l, err := binary.ReadUvarint(si.s)
	if err != nil {
		if err != io.EOF {
			si.err = err

			return false
		}
	}

	for len(si.buf) < int(l) {
		si.buf = append(si.buf, make([]byte, 1024)...)
	}

	n, err := si.s.Read(si.buf[:l])
	if err != nil && err != io.EOF {
		si.err = err
		return false
	}
	if n < int(l) {
		n, err = si.s.Read(si.buf[n:l])
		if err != nil {
			si.err = err
			return false
		}
	}

	si.curT = ts
	si.curLog = string(si.buf[:l])

	return true
}

func (si *bufferedReader) At() (int64, string) {
	return si.curT, si.curLog
}

func (si *bufferedReader) Err() error {
	return si.err
}

type noopFlushingWriter struct {
	io.WriteCloser
}

func (noopFlushingWriter) Flush() error {
	return nil
}
