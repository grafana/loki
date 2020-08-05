package chunkenc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/stats"
)

const (
	blocksPerChunk = 10
	maxLineLength  = 1024 * 1024 * 1024
)

var (
	magicNumber = uint32(0x12EE56A)

	chunkFormatV1 = byte(1)
	chunkFormatV2 = byte(2)
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
	// Target size in compressed bytes
	targetSize int

	// The finished blocks.
	blocks []block
	// The compressed size of all the blocks
	cutBlockSize int

	// Current in-mem block being appended to.
	head *headBlock

	// the chunk format default to v2
	format   byte
	encoding Encoding

	readers ReaderPool
	writers WriterPool
}

type block struct {
	// This is compressed bytes.
	b          []byte
	numEntries int

	mint, maxt int64

	offset           int // The offset of the block in the chunk.
	uncompressedSize int // Total uncompressed size in bytes when the chunk is cut.

	readers ReaderPool
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

func (hb *headBlock) serialise(pool WriterPool) ([]byte, error) {
	inBuf := serializeBytesBufferPool.Get().(*bytes.Buffer)
	defer func() {
		inBuf.Reset()
		serializeBytesBufferPool.Put(inBuf)
	}()
	outBuf := &bytes.Buffer{}

	encBuf := make([]byte, binary.MaxVarintLen64)
	compressedWriter := pool.GetWriter(outBuf)
	defer pool.PutWriter(compressedWriter)
	for _, logEntry := range hb.entries {
		n := binary.PutVarint(encBuf, logEntry.t)
		inBuf.Write(encBuf[:n])

		n = binary.PutUvarint(encBuf, uint64(len(logEntry.s)))
		inBuf.Write(encBuf[:n])

		inBuf.WriteString(logEntry.s)
	}

	if _, err := compressedWriter.Write(inBuf.Bytes()); err != nil {
		return nil, errors.Wrap(err, "appending entry")
	}
	if err := compressedWriter.Close(); err != nil {
		return nil, errors.Wrap(err, "flushing pending compress buffer")
	}

	return outBuf.Bytes(), nil
}

type entry struct {
	t int64
	s string
}

// NewMemChunk returns a new in-mem chunk.
func NewMemChunk(enc Encoding, blockSize, targetSize int) *MemChunk {
	c := &MemChunk{
		blockSize:  blockSize,  // The blockSize in bytes.
		targetSize: targetSize, // Desired chunk size in compressed bytes
		blocks:     []block{},

		head:   &headBlock{},
		format: chunkFormatV2,

		encoding: enc,
		writers:  getWriterPool(enc),
		readers:  getReaderPool(enc),
	}

	return c
}

// NewByteChunk returns a MemChunk on the passed bytes.
func NewByteChunk(b []byte, blockSize, targetSize int) (*MemChunk, error) {
	bc := &MemChunk{
		head:       &headBlock{}, // Dummy, empty headblock.
		blockSize:  blockSize,
		targetSize: targetSize,
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
	bc.format = version
	switch version {
	case chunkFormatV1:
		bc.readers, bc.writers = &Gzip, &Gzip
	case chunkFormatV2:
		// format v2 has a byte for block encoding.
		enc := Encoding(db.byte())
		if db.err() != nil {
			return nil, errors.Wrap(db.err(), "verifying encoding")
		}
		bc.encoding = enc
		bc.readers, bc.writers = getReaderPool(enc), getWriterPool(enc)
	default:
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
		blk := block{
			readers: bc.readers,
		}
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
			level.Error(util.Logger).Log("msg", "Checksum does not match for a block in chunk, this block will be skipped", "err", ErrInvalidChecksum)
			continue
		}

		bc.blocks = append(bc.blocks, blk)

		// Update the counter used to track the size of cut blocks.
		bc.cutBlockSize += len(blk.b)

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
	eb.putByte(c.format)
	if c.format == chunkFormatV2 {
		// chunk format v2 has a byte for encoding.
		eb.putByte(byte(c.encoding))
	}

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

// BlockCount implements Chunk.
func (c *MemChunk) BlockCount() int {
	return len(c.blocks)
}

// SpaceFor implements Chunk.
func (c *MemChunk) SpaceFor(e *logproto.Entry) bool {
	if c.targetSize > 0 {
		// This is looking to see if the uncompressed lines will fit which is not
		// a great check, but it will guarantee we are always under the target size
		newHBSize := c.head.size + len(e.Line)
		return (c.cutBlockSize + newHBSize) < c.targetSize
	}
	// if targetSize is not defined, default to the original behavior of fixed blocks per chunk
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

// CompressedSize implements Chunk
func (c *MemChunk) CompressedSize() int {
	size := 0
	// Better to account for any uncompressed data than ignore it even though this isn't accurate.
	if !c.head.isEmpty() {
		size += c.head.size
	}
	size += c.cutBlockSize
	return size
}

// Utilization implements Chunk.
func (c *MemChunk) Utilization() float64 {
	if c.targetSize != 0 {
		return float64(c.CompressedSize()) / float64(c.targetSize)
	}
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

	b, err := c.head.serialise(c.writers)
	if err != nil {
		return err
	}

	c.blocks = append(c.blocks, block{
		readers:          c.readers,
		b:                b,
		numEntries:       len(c.head.entries),
		mint:             c.head.mint,
		maxt:             c.head.maxt,
		uncompressedSize: c.head.size,
	})

	c.cutBlockSize += len(b)

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
func (c *MemChunk) Iterator(ctx context.Context, mintT, maxtT time.Time, direction logproto.Direction, filter logql.LineFilter) (iter.EntryIterator, error) {
	mint, maxt := mintT.UnixNano(), maxtT.UnixNano()
	its := make([]iter.EntryIterator, 0, len(c.blocks)+1)

	for _, b := range c.blocks {
		if maxt < b.mint || b.maxt < mint {
			continue
		}
		its = append(its, b.Iterator(ctx, filter))
	}

	if !c.head.isEmpty() {
		its = append(its, c.head.iterator(ctx, mint, maxt, filter))
	}

	iterForward := iter.NewTimeRangedIterator(
		iter.NewNonOverlappingIterator(its, ""),
		time.Unix(0, mint),
		time.Unix(0, maxt),
	)

	if direction == logproto.FORWARD {
		return iterForward, nil
	}

	return iter.NewEntryReversedIter(iterForward)
}

// Iterator implements Chunk.
func (c *MemChunk) SampleIterator(ctx context.Context, mintT, maxtT time.Time, filter logql.LineFilter, extractor logql.SampleExtractor) iter.SampleIterator {
	mint, maxt := mintT.UnixNano(), maxtT.UnixNano()
	its := make([]iter.SampleIterator, 0, len(c.blocks)+1)

	for _, b := range c.blocks {
		if maxt < b.mint || b.maxt < mint {
			continue
		}
		its = append(its, b.SampleIterator(ctx, filter, extractor))
	}

	if !c.head.isEmpty() {
		its = append(its, c.head.sampleIterator(ctx, mint, maxt, filter, extractor))
	}

	return iter.NewTimeRangedSampleIterator(
		iter.NewNonOverlappingSampleIterator(its, ""),
		mint,
		maxt,
	)
}

// Blocks implements Chunk
func (c *MemChunk) Blocks(mintT, maxtT time.Time) []Block {
	mint, maxt := mintT.UnixNano(), maxtT.UnixNano()
	blocks := make([]Block, 0, len(c.blocks))

	for _, b := range c.blocks {
		if maxt >= b.mint && b.maxt >= mint {
			blocks = append(blocks, b)
		}
	}
	return blocks
}

func (b block) Iterator(ctx context.Context, filter logql.LineFilter) iter.EntryIterator {
	if len(b.b) == 0 {
		return emptyIterator
	}
	return newEntryIterator(ctx, b.readers, b.b, filter)
}

func (b block) SampleIterator(ctx context.Context, filter logql.LineFilter, extractor logql.SampleExtractor) iter.SampleIterator {
	if len(b.b) == 0 {
		return iter.NoopIterator
	}
	return newSampleIterator(ctx, b.readers, b.b, filter, extractor)
}

func (b block) Offset() int {
	return b.offset
}

func (b block) Entries() int {
	return b.numEntries
}
func (b block) MinTime() int64 {
	return b.mint
}
func (b block) MaxTime() int64 {
	return b.maxt
}

func (hb *headBlock) iterator(ctx context.Context, mint, maxt int64, filter logql.LineFilter) iter.EntryIterator {
	if hb.isEmpty() || (maxt < hb.mint || hb.maxt < mint) {
		return emptyIterator
	}

	chunkStats := stats.GetChunkData(ctx)

	// We are doing a copy everytime, this is because b.entries could change completely,
	// the alternate would be that we allocate a new b.entries everytime we cut a block,
	// but the tradeoff is that queries to near-realtime data would be much lower than
	// cutting of blocks.
	chunkStats.HeadChunkLines += int64(len(hb.entries))
	entries := make([]entry, 0, len(hb.entries))
	for _, e := range hb.entries {
		chunkStats.HeadChunkBytes += int64(len(e.s))
		if filter == nil || filter.Filter([]byte(e.s)) {
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

func (hb *headBlock) sampleIterator(ctx context.Context, mint, maxt int64, filter logql.LineFilter, extractor logql.SampleExtractor) iter.SampleIterator {
	if hb.isEmpty() || (maxt < hb.mint || hb.maxt < mint) {
		return iter.NoopIterator
	}
	chunkStats := stats.GetChunkData(ctx)
	chunkStats.HeadChunkLines += int64(len(hb.entries))
	samples := make([]logproto.Sample, 0, len(hb.entries))
	for _, e := range hb.entries {
		chunkStats.HeadChunkBytes += int64(len(e.s))
		if filter == nil || filter.Filter([]byte(e.s)) {
			if value, ok := extractor.Extract([]byte(e.s)); ok {
				samples = append(samples, logproto.Sample{
					Timestamp: e.t,
					Value:     value,
					Hash:      xxhash.Sum64([]byte(e.s)),
				})

			}
		}
	}

	if len(samples) == 0 {
		return iter.NoopIterator
	}

	return iter.NewSeriesIterator(logproto.Series{Samples: samples})
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
	origBytes []byte
	stats     *stats.ChunkData

	bufReader *bufio.Reader
	reader    io.Reader
	pool      ReaderPool

	err error

	decBuf   []byte // The buffer for decoding the lengths.
	buf      []byte // The buffer for a single entry.
	currLine []byte // the current line, this is the same as the buffer but sliced the the line size.
	currTs   int64
	consumed bool

	closed bool

	filter logql.LineFilter
}

func newBufferedIterator(ctx context.Context, pool ReaderPool, b []byte, filter logql.LineFilter) *bufferedIterator {
	chunkStats := stats.GetChunkData(ctx)
	chunkStats.CompressedBytes += int64(len(b))
	return &bufferedIterator{
		stats:     chunkStats,
		origBytes: b,
		reader:    nil, // will be initialized later
		bufReader: nil, // will be initialized later
		pool:      pool,
		filter:    filter,
		decBuf:    make([]byte, binary.MaxVarintLen64),
		consumed:  true,
	}
}

func (si *bufferedIterator) Next() bool {
	if !si.closed && si.reader == nil {
		// initialize reader now, hopefully reusing one of the previous readers
		si.reader = si.pool.GetReader(bytes.NewBuffer(si.origBytes))
		si.bufReader = BufReaderPool.Get(si.reader)
	}

	for {
		ts, line, ok := si.moveNext()
		if !ok {
			si.Close()
			return false
		}
		// we decode always the line length and ts as varint
		si.stats.DecompressedBytes += int64(len(line)) + 2*binary.MaxVarintLen64
		si.stats.DecompressedLines++
		if si.filter != nil && !si.filter.Filter(line) {
			continue
		}
		si.currTs = ts
		si.currLine = line
		si.consumed = false
		return true
	}
}

// moveNext moves the buffer to the next entry
func (si *bufferedIterator) moveNext() (int64, []byte, bool) {
	ts, err := binary.ReadVarint(si.bufReader)
	if err != nil {
		if err != io.EOF {
			si.err = err
		}
		return 0, nil, false
	}

	l, err := binary.ReadUvarint(si.bufReader)
	if err != nil {
		if err != io.EOF {
			si.err = err
			return 0, nil, false
		}
	}
	lineSize := int(l)

	if lineSize >= maxLineLength {
		si.err = fmt.Errorf("line too long %d, maximum %d", lineSize, maxLineLength)
		return 0, nil, false
	}
	// If the buffer is not yet initialize or too small, we get a new one.
	if si.buf == nil || lineSize > cap(si.buf) {
		// in case of a replacement we replace back the buffer in the pool
		if si.buf != nil {
			BytesBufferPool.Put(si.buf)
		}
		si.buf = BytesBufferPool.Get(lineSize).([]byte)
		if lineSize > cap(si.buf) {
			si.err = fmt.Errorf("could not get a line buffer of size %d, actual %d", lineSize, cap(si.buf))
			return 0, nil, false
		}
	}
	// Then process reading the line.
	n, err := si.bufReader.Read(si.buf[:lineSize])
	if err != nil && err != io.EOF {
		si.err = err
		return 0, nil, false
	}
	for n < lineSize {
		r, err := si.bufReader.Read(si.buf[n:lineSize])
		if err != nil && err != io.EOF {
			si.err = err
			return 0, nil, false
		}
		n += r
	}
	return ts, si.buf[:lineSize], true
}

func (si *bufferedIterator) Error() error { return si.err }

func (si *bufferedIterator) Close() error {
	if !si.closed {
		si.closed = true
		si.close()
	}
	return si.err
}

func (si *bufferedIterator) close() {
	if si.reader != nil {
		si.pool.PutReader(si.reader)
		si.reader = nil
	}
	if si.bufReader != nil {
		BufReaderPool.Put(si.bufReader)
		si.bufReader = nil
	}

	if si.buf != nil {
		BytesBufferPool.Put(si.buf)
		si.buf = nil
	}
	si.origBytes = nil
	si.decBuf = nil
}

func (si *bufferedIterator) Labels() string { return "" }

func newEntryIterator(ctx context.Context, pool ReaderPool, b []byte, filter logql.LineFilter) iter.EntryIterator {
	return &entryBufferedIterator{
		bufferedIterator: newBufferedIterator(ctx, pool, b, filter),
	}
}

type entryBufferedIterator struct {
	*bufferedIterator
	cur logproto.Entry
}

func (e *entryBufferedIterator) Entry() logproto.Entry {
	if !e.consumed {
		e.cur.Timestamp = time.Unix(0, e.currTs)
		e.cur.Line = string(e.currLine)
		e.consumed = true
	}
	return e.cur
}

func newSampleIterator(ctx context.Context, pool ReaderPool, b []byte, filter logql.LineFilter, extractor logql.SampleExtractor) iter.SampleIterator {
	it := &sampleBufferedIterator{
		bufferedIterator: newBufferedIterator(ctx, pool, b, filter),
		extractor:        extractor,
	}
	return it
}

type sampleBufferedIterator struct {
	*bufferedIterator
	extractor logql.SampleExtractor
	cur       logproto.Sample
	currValue float64
}

func (e *sampleBufferedIterator) Next() bool {
	var ok bool
	for e.bufferedIterator.Next() {
		if e.currValue, ok = e.extractor.Extract(e.currLine); ok {
			return true
		}
	}
	return false
}

func (e *sampleBufferedIterator) Sample() logproto.Sample {
	if !e.consumed {
		e.cur.Timestamp = e.currTs
		e.cur.Hash = xxhash.Sum64(e.currLine)
		e.cur.Value = e.currValue
		e.consumed = true
	}
	return e.cur
}
