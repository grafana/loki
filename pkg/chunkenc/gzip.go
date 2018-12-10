package chunkenc

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"hash"
	"hash/crc32"
	"io"
	"math"
	"time"

	"github.com/grafana/loki/pkg/logproto"

	"github.com/grafana/loki/pkg/iter"

	"github.com/pkg/errors"
)

var (
	magicNumber = uint32(0x12EE56A)

	chunkFormatV1 = byte(1)
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

	// The finished blocks.
	blocks []block

	// Current in-mem block being appended to.
	head *headBlock

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
	// This is the list of raw entries.
	entries []entry
	size    int // size of uncompressed bytes.

	mint, maxt int64
}

func (hb *headBlock) isEmpty() bool {
	return len(hb.entries) == 0
}

func (hb *headBlock) append(ts int64, line string) error {
	if !hb.isEmpty() && hb.maxt >= ts {
		return ErrOutOfOrder
	}

	hb.entries = append(hb.entries, entry{ts, line})
	if hb.mint == 0 || hb.mint > ts {
		hb.mint = ts
	}
	hb.maxt = ts
	hb.size += len(line)

	return nil
}

func (hb *headBlock) serialise(cw func(w io.Writer) CompressionWriter) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 1<<15)) // 32K. Pool it later.
	encBuf := make([]byte, binary.MaxVarintLen64)
	compressedWriter := cw(buf)
	for _, logEntry := range hb.entries {
		n := binary.PutVarint(encBuf, logEntry.t)
		_, err := compressedWriter.Write(encBuf[:n])
		if err != nil {
			return nil, errors.Wrap(err, "appending entry")
		}

		n = binary.PutUvarint(encBuf, uint64(len(logEntry.s)))
		_, err = compressedWriter.Write(encBuf[:n])
		if err != nil {
			return nil, errors.Wrap(err, "appending entry")
		}
		_, err = compressedWriter.Write([]byte(logEntry.s))
		if err != nil {
			return nil, errors.Wrap(err, "appending entry")
		}
	}
	if err := compressedWriter.Close(); err != nil {
		return nil, errors.Wrap(err, "flushing pending compress buffer")
	}

	return buf.Bytes(), nil
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

		head: &headBlock{
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

	return c
}

// NewByteChunk returns a MemChunk on the passed bytes.
func NewByteChunk(b []byte) (*MemChunk, error) {
	bc := &MemChunk{
		cr:   func(r io.Reader) (CompressionReader, error) { return gzip.NewReader(r) },
		head: &headBlock{}, // Dummy, empty headblock.
	}

	db := decbuf{b: b}

	// Verify the header.
	m, version := db.be32(), db.byte()
	if db.err() != nil {
		return nil, errors.Wrap(db.err(), "verifying header")
	}
	if m != magicNumber {
		return nil, errors.Errorf("invalid magic number %x", m)
	}
	if version != 1 {
		return nil, errors.Errorf("invalid version %d", version)
	}

	metasOffset := binary.BigEndian.Uint64(b[len(b)-8:])
	mb := b[metasOffset : len(b)-(8+4)] // storing the metasOffset + checksum of meta
	db = decbuf{b: mb}

	expCRC := binary.BigEndian.Uint32(b[len(b)-(8+4):])
	if expCRC != db.crc32() {
		return nil, ErrInvalidChecksum
	}

	// Read the number of blocks.
	num := db.uvarint()

	for i := 0; i < num; i++ {
		blk := block{}
		// Read #entries.
		blk.numEntries = db.uvarint()

		// Read mint, maxt.
		blk.mint = db.varint64()
		blk.maxt = db.varint64()

		// Read offset and length.
		blk.offset = db.uvarint()
		l := db.uvarint()
		blk.b = b[blk.offset : blk.offset+l]

		// Verify checksums.
		expCRC := binary.BigEndian.Uint32(b[blk.offset+l:])
		if expCRC != crc32.Checksum(blk.b, castagnoliTable) {
			return bc, ErrInvalidChecksum
		}

		bc.blocks = append(bc.blocks, blk)

		if db.err() != nil {
			return nil, errors.Wrap(db.err(), "decoding block meta")
		}
	}

	return bc, nil
}

// Bytes implements Chunk.
func (c *MemChunk) Bytes() ([]byte, error) {
	if c.head != nil {
		// When generating the bytes, we need to flush the data held in-buffer.
		if err := c.cut(); err != nil {
			return nil, err
		}
	}
	crc32Hash := newCRC32()

	buf := bytes.NewBuffer(nil)
	offset := 0

	eb := encbuf{b: make([]byte, 0, 1<<10)}

	// Write the header (magicNum + version).
	eb.putBE32(magicNumber)
	eb.putByte(chunkFormatV1)

	n, err := buf.Write(eb.get())
	if err != nil {
		return buf.Bytes(), errors.Wrap(err, "write blockMeta #entries")
	}
	offset += n

	// Write Blocks.
	for i, b := range c.blocks {
		c.blocks[i].offset = offset

		eb.reset()
		eb.putBytes(b.b)
		eb.putHash(crc32Hash)

		n, err := buf.Write(eb.get())
		if err != nil {
			return buf.Bytes(), errors.Wrap(err, "write block")
		}
		offset += n
	}

	metasOffset := offset
	// Write the number of blocks.
	eb.reset()
	eb.putUvarint(len(c.blocks))

	// Write BlockMetas.
	for _, b := range c.blocks {
		eb.putUvarint(b.numEntries)
		eb.putVarint64(b.mint)
		eb.putVarint64(b.maxt)
		eb.putUvarint(b.offset)
		eb.putUvarint(len(b.b))
	}
	eb.putHash(crc32Hash)

	_, err = buf.Write(eb.get())
	if err != nil {
		return buf.Bytes(), errors.Wrap(err, "write block metas")
	}

	// Write the metasOffset.
	eb.reset()
	eb.putBE64int(metasOffset)
	_, err = buf.Write(eb.get())
	if err != nil {
		return buf.Bytes(), errors.Wrap(err, "write metasOffset")
	}

	return buf.Bytes(), nil
}

// Encoding implements Chunk.
func (c *MemChunk) Encoding() Encoding {
	return c.encoding
}

// Size implements Chunk.
func (c *MemChunk) Size() int {
	ne := 0
	for _, blk := range c.blocks {
		ne += blk.numEntries
	}

	if !c.head.isEmpty() {
		ne += len(c.head.entries)
	}

	return ne
}

// SpaceFor implements Chunk.
func (c *MemChunk) SpaceFor(*logproto.Entry) bool {
	return len(c.blocks) < 10
}

// Append implements Chunk.
func (c *MemChunk) Append(entry *logproto.Entry) error {
	return c.head.append(entry.Timestamp.UnixNano(), entry.Line)
}

// Close implements Chunk.
// TODO: Fix this to check edge cases.
func (c *MemChunk) Close() error {
	return c.cut()
}

// cut a new block and add it to finished blocks.
func (c *MemChunk) cut() error {
	if c.head.isEmpty() {
		return nil
	}

	b, err := c.head.serialise(c.cw)
	if err != nil {
		return err
	}

	c.blocks = append(c.blocks, block{
		b:          b,
		numEntries: len(c.head.entries),
		mint:       c.head.mint,
		maxt:       c.head.maxt,
	})

	c.head.entries = c.head.entries[:0]
	c.head.mint = 0 // Will be set on first append.

	return nil
}

// Bounds implements Chunk.
func (c *MemChunk) Bounds() (fromT, toT time.Time) {
	var from, to int64
	if len(c.blocks) > 0 {
		from = c.blocks[0].mint
		to = c.blocks[len(c.blocks)-1].maxt
	}

	if !c.head.isEmpty() {
		if from == 0 || from > c.head.mint {
			from = c.head.mint
		}

		if to < c.head.maxt {
			to = c.head.maxt
		}
	}

	return time.Unix(0, from), time.Unix(0, to)
}

// Iterator implements Chunk.
func (c *MemChunk) Iterator(mintT, maxtT time.Time, direction logproto.Direction) (iter.EntryIterator, error) {
	mint, maxt := mintT.UnixNano(), maxtT.UnixNano()
	its := make([]iter.EntryIterator, 0, len(c.blocks))

	for _, b := range c.blocks {
		if maxt > b.mint && b.maxt > mint {
			it, err := b.iterator(c.cr)
			if err != nil {
				return nil, err
			}

			its = append(its, it)
		}
	}

	its = append(its, c.head.iterator(mint, maxt))

	iterForward := iter.NewTimeRangedIterator(
		iter.NewNonOverlappingIterator(its, ""),
		time.Unix(0, mint),
		time.Unix(0, maxt),
	)

	if direction == logproto.FORWARD {
		return iterForward, nil
	}

	return iter.NewEntryIteratorBackward(iterForward)
}

func (b block) iterator(cr func(io.Reader) (CompressionReader, error)) (iter.EntryIterator, error) {
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

func (hb *headBlock) iterator(mint, maxt int64) iter.EntryIterator {
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

func (li *listIterator) Entry() logproto.Entry {
	return logproto.Entry{
		Timestamp: time.Unix(0, li.cur.t),
		Line:      li.cur.s,
	}
}

func (li *listIterator) Error() error   { return nil }
func (li *listIterator) Close() error   { return nil }
func (li *listIterator) Labels() string { return "" }

type bufferedIterator struct {
	s *bufio.Reader

	curT   int64
	curLog string

	err error

	buf    []byte // The buffer a single entry.
	decBuf []byte // The buffer for decoding the lengths.
}

func newBufferedIterator(s *bufio.Reader) *bufferedIterator {
	return &bufferedIterator{
		s:      s,
		buf:    make([]byte, 1024),
		decBuf: make([]byte, binary.MaxVarintLen64),
	}
}

func (si *bufferedIterator) Seek(int64) bool {
	return false
}

func (si *bufferedIterator) Next() bool {
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
		_, err = si.s.Read(si.buf[n:l])
		if err != nil {
			si.err = err
			return false
		}
	}

	si.curT = ts
	si.curLog = string(si.buf[:l])

	return true
}

func (si *bufferedIterator) Entry() logproto.Entry {
	return logproto.Entry{
		Timestamp: time.Unix(0, si.curT),
		Line:      si.curLog,
	}
}

func (si *bufferedIterator) Error() error   { return si.err }
func (si *bufferedIterator) Close() error   { return si.err }
func (si *bufferedIterator) Labels() string { return "" }
