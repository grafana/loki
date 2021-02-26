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
	"sort"
	"time"

	"github.com/cespare/xxhash/v2"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/logql/stats"
)

const (
	_ byte = iota
	chunkFormatV1
	chunkFormatV2
	chunkFormatV3

	blocksPerChunk = 10
	maxLineLength  = 1024 * 1024 * 1024
)

var magicNumber = uint32(0x12EE56A)

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

func (hb *headBlock) clear() {
	if hb.entries != nil {
		hb.entries = hb.entries[:0]
	}
	hb.size = 0
	hb.mint = 0
	hb.maxt = 0
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

// CheckpointBytes serializes a headblock to []byte. This is used by the WAL checkpointing,
// which does not want to mutate a chunk by cutting it (otherwise risking content address changes), but
// needs to serialize/deserialize the data to disk to ensure data durability.
func (hb *headBlock) CheckpointBytes(version byte, b []byte) ([]byte, error) {
	buf := bytes.NewBuffer(b[:0])
	err := hb.CheckpointTo(version, buf)
	return buf.Bytes(), err
}

// CheckpointSize returns the estimated size of the headblock checkpoint.
func (hb *headBlock) CheckpointSize(version byte) int {
	size := 1                                                                 // version
	size += binary.MaxVarintLen32 * 2                                         // total entries + total size
	size += binary.MaxVarintLen64 * 2                                         // mint,maxt
	size += (binary.MaxVarintLen64 + binary.MaxVarintLen32) * len(hb.entries) // ts + len of log line.

	for _, e := range hb.entries {
		size += len(e.s)
	}
	return size
}

// CheckpointTo serializes a headblock to a `io.Writer`. see `CheckpointBytes`.
func (hb *headBlock) CheckpointTo(version byte, w io.Writer) error {
	eb := EncodeBufferPool.Get().(*encbuf)
	defer EncodeBufferPool.Put(eb)

	eb.reset()

	eb.putByte(version)
	_, err := w.Write(eb.get())
	if err != nil {
		return errors.Wrap(err, "write headBlock version")
	}
	eb.reset()

	eb.putUvarint(len(hb.entries))
	eb.putUvarint(hb.size)
	eb.putVarint64(hb.mint)
	eb.putVarint64(hb.maxt)

	_, err = w.Write(eb.get())
	if err != nil {
		return errors.Wrap(err, "write headBlock metas")
	}
	eb.reset()

	for _, entry := range hb.entries {
		eb.putVarint64(entry.t)
		eb.putUvarint(len(entry.s))
		_, err = w.Write(eb.get())
		if err != nil {
			return errors.Wrap(err, "write headBlock entry ts")
		}
		eb.reset()

		_, err := io.WriteString(w, entry.s)
		if err != nil {
			return errors.Wrap(err, "write headblock entry line")
		}
	}
	return nil
}

func (hb *headBlock) FromCheckpoint(b []byte) error {
	if len(b) < 1 {
		return nil
	}

	db := decbuf{b: b}

	version := db.byte()
	if db.err() != nil {
		return errors.Wrap(db.err(), "verifying headblock header")
	}
	switch version {
	case chunkFormatV1, chunkFormatV2, chunkFormatV3:
	default:
		return errors.Errorf("incompatible headBlock version (%v), only V1,V2,V3 is currently supported", version)
	}

	ln := db.uvarint()
	hb.size = db.uvarint()
	hb.mint = db.varint64()
	hb.maxt = db.varint64()

	if err := db.err(); err != nil {
		return errors.Wrap(err, "verifying headblock metadata")
	}

	hb.entries = make([]entry, ln)
	for i := 0; i < ln && db.err() == nil; i++ {
		var entry entry
		entry.t = db.varint64()
		lineLn := db.uvarint()
		entry.s = string(db.bytes(lineLn))
		hb.entries[i] = entry
	}

	if err := db.err(); err != nil {
		return errors.Wrap(err, "decoding entries")
	}

	return nil
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
		format: chunkFormatV3,

		encoding: enc,
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
		bc.encoding = EncGZIP
	case chunkFormatV2, chunkFormatV3:
		// format v2+ has a byte for block encoding.
		enc := Encoding(db.byte())
		if db.err() != nil {
			return nil, errors.Wrap(db.err(), "verifying encoding")
		}
		bc.encoding = enc
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
		var blk block
		// Read #entries.
		blk.numEntries = db.uvarint()

		// Read mint, maxt.
		blk.mint = db.varint64()
		blk.maxt = db.varint64()

		// Read offset and length.
		blk.offset = db.uvarint()
		if version == chunkFormatV3 {
			blk.uncompressedSize = db.uvarint()
		}
		l := db.uvarint()
		blk.b = b[blk.offset : blk.offset+l]

		// Verify checksums.
		expCRC := binary.BigEndian.Uint32(b[blk.offset+l:])
		if expCRC != crc32.Checksum(blk.b, castagnoliTable) {
			level.Error(util_log.Logger).Log("msg", "Checksum does not match for a block in chunk, this block will be skipped", "err", ErrInvalidChecksum)
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

// BytesWith uses a provided []byte for buffer instantiation
// NOTE: This does not cut the head block nor include any head block data.
func (c *MemChunk) BytesWith(b []byte) ([]byte, error) {
	buf := bytes.NewBuffer(b[:0])
	if _, err := c.WriteTo(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Bytes implements Chunk.
// NOTE: Does not cut head block or include any head block data.
func (c *MemChunk) Bytes() ([]byte, error) {
	return c.BytesWith(nil)
}

// BytesSize returns the raw size of the chunk.
// NOTE: This does not account for the head block nor include any head block data.
func (c *MemChunk) BytesSize() int {
	size := 4 // magic number
	size++    // format
	if c.format > chunkFormatV1 {
		size++ // chunk format v2+ has a byte for encoding.
	}

	// blocks
	for _, b := range c.blocks {
		size += len(b.b) + crc32.Size // size + crc

		size += binary.MaxVarintLen32 // num entries
		size += binary.MaxVarintLen64 // mint
		size += binary.MaxVarintLen64 // maxt
		size += binary.MaxVarintLen32 // offset
		if c.format == chunkFormatV3 {
			size += binary.MaxVarintLen32 // uncompressed size
		}
		size += binary.MaxVarintLen32 // len(b)
	}

	// blockmeta
	size += binary.MaxVarintLen32 // len  blocks

	size += crc32.Size // metablock crc
	size += 8          // metaoffset
	return size
}

// WriteTo Implements io.WriterTo
// NOTE: Does not cut head block or include any head block data.
// For this to be the case you must call Close() first.
// This decision notably enables WAL checkpointing, which would otherwise
// result in different content addressable chunks in storage based on the timing of when
// they were checkpointed (which would cause new blocks to be cut early).
func (c *MemChunk) WriteTo(w io.Writer) (int64, error) {
	crc32Hash := crc32HashPool.Get().(hash.Hash32)
	defer crc32HashPool.Put(crc32Hash)
	crc32Hash.Reset()

	offset := int64(0)

	eb := EncodeBufferPool.Get().(*encbuf)
	defer EncodeBufferPool.Put(eb)

	eb.reset()

	// Write the header (magicNum + version).
	eb.putBE32(magicNumber)
	eb.putByte(c.format)
	if c.format > chunkFormatV1 {
		// chunk format v2+ has a byte for encoding.
		eb.putByte(byte(c.encoding))
	}

	n, err := w.Write(eb.get())
	if err != nil {
		return offset, errors.Wrap(err, "write blockMeta #entries")
	}
	offset += int64(n)

	// Write Blocks.
	for i, b := range c.blocks {
		c.blocks[i].offset = int(offset)

		crc32Hash.Reset()
		_, err := crc32Hash.Write(b.b)
		if err != nil {
			return offset, errors.Wrap(err, "write block")
		}

		n, err := w.Write(crc32Hash.Sum(b.b))
		if err != nil {
			return offset, errors.Wrap(err, "write block")
		}
		offset += int64(n)
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
		if c.format == chunkFormatV3 {
			eb.putUvarint(b.uncompressedSize)
		}
		eb.putUvarint(len(b.b))
	}
	eb.putHash(crc32Hash)

	n, err = w.Write(eb.get())
	if err != nil {
		return offset, errors.Wrap(err, "write block metas")
	}
	offset += int64(n)

	// Write the metasOffset.
	eb.reset()
	eb.putBE64int(int(metasOffset))
	n, err = w.Write(eb.get())
	if err != nil {
		return offset, errors.Wrap(err, "write metasOffset")
	}
	offset += int64(n)

	return offset, nil
}

// SerializeForCheckpointTo serialize the chunk & head into different `io.Writer` for checkpointing use.
// This is to ensure eventually flushed chunks don't have different substructures depending on when they were checkpointed.
// In turn this allows us to maintain a more effective dedupe ratio in storage.
func (c *MemChunk) SerializeForCheckpointTo(chk, head io.Writer) error {
	_, err := c.WriteTo(chk)
	if err != nil {
		return err
	}

	if c.head.isEmpty() {
		return nil
	}

	err = c.head.CheckpointTo(c.format, head)
	if err != nil {
		return err
	}

	return nil
}

func (c *MemChunk) CheckpointSize() (chunk, head int) {
	return c.BytesSize(), c.head.CheckpointSize(c.format)
}

func MemchunkFromCheckpoint(chk, head []byte, blockSize int, targetSize int) (*MemChunk, error) {
	mc, err := NewByteChunk(chk, blockSize, targetSize)
	if err != nil {
		return nil, err
	}
	return mc, mc.head.FromCheckpoint(head)
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

// CompressedSize implements Chunk.
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

	b, err := c.head.serialise(getWriterPool(c.encoding))
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

	c.cutBlockSize += len(b)

	c.head.clear()
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
func (c *MemChunk) Iterator(ctx context.Context, mintT, maxtT time.Time, direction logproto.Direction, pipeline log.StreamPipeline) (iter.EntryIterator, error) {
	mint, maxt := mintT.UnixNano(), maxtT.UnixNano()
	blockItrs := make([]iter.EntryIterator, 0, len(c.blocks)+1)
	var headIterator iter.EntryIterator

	for _, b := range c.blocks {
		if maxt < b.mint || b.maxt < mint {
			continue
		}
		blockItrs = append(blockItrs, encBlock{c.encoding, b}.Iterator(ctx, pipeline))
	}

	if !c.head.isEmpty() {
		headIterator = c.head.iterator(ctx, direction, mint, maxt, pipeline)
	}

	if direction == logproto.FORWARD {
		// add the headblock iterator at the end.
		if headIterator != nil {
			blockItrs = append(blockItrs, headIterator)
		}
		return iter.NewTimeRangedIterator(
			iter.NewNonOverlappingIterator(blockItrs, ""),
			time.Unix(0, mint),
			time.Unix(0, maxt),
		), nil
	}
	// reverse each block entries
	for i, it := range blockItrs {
		r, err := iter.NewEntryReversedIter(
			iter.NewTimeRangedIterator(it,
				time.Unix(0, mint),
				time.Unix(0, maxt),
			))
		if err != nil {
			return nil, err
		}
		blockItrs[i] = r
	}
	// except the head block which is already reversed via the heapIterator.
	if headIterator != nil {
		blockItrs = append(blockItrs, headIterator)
	}
	// then reverse all iterators.
	for i, j := 0, len(blockItrs)-1; i < j; i, j = i+1, j-1 {
		blockItrs[i], blockItrs[j] = blockItrs[j], blockItrs[i]
	}

	return iter.NewNonOverlappingIterator(blockItrs, ""), nil
}

// Iterator implements Chunk.
func (c *MemChunk) SampleIterator(ctx context.Context, from, through time.Time, extractor log.StreamSampleExtractor) iter.SampleIterator {
	mint, maxt := from.UnixNano(), through.UnixNano()
	its := make([]iter.SampleIterator, 0, len(c.blocks)+1)

	for _, b := range c.blocks {
		if maxt < b.mint || b.maxt < mint {
			continue
		}
		its = append(its, encBlock{c.encoding, b}.SampleIterator(ctx, extractor))
	}

	if !c.head.isEmpty() {
		its = append(its, c.head.sampleIterator(ctx, mint, maxt, extractor))
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
			blocks = append(blocks, encBlock{c.encoding, b})
		}
	}
	return blocks
}

// encBlock is an internal wrapper for a block, mainly to avoid binding an encoding in a block itself.
// This may seem roundabout, but the encoding is already a field on the parent MemChunk type. encBlock
// then allows us to bind a decoding context to a block when requested, but otherwise helps reduce the
// chances of chunk<>block encoding drift in the codebase as the latter is parameterized by the former.
type encBlock struct {
	enc Encoding
	block
}

func (b encBlock) Iterator(ctx context.Context, pipeline log.StreamPipeline) iter.EntryIterator {
	if len(b.b) == 0 {
		return iter.NoopIterator
	}
	return newEntryIterator(ctx, getReaderPool(b.enc), b.b, pipeline)
}

func (b encBlock) SampleIterator(ctx context.Context, extractor log.StreamSampleExtractor) iter.SampleIterator {
	if len(b.b) == 0 {
		return iter.NoopIterator
	}
	return newSampleIterator(ctx, getReaderPool(b.enc), b.b, extractor)
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

func (hb *headBlock) iterator(ctx context.Context, direction logproto.Direction, mint, maxt int64, pipeline log.StreamPipeline) iter.EntryIterator {
	if hb.isEmpty() || (maxt < hb.mint || hb.maxt < mint) {
		return iter.NoopIterator
	}

	chunkStats := stats.GetChunkData(ctx)

	// We are doing a copy everytime, this is because b.entries could change completely,
	// the alternate would be that we allocate a new b.entries everytime we cut a block,
	// but the tradeoff is that queries to near-realtime data would be much lower than
	// cutting of blocks.
	chunkStats.HeadChunkLines += int64(len(hb.entries))
	streams := map[uint64]*logproto.Stream{}

	process := func(e entry) {
		// apply time filtering
		if e.t < mint || e.t >= maxt {
			return
		}
		chunkStats.HeadChunkBytes += int64(len(e.s))
		newLine, parsedLbs, ok := pipeline.ProcessString(e.s)
		if !ok {
			return
		}
		var stream *logproto.Stream
		lhash := parsedLbs.Hash()
		if stream, ok = streams[lhash]; !ok {
			stream = &logproto.Stream{
				Labels: parsedLbs.String(),
			}
			streams[lhash] = stream
		}
		stream.Entries = append(stream.Entries, logproto.Entry{
			Timestamp: time.Unix(0, e.t),
			Line:      newLine,
		})
	}

	if direction == logproto.FORWARD {
		for _, e := range hb.entries {
			process(e)
		}
	} else {
		for i := len(hb.entries) - 1; i >= 0; i-- {
			process(hb.entries[i])
		}
	}

	if len(streams) == 0 {
		return iter.NoopIterator
	}
	streamsResult := make([]logproto.Stream, 0, len(streams))
	for _, stream := range streams {
		streamsResult = append(streamsResult, *stream)
	}
	return iter.NewStreamsIterator(ctx, streamsResult, direction)
}

func (hb *headBlock) sampleIterator(ctx context.Context, mint, maxt int64, extractor log.StreamSampleExtractor) iter.SampleIterator {
	if hb.isEmpty() || (maxt < hb.mint || hb.maxt < mint) {
		return iter.NoopIterator
	}
	chunkStats := stats.GetChunkData(ctx)
	chunkStats.HeadChunkLines += int64(len(hb.entries))
	series := map[uint64]*logproto.Series{}
	for _, e := range hb.entries {
		chunkStats.HeadChunkBytes += int64(len(e.s))
		value, parsedLabels, ok := extractor.ProcessString(e.s)
		if !ok {
			continue
		}
		var found bool
		var s *logproto.Series
		lhash := parsedLabels.Hash()
		if s, found = series[lhash]; !found {
			s = &logproto.Series{
				Labels: parsedLabels.String(),
			}
			series[lhash] = s
		}

		// []byte here doesn't create allocation because Sum64 has go:noescape directive
		// It specifies that the function does not allow any of the pointers passed as arguments to escape into the heap or into the values returned from the function.
		h := xxhash.Sum64([]byte(e.s))
		s.Samples = append(s.Samples, logproto.Sample{
			Timestamp: e.t,
			Value:     value,
			Hash:      h,
		})
	}

	if len(series) == 0 {
		return iter.NoopIterator
	}
	seriesRes := make([]logproto.Series, 0, len(series))
	for _, s := range series {
		// todo(ctovena) not sure we need this sort.
		sort.Sort(s)
		seriesRes = append(seriesRes, *s)
	}
	return iter.NewMultiSeriesIterator(ctx, seriesRes)
}

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

	closed bool
}

func newBufferedIterator(ctx context.Context, pool ReaderPool, b []byte) *bufferedIterator {
	chunkStats := stats.GetChunkData(ctx)
	chunkStats.CompressedBytes += int64(len(b))
	return &bufferedIterator{
		stats:     chunkStats,
		origBytes: b,
		reader:    nil, // will be initialized later
		bufReader: nil, // will be initialized later
		pool:      pool,
		decBuf:    make([]byte, binary.MaxVarintLen64),
	}
}

func (si *bufferedIterator) Next() bool {
	if !si.closed && si.reader == nil {
		// initialize reader now, hopefully reusing one of the previous readers
		si.reader = si.pool.GetReader(bytes.NewBuffer(si.origBytes))
		si.bufReader = BufReaderPool.Get(si.reader)
	}

	ts, line, ok := si.moveNext()
	if !ok {
		si.Close()
		return false
	}
	// we decode always the line length and ts as varint
	si.stats.DecompressedBytes += int64(len(line)) + 2*binary.MaxVarintLen64
	si.stats.DecompressedLines++

	si.currTs = ts
	si.currLine = line
	return true
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

func newEntryIterator(ctx context.Context, pool ReaderPool, b []byte, pipeline log.StreamPipeline) iter.EntryIterator {
	return &entryBufferedIterator{
		bufferedIterator: newBufferedIterator(ctx, pool, b),
		pipeline:         pipeline,
	}
}

type entryBufferedIterator struct {
	*bufferedIterator
	pipeline log.StreamPipeline

	cur        logproto.Entry
	currLabels log.LabelsResult
}

func (e *entryBufferedIterator) Entry() logproto.Entry {
	return e.cur
}

func (e *entryBufferedIterator) Labels() string { return e.currLabels.String() }

func (e *entryBufferedIterator) Next() bool {
	for e.bufferedIterator.Next() {
		newLine, lbs, ok := e.pipeline.Process(e.currLine)
		if !ok {
			continue
		}
		e.cur.Timestamp = time.Unix(0, e.currTs)
		e.cur.Line = string(newLine)
		e.currLabels = lbs
		return true
	}
	return false
}

func newSampleIterator(ctx context.Context, pool ReaderPool, b []byte, extractor log.StreamSampleExtractor) iter.SampleIterator {
	it := &sampleBufferedIterator{
		bufferedIterator: newBufferedIterator(ctx, pool, b),
		extractor:        extractor,
	}
	return it
}

type sampleBufferedIterator struct {
	*bufferedIterator

	extractor log.StreamSampleExtractor

	cur        logproto.Sample
	currLabels log.LabelsResult
}

func (e *sampleBufferedIterator) Next() bool {
	for e.bufferedIterator.Next() {
		val, labels, ok := e.extractor.Process(e.currLine)
		if !ok {
			continue
		}
		e.currLabels = labels
		e.cur.Value = val
		e.cur.Hash = xxhash.Sum64(e.currLine)
		e.cur.Timestamp = e.currTs
		return true
	}
	return false
}
func (e *sampleBufferedIterator) Labels() string { return e.currLabels.String() }

func (e *sampleBufferedIterator) Sample() logproto.Sample {
	return e.cur
}
