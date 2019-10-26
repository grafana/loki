package chunkenc

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"time"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/pkg/errors"
)

const blocksPerChunk = 10

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
	cPool    CompressionPool
}

type block struct {
	// This is compressed bytes.
	b          []byte
	numEntries int

	mint, maxt int64

	offset           int // The offset of the block in the chunk.
	uncompressedSize int // Total uncompressed size in bytes when the chunk is cut.
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
	if !hb.isEmpty() && hb.maxt > ts {
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

func (hb *headBlock) serialise(pool CompressionPool) ([]byte, error) {
	buf := &bytes.Buffer{}
	encBuf := make([]byte, binary.MaxVarintLen64)
	compressedWriter := pool.GetWriter(buf)
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
	pool.PutWriter(compressedWriter)
	return buf.Bytes(), nil
}

type entry struct {
	t int64
	s string
}

// NewMemChunkSize returns a new in-mem chunk.
// Mainly for config push size.
func NewMemChunkSize(enc Encoding, blockSize int) *MemChunk {
	c := &MemChunk{
		blockSize: blockSize, // The blockSize in bytes.
		blocks:    []block{},

		head: &headBlock{},

		encoding: enc,
	}

	switch enc {
	case EncGZIP:
		c.cPool = &Gzip
	default:
		panic("unknown encoding")
	}

	return c
}

// NewMemChunk returns a new in-mem chunk for query.
func NewMemChunk(enc Encoding) *MemChunk {
	return NewMemChunkSize(enc, 256*1024)
}

// NewByteChunk returns a MemChunk on the passed bytes.
func NewByteChunk(b []byte) (*MemChunk, error) {
	bc := &MemChunk{
		cPool:    &Gzip,
		encoding: EncGZIP,
		head:     &headBlock{}, // Dummy, empty headblock.
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
	bc.blocks = make([]block, 0, num)

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
	return len(c.blocks) < blocksPerChunk
}

// UncompressedSize implements Chunk.
func (c *MemChunk) UncompressedSize() int {
	size := 0

	if !c.head.isEmpty() {
		size += c.head.size
	}

	for _, b := range c.blocks {
		size += b.uncompressedSize
	}

	return size
}

// Utilization implements Chunk.  It is the bytes used as a percentage of the
func (c *MemChunk) Utilization() float64 {
	size := c.UncompressedSize()

	return float64(size) / float64(blocksPerChunk*c.blockSize)
}

// Append implements Chunk.
func (c *MemChunk) Append(entry *logproto.Entry) error {
	entryTimestamp := entry.Timestamp.UnixNano()

	// If the head block is empty but there are cut blocks, we have to make
	// sure the new entry is not out of order compared to the previous block
	if c.head.isEmpty() && len(c.blocks) > 0 && c.blocks[len(c.blocks)-1].maxt > entryTimestamp {
		return ErrOutOfOrder
	}

	if err := c.head.append(entryTimestamp, entry.Line); err != nil {
		return err
	}

	if c.head.size >= c.blockSize {
		return c.cut()
	}

	return nil
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

	b, err := c.head.serialise(c.cPool)
	if err != nil {
		return err
	}

	c.blocks = append(c.blocks, block{
		b:                b,
		numEntries:       len(c.head.entries),
		mint:             c.head.mint,
		maxt:             c.head.maxt,
		uncompressedSize: c.head.size,
	})

	c.head.entries = c.head.entries[:0]
	c.head.mint = 0 // Will be set on first append.
	c.head.size = 0

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
func (c *MemChunk) Iterator(mintT, maxtT time.Time, direction logproto.Direction, filter logql.Filter) (iter.EntryIterator, error) {
	mint, maxt := mintT.UnixNano(), maxtT.UnixNano()
	its := make([]iter.EntryIterator, 0, len(c.blocks)+1)

	for _, b := range c.blocks {
		if maxt > b.mint && b.maxt > mint {
			its = append(its, b.iterator(c.cPool, filter))
		}
	}

	if !c.head.isEmpty() {
		its = append(its, c.head.iterator(mint, maxt, filter))
	}

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

func (b block) iterator(pool CompressionPool, filter logql.Filter) iter.EntryIterator {
	if len(b.b) == 0 {
		return emptyIterator
	}
	return newBufferedIterator(pool, b.b, filter)
}

func (hb *headBlock) iterator(mint, maxt int64, filter logql.Filter) iter.EntryIterator {
	if hb.isEmpty() || (maxt < hb.mint || hb.maxt < mint) {
		return emptyIterator
	}

	// We are doing a copy everytime, this is because b.entries could change completely,
	// the alternate would be that we allocate a new b.entries everytime we cut a block,
	// but the tradeoff is that queries to near-realtime data would be much lower than
	// cutting of blocks.

	entries := make([]entry, 0, len(hb.entries))
	for _, e := range hb.entries {
		if filter == nil || filter([]byte(e.s)) {
			entries = append(entries, e)
		}
	}

	if len(entries) == 0 {
		return emptyIterator
	}

	return &listIterator{
		entries: entries,
		cur:     -1,
	}
}

var emptyIterator = &listIterator{}

type listIterator struct {
	entries []entry
	cur     int
}

func (li *listIterator) Next() bool {
	li.cur++

	return li.cur < len(li.entries)
}

func (li *listIterator) Entry() logproto.Entry {
	if li.cur < 0 || li.cur >= len(li.entries) {
		return logproto.Entry{}
	}

	cur := li.entries[li.cur]

	return logproto.Entry{
		Timestamp: time.Unix(0, cur.t),
		Line:      cur.s,
	}
}

func (li *listIterator) Error() error   { return nil }
func (li *listIterator) Close() error   { return nil }
func (li *listIterator) Labels() string { return "" }

type bufferedIterator struct {
	s      *bufio.Reader
	reader CompressionReader
	pool   CompressionPool

	cur logproto.Entry

	err error

	buf    []byte // The buffer for a single entry.
	decBuf []byte // The buffer for decoding the lengths.

	closed bool

	filter logql.Filter
}

func newBufferedIterator(pool CompressionPool, b []byte, filter logql.Filter) *bufferedIterator {
	r := pool.GetReader(bytes.NewBuffer(b))
	return &bufferedIterator{
		s:      BufReaderPool.Get(r),
		reader: r,
		pool:   pool,
		filter: filter,
		decBuf: make([]byte, binary.MaxVarintLen64),
	}
}

func (si *bufferedIterator) Next() bool {
	for {
		ts, line, ok := si.moveNext()
		if !ok {
			si.Close()
			return false
		}
		if si.filter != nil && !si.filter(line) {
			continue
		}
		si.cur.Line = string(line)
		si.cur.Timestamp = time.Unix(0, ts)
		return true
	}
}

// moveNext moves the buffer to the next entry
func (si *bufferedIterator) moveNext() (int64, []byte, bool) {
	ts, err := binary.ReadVarint(si.s)
	if err != nil {
		if err != io.EOF {
			si.err = err
		}
		return 0, nil, false
	}

	l, err := binary.ReadUvarint(si.s)
	if err != nil {
		if err != io.EOF {
			si.err = err
			return 0, nil, false
		}
	}
	lineSize := int(l)

	// If the buffer is not yet initialize or too small, we get a new one.
	if si.buf == nil || lineSize > cap(si.buf) {
		// in case of a replacement we replace back the buffer in the pool
		if si.buf != nil {
			BytesBufferPool.Put(si.buf)
		}
		si.buf = BytesBufferPool.Get(lineSize).([]byte)
		if lineSize > cap(si.buf) {
			fmt.Println("oups ", lineSize, " ", len(si.buf), " ", cap(si.buf))
		}
	}

	// Then process reading the line.
	n, err := si.s.Read(si.buf[:lineSize])
	if err != nil && err != io.EOF {
		si.err = err
		return 0, nil, false
	}
	for n < lineSize {
		r, err := si.s.Read(si.buf[n:lineSize])
		if err != nil {
			si.err = err
			return 0, nil, false
		}
		n += r
	}
	return ts, si.buf[:lineSize], true
}

func (si *bufferedIterator) Entry() logproto.Entry {
	return si.cur
}

func (si *bufferedIterator) Error() error { return si.err }

func (si *bufferedIterator) Close() error {
	if !si.closed {
		si.closed = true
		si.pool.PutReader(si.reader)
		BufReaderPool.Put(si.s)
		if si.buf != nil {
			BytesBufferPool.Put(si.buf)
		}
		si.s = nil
		si.buf = nil
		si.decBuf = nil
		si.reader = nil
		return si.err
	}
	return si.err
}

func (si *bufferedIterator) Labels() string { return "" }
