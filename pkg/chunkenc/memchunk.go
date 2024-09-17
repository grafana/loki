package chunkenc

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"time"
	"unsafe"

	"github.com/cespare/xxhash/v2"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/util/filter"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	_ byte = iota
	ChunkFormatV1
	ChunkFormatV2
	ChunkFormatV3
	ChunkFormatV4

	blocksPerChunk = 10
	maxLineLength  = 1024 * 1024 * 1024

	// defaultBlockSize is used for target block size when cutting partially deleted chunks from a delete request.
	// This could wary from configured block size using `ingester.chunks-block-size` flag or equivalent yaml config resulting in
	// different block size in the new chunk which should be fine.
	defaultBlockSize = 256 * 1024

	chunkMetasSectionIdx              = 1
	chunkStructuredMetadataSectionIdx = 2
)

var HeadBlockFmts = []HeadBlockFmt{OrderedHeadBlockFmt, UnorderedHeadBlockFmt, UnorderedWithStructuredMetadataHeadBlockFmt}

type HeadBlockFmt byte

func (f HeadBlockFmt) Byte() byte { return byte(f) }

func (f HeadBlockFmt) String() string {
	switch {
	case f < UnorderedHeadBlockFmt:
		return "ordered"
	case f == UnorderedHeadBlockFmt:
		return "unordered"
	case f == UnorderedWithStructuredMetadataHeadBlockFmt:
		return "unordered with structured metadata"
	default:
		return fmt.Sprintf("unknown: %v", byte(f))
	}
}

func (f HeadBlockFmt) NewBlock(symbolizer *symbolizer) HeadBlock {
	switch {
	case f < UnorderedHeadBlockFmt:
		return &headBlock{}
	default:
		return newUnorderedHeadBlock(f, symbolizer)
	}
}

const (
	_ HeadBlockFmt = iota
	// placeholders to start splitting chunk formats vs head block
	// fmts at v3
	_
	_
	OrderedHeadBlockFmt
	UnorderedHeadBlockFmt
	UnorderedWithStructuredMetadataHeadBlockFmt
)

// ChunkHeadFormatFor returns corresponding head block format for the given `chunkfmt`.
func ChunkHeadFormatFor(chunkfmt byte) HeadBlockFmt {
	if chunkfmt < ChunkFormatV3 {
		return OrderedHeadBlockFmt
	}

	if chunkfmt == ChunkFormatV3 {
		return UnorderedHeadBlockFmt
	}

	// return the latest head format for all chunkformat >v3
	return UnorderedWithStructuredMetadataHeadBlockFmt
}

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

	symbolizer *symbolizer
	// The finished blocks.
	blocks []block
	// The compressed size of all the blocks
	cutBlockSize int

	// Current in-mem block being appended to.
	head HeadBlock

	format   byte
	encoding Encoding
	headFmt  HeadBlockFmt

	// compressed size of chunk. Set when chunk is cut or while decoding chunk from storage.
	compressedSize int
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

func (hb *headBlock) Format() HeadBlockFmt { return OrderedHeadBlockFmt }

func (hb *headBlock) IsEmpty() bool {
	return len(hb.entries) == 0
}

func (hb *headBlock) Entries() int { return len(hb.entries) }

func (hb *headBlock) UncompressedSize() int { return hb.size }

func (hb *headBlock) Reset() {
	if hb.entries != nil {
		hb.entries = hb.entries[:0]
	}
	hb.size = 0
	hb.mint = 0
	hb.maxt = 0
}

func (hb *headBlock) Bounds() (int64, int64) { return hb.mint, hb.maxt }

// The headBlock does not check for duplicates, and will always return false
func (hb *headBlock) Append(ts int64, line string, _ labels.Labels) (bool, error) {
	if !hb.IsEmpty() && hb.maxt > ts {
		return false, ErrOutOfOrder
	}

	hb.entries = append(hb.entries, entry{t: ts, s: line})
	if hb.mint == 0 || hb.mint > ts {
		hb.mint = ts
	}
	hb.maxt = ts
	hb.size += len(line)

	return false, nil
}

func (hb *headBlock) Serialise(pool WriterPool) ([]byte, error) {
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
func (hb *headBlock) CheckpointBytes(b []byte) ([]byte, error) {
	buf := bytes.NewBuffer(b[:0])
	err := hb.CheckpointTo(buf)
	return buf.Bytes(), err
}

// CheckpointSize returns the estimated size of the headblock checkpoint.
func (hb *headBlock) CheckpointSize() int {
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
func (hb *headBlock) CheckpointTo(w io.Writer) error {
	eb := EncodeBufferPool.Get().(*encbuf)
	defer EncodeBufferPool.Put(eb)

	eb.reset()

	eb.putByte(byte(hb.Format()))
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

func (hb *headBlock) LoadBytes(b []byte) error {
	if len(b) < 1 {
		return nil
	}

	db := decbuf{b: b}

	version := db.byte()
	if db.err() != nil {
		return errors.Wrap(db.err(), "verifying headblock header")
	}
	switch version {
	case ChunkFormatV1, ChunkFormatV2, ChunkFormatV3, ChunkFormatV4:
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

func (hb *headBlock) Convert(version HeadBlockFmt, symbolizer *symbolizer) (HeadBlock, error) {
	if version < UnorderedHeadBlockFmt {
		return hb, nil
	}
	out := version.NewBlock(symbolizer)

	for _, e := range hb.entries {
		if _, err := out.Append(e.t, e.s, e.structuredMetadata); err != nil {
			return nil, err
		}
	}
	return out, nil
}

type entry struct {
	t                  int64
	s                  string
	structuredMetadata labels.Labels
}

// NewMemChunk returns a new in-mem chunk.
func NewMemChunk(chunkFormat byte, enc Encoding, head HeadBlockFmt, blockSize, targetSize int) *MemChunk {
	return newMemChunkWithFormat(chunkFormat, enc, head, blockSize, targetSize)
}

func panicIfInvalidFormat(chunkFmt byte, head HeadBlockFmt) {
	if chunkFmt == ChunkFormatV2 && head != OrderedHeadBlockFmt {
		panic("only OrderedHeadBlockFmt is supported for V2 chunks")
	}
	if chunkFmt == ChunkFormatV4 && head != UnorderedWithStructuredMetadataHeadBlockFmt {
		fmt.Println("received head fmt", head.String())
		panic("only UnorderedWithStructuredMetadataHeadBlockFmt is supported for V4 chunks")
	}
}

// NewMemChunk returns a new in-mem chunk.
func newMemChunkWithFormat(format byte, enc Encoding, head HeadBlockFmt, blockSize, targetSize int) *MemChunk {
	panicIfInvalidFormat(format, head)

	symbolizer := newSymbolizer()
	return &MemChunk{
		blockSize:  blockSize,  // The blockSize in bytes.
		targetSize: targetSize, // Desired chunk size in compressed bytes
		blocks:     []block{},

		format: format,
		head:   head.NewBlock(symbolizer),

		encoding:   enc,
		headFmt:    head,
		symbolizer: symbolizer,
	}
}

// NewByteChunk returns a MemChunk on the passed bytes.
func NewByteChunk(b []byte, blockSize, targetSize int) (*MemChunk, error) {
	return newByteChunk(b, blockSize, targetSize, false)
}

func newByteChunk(b []byte, blockSize, targetSize int, fromCheckpoint bool) (*MemChunk, error) {
	bc := &MemChunk{
		head:           &headBlock{}, // Dummy, empty headblock.
		blockSize:      blockSize,
		targetSize:     targetSize,
		symbolizer:     newSymbolizer(),
		compressedSize: len(b),
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
	case ChunkFormatV1:
		bc.encoding = EncGZIP
	case ChunkFormatV2, ChunkFormatV3, ChunkFormatV4:
		// format v2+ has a byte for block encoding.
		enc := Encoding(db.byte())
		if db.err() != nil {
			return nil, errors.Wrap(db.err(), "verifying encoding")
		}
		bc.encoding = enc
	default:
		return nil, errors.Errorf("invalid version %d", version)
	}

	// Set the correct headblock format based on chunk format
	bc.headFmt = ChunkHeadFormatFor(version)

	// readSectionLenAndOffset reads len and offset for different sections within the chunk.
	// Starting from chunk version 4, we have started writing offset and length of various sections within the chunk.
	// These len and offset pairs would be stored together at the end of the chunk.
	// Considering N stored length and offset pairs, they can be referenced by index starting from [1-N]
	// where 1 would be referring to last entry, 2 would be referring to last 2nd entry and so on.
	readSectionLenAndOffset := func(idx int) (uint64, uint64) {
		lenAndOffsetPos := len(b) - (idx * 16)
		lenAndOffset := b[lenAndOffsetPos : lenAndOffsetPos+16]
		return binary.BigEndian.Uint64(lenAndOffset[:8]), binary.BigEndian.Uint64(lenAndOffset[8:])
	}

	metasOffset := uint64(0)
	metasLen := uint64(0)
	// There is a rare issue where chunks built by Loki have incorrect offset for some blocks which causes Loki to fail to read those chunks.
	// While the root cause is yet to be identified, we will try to read those problematic chunks using the expected offset for blocks calculated using other relative offsets in the chunk.
	expectedBlockOffset := 0
	if version >= ChunkFormatV4 {
		// version >= 4 starts writing length of sections before their offsets
		metasLen, metasOffset = readSectionLenAndOffset(chunkMetasSectionIdx)
		structuredMetadataLength, structuredMetadataOffset := readSectionLenAndOffset(chunkStructuredMetadataSectionIdx)
		expectedBlockOffset = int(structuredMetadataLength + structuredMetadataOffset + 4)
	} else {
		// version <= 3 does not store length of metas. metas are followed by metasOffset + hash and then the chunk ends
		metasOffset = binary.BigEndian.Uint64(b[len(b)-8:])
		metasLen = uint64(len(b)-(8+4)) - metasOffset
		// version 1 writes blocks after version number while version 2 and 3 write blocks after chunk encoding
		expectedBlockOffset = len(b) - len(db.b)
	}
	mb := b[metasOffset : metasOffset+metasLen]
	db = decbuf{b: mb}

	expCRC := binary.BigEndian.Uint32(b[metasOffset+metasLen:])
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
		if version >= ChunkFormatV3 {
			blk.uncompressedSize = db.uvarint()
		}
		l := db.uvarint()

		invalidBlockErr := validateBlock(b, blk.offset, l)
		if invalidBlockErr != nil {
			level.Error(util_log.Logger).Log("msg", "invalid block found", "err", invalidBlockErr)
			// if block is expected to have different offset than what is encoded, see if we get a valid block using expected offset
			if blk.offset != expectedBlockOffset {
				_ = level.Error(util_log.Logger).Log("msg", "block offset does not match expected one, will try reading with expected offset", "actual", blk.offset, "expected", expectedBlockOffset)
				blk.offset = expectedBlockOffset
				if err := validateBlock(b, blk.offset, l); err != nil {
					level.Error(util_log.Logger).Log("msg", "could not find valid block using expected offset", "err", err)
				} else {
					invalidBlockErr = nil
					level.Info(util_log.Logger).Log("msg", "valid block found using expected offset")
				}
			}

			// if the block read with expected offset is still invalid, do not continue further
			if invalidBlockErr != nil {
				if errors.Is(invalidBlockErr, ErrInvalidChecksum) {
					expectedBlockOffset += l + 4
					continue
				}
				return nil, invalidBlockErr
			}
		}

		// next block starts at current block start + current block length + checksum
		expectedBlockOffset = blk.offset + l + 4
		blk.b = b[blk.offset : blk.offset+l]
		bc.blocks = append(bc.blocks, blk)

		// Update the counter used to track the size of cut blocks.
		bc.cutBlockSize += len(blk.b)

		if db.err() != nil {
			return nil, errors.Wrap(db.err(), "decoding block meta")
		}
	}

	if version >= ChunkFormatV4 {
		structuredMetadataLength, structuredMetadataOffset := readSectionLenAndOffset(chunkStructuredMetadataSectionIdx)
		lb := b[structuredMetadataOffset : structuredMetadataOffset+structuredMetadataLength] // structured metadata offset + checksum
		db = decbuf{b: lb}

		expCRC := binary.BigEndian.Uint32(b[structuredMetadataOffset+structuredMetadataLength:])
		if expCRC != db.crc32() {
			return nil, ErrInvalidChecksum
		}

		if fromCheckpoint {
			bc.symbolizer = symbolizerFromCheckpoint(lb)
		} else {
			symbolizer, err := symbolizerFromEnc(lb, GetReaderPool(bc.encoding))
			if err != nil {
				return nil, err
			}
			bc.symbolizer = symbolizer
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
	if c.format > ChunkFormatV1 {
		size++ // chunk format v2+ has a byte for encoding.
	}

	// blocks
	for _, b := range c.blocks {
		size += len(b.b) + crc32.Size // size + crc

		size += binary.MaxVarintLen32 // num entries
		size += binary.MaxVarintLen64 // mint
		size += binary.MaxVarintLen64 // maxt
		size += binary.MaxVarintLen32 // offset
		if c.format >= ChunkFormatV3 {
			size += binary.MaxVarintLen32 // uncompressed size
		}
		size += binary.MaxVarintLen32 // len(b)
	}

	// blockmeta
	size += binary.MaxVarintLen32 // len  blocks

	size += crc32.Size // metablock crc
	size += 8          // metaoffset

	if c.format >= ChunkFormatV4 {
		size += 8 // metablock length

		size += c.symbolizer.CheckpointSize() // structured metadata block
		size += crc32.Size                    // structured metadata block crc

		size += 8 + 8 // structured metadata offset and length
	}
	return size
}

func (c *MemChunk) WriteTo(w io.Writer) (int64, error) {
	return c.writeTo(w, false)
}

// WriteTo Implements io.WriterTo
// NOTE: Does not cut head block or include any head block data.
// For this to be the case you must call Close() first.
// This decision notably enables WAL checkpointing, which would otherwise
// result in different content addressable chunks in storage based on the timing of when
// they were checkpointed (which would cause new blocks to be cut early).
func (c *MemChunk) writeTo(w io.Writer, forCheckpoint bool) (int64, error) {
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
	if c.format > ChunkFormatV1 {
		// chunk format v2+ has a byte for encoding.
		eb.putByte(byte(c.encoding))
	}

	n, err := w.Write(eb.get())
	if err != nil {
		return offset, errors.Wrap(err, "write blockMeta #entries")
	}
	offset += int64(n)
	structuredMetadataOffset := offset
	structuredMetadataLength := 0

	if c.format >= ChunkFormatV4 {
		var (
			n       int
			crcHash []byte
		)
		if forCheckpoint {
			var err error
			n, crcHash, err = c.symbolizer.CheckpointTo(w)
			if err != nil {
				return offset, errors.Wrap(err, "write structured metadata")
			}
		} else {
			var err error
			n, crcHash, err = c.symbolizer.SerializeTo(w, GetWriterPool(c.encoding))
			if err != nil {
				return offset, errors.Wrap(err, "write structured metadata")
			}
		}
		offset += int64(n)
		structuredMetadataLength = n

		n, err = w.Write(crcHash)
		if err != nil {
			return offset, errors.Wrap(err, "write crc32 hash for structured metadata")
		}
		offset += int64(n)
	}

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
		if c.format >= ChunkFormatV3 {
			eb.putUvarint(b.uncompressedSize)
		}
		eb.putUvarint(len(b.b))
	}
	metasLen := len(eb.get())
	eb.putHash(crc32Hash)

	n, err = w.Write(eb.get())
	if err != nil {
		return offset, errors.Wrap(err, "write block metas")
	}
	offset += int64(n)

	if c.format >= ChunkFormatV4 {
		// Write structured metadata offset and length
		eb.reset()
		eb.putBE64int(structuredMetadataLength)
		eb.putBE64int(int(structuredMetadataOffset))
		n, err = w.Write(eb.get())
		if err != nil {
			return offset, errors.Wrap(err, "write structured metadata offset and length")
		}
		offset += int64(n)
	}

	// Write the metasOffset.
	eb.reset()
	if c.format >= ChunkFormatV4 {
		eb.putBE64int(metasLen)
	}
	eb.putBE64int(int(metasOffset))
	n, err = w.Write(eb.get())
	if err != nil {
		return offset, errors.Wrap(err, "write metasOffset")
	}
	offset += int64(n)

	c.compressedSize = int(offset)
	return offset, nil
}

// SerializeForCheckpointTo serialize the chunk & head into different `io.Writer` for checkpointing use.
// This is to ensure eventually flushed chunks don't have different substructures depending on when they were checkpointed.
// In turn this allows us to maintain a more effective dedupe ratio in storage.
func (c *MemChunk) SerializeForCheckpointTo(chk, head io.Writer) error {
	// serialize the head before the MemChunk because:
	// * We store structured metadata with chunks(using symbolizer) which are then referenced by blocks and head.
	// * When a write request is received with some new labels of structured metadata, we update symbolizer first and then append log entry to head.
	// * Labels stored in symbolizer are serialized with MemChunk.
	// This means if we serialize the MemChunk before the head, we might miss writing some newly added structured metadata labels which are referenced by head.
	err := c.head.CheckpointTo(head)
	if err != nil {
		return err
	}

	_, err = c.writeTo(chk, true)
	return err
}

func (c *MemChunk) CheckpointSize() (chunk, head int) {
	return c.BytesSize(), c.head.CheckpointSize()
}

func MemchunkFromCheckpoint(chk, head []byte, desiredIfNotUnordered HeadBlockFmt, blockSize int, targetSize int) (*MemChunk, error) {
	mc, err := newByteChunk(chk, blockSize, targetSize, true)
	if err != nil {
		return nil, err
	}
	h, err := HeadFromCheckpoint(head, desiredIfNotUnordered, mc.symbolizer)
	if err != nil {
		return nil, err
	}

	mc.head = h
	mc.headFmt = h.Format()
	return mc, nil
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

	ne += c.head.Entries()

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
		newHBSize := c.head.UncompressedSize() + len(e.Line)
		structuredMetadataSize := 0
		if c.format >= ChunkFormatV4 {
			newHBSize += metaLabelsLen(logproto.FromLabelAdaptersToLabels(e.StructuredMetadata))
			// structured metadata is compressed while serializing the chunk so we don't know what their size would be after compression.
			// As adoption increases, their overall size can be non-trivial so we can't ignore them while calculating chunk size.
			// ToDo(Sandeep): See if we can just use some average compression ratio for each compression format we support and use it here
			structuredMetadataSize = c.symbolizer.UncompressedSize()
		}
		return (structuredMetadataSize + c.cutBlockSize + newHBSize) < c.targetSize
	}
	// if targetSize is not defined, default to the original behavior of fixed blocks per chunk
	return len(c.blocks) < blocksPerChunk
}

// UncompressedSize implements Chunk.
func (c *MemChunk) UncompressedSize() int {
	size := 0

	size += c.head.UncompressedSize()

	for _, b := range c.blocks {
		size += b.uncompressedSize
	}

	if c.format >= ChunkFormatV4 {
		size += c.symbolizer.UncompressedSize()
	}

	return size
}

// CompressedSize implements Chunk.
func (c *MemChunk) CompressedSize() int {
	if c.compressedSize != 0 {
		return c.compressedSize
	}

	size := 0
	// Better to account for any uncompressed data than ignore it even though this isn't accurate.
	size += c.head.UncompressedSize()
	if c.format >= ChunkFormatV4 {
		size += c.symbolizer.UncompressedSize() // length of each symbol
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
// The MemChunk may return true or false, depending on what the head block returns.
func (c *MemChunk) Append(entry *logproto.Entry) (bool, error) {
	entryTimestamp := entry.Timestamp.UnixNano()

	// If the head block is empty but there are cut blocks, we have to make
	// sure the new entry is not out of order compared to the previous block
	if c.headFmt < UnorderedHeadBlockFmt && c.head.IsEmpty() && len(c.blocks) > 0 && c.blocks[len(c.blocks)-1].maxt > entryTimestamp {
		return false, ErrOutOfOrder
	}

	if c.format < ChunkFormatV4 {
		entry.StructuredMetadata = nil
	}
	dup, err := c.head.Append(entryTimestamp, entry.Line, logproto.FromLabelAdaptersToLabels(entry.StructuredMetadata))
	if err != nil {
		return dup, err
	}

	if c.head.UncompressedSize() >= c.blockSize {
		return false, c.cut()
	}

	return dup, nil
}

// Close implements Chunk.
// TODO: Fix this to check edge cases.
func (c *MemChunk) Close() error {
	if err := c.cut(); err != nil {
		return err
	}
	return c.reorder()
}

// reorder ensures all blocks in a chunk are in
// monotonically increasing order.
// This mutates
func (c *MemChunk) reorder() error {
	var lastMax int64 // placeholder to check order across blocks
	ordered := true
	for _, b := range c.blocks {
		if b.mint < lastMax {
			ordered = false
		}
		lastMax = b.maxt
	}

	if ordered {
		return nil
	}

	// Otherwise, we need to rebuild the blocks
	from, to := c.Bounds()
	newC, err := c.Rebound(from, to, nil)
	if err != nil {
		return err
	}
	*c = *newC.(*MemChunk)
	return nil
}

func (c *MemChunk) ConvertHead(desired HeadBlockFmt) error {
	if c.head != nil && c.head.Format() != desired {
		newH, err := c.head.Convert(desired, c.symbolizer)
		if err != nil {
			return err
		}

		c.head = newH
	}
	c.headFmt = desired
	return nil
}

// cut a new block and add it to finished blocks.
func (c *MemChunk) cut() error {
	if c.head.IsEmpty() {
		return nil
	}

	b, err := c.head.Serialise(GetWriterPool(c.encoding))
	if err != nil {
		return err
	}

	mint, maxt := c.head.Bounds()
	c.blocks = append(c.blocks, block{
		b:                b,
		numEntries:       c.head.Entries(),
		mint:             mint,
		maxt:             maxt,
		uncompressedSize: c.head.UncompressedSize(),
	})

	c.cutBlockSize += len(b)

	c.head.Reset()
	return nil
}

// Bounds implements Chunk.
func (c *MemChunk) Bounds() (fromT, toT time.Time) {
	from, to := c.head.Bounds()

	// need to check all the blocks in case they overlap
	for _, b := range c.blocks {
		if from == 0 || from > b.mint {
			from = b.mint
		}
		if to < b.maxt {
			to = b.maxt
		}
	}

	return time.Unix(0, from), time.Unix(0, to)
}

// Iterator implements Chunk.
func (c *MemChunk) Iterator(ctx context.Context, mintT, maxtT time.Time, direction logproto.Direction, pipeline log.StreamPipeline) (iter.EntryIterator, error) {
	mint, maxt := mintT.UnixNano(), maxtT.UnixNano()
	blockItrs := make([]iter.EntryIterator, 0, len(c.blocks)+1)

	if c.format >= ChunkFormatV4 {
		stats := stats.FromContext(ctx)
		stats.AddCompressedBytes(int64(c.symbolizer.CompressedSize()))
		decompressedSize := int64(c.symbolizer.DecompressedSize())
		stats.AddDecompressedBytes(decompressedSize)
		stats.AddDecompressedStructuredMetadataBytes(decompressedSize)
	}
	var headIterator iter.EntryIterator

	var lastMax int64 // placeholder to check order across blocks
	ordered := true
	for _, b := range c.blocks {

		// skip this block
		if maxt < b.mint || b.maxt < mint {
			continue
		}

		if b.mint < lastMax {
			ordered = false
		}
		lastMax = b.maxt

		blockItrs = append(blockItrs, encBlock{c.encoding, c.format, c.symbolizer, b}.Iterator(ctx, pipeline))
	}

	if !c.head.IsEmpty() {
		from, _ := c.head.Bounds()
		if from < lastMax {
			ordered = false
		}
		headIterator = c.head.Iterator(ctx, direction, mint, maxt, pipeline)
	}

	if direction == logproto.FORWARD {
		// add the headblock iterator at the end.
		if headIterator != nil {
			blockItrs = append(blockItrs, headIterator)
		}

		var it iter.EntryIterator
		if ordered {
			it = iter.NewNonOverlappingIterator(blockItrs)
		} else {
			it = iter.NewSortEntryIterator(blockItrs, direction)
		}

		return iter.NewTimeRangedIterator(
			it,
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

	if ordered {
		return iter.NewNonOverlappingIterator(blockItrs), nil
	}
	return iter.NewSortEntryIterator(blockItrs, direction), nil
}

// Iterator implements Chunk.
func (c *MemChunk) SampleIterator(ctx context.Context, from, through time.Time, extractor log.StreamSampleExtractor) iter.SampleIterator {
	mint, maxt := from.UnixNano(), through.UnixNano()
	its := make([]iter.SampleIterator, 0, len(c.blocks)+1)

	if c.format >= ChunkFormatV4 {
		stats := stats.FromContext(ctx)
		stats.AddCompressedBytes(int64(c.symbolizer.CompressedSize()))
		decompressedSize := int64(c.symbolizer.DecompressedSize())
		stats.AddDecompressedBytes(decompressedSize)
		stats.AddDecompressedStructuredMetadataBytes(decompressedSize)
	}

	var lastMax int64 // placeholder to check order across blocks
	ordered := true
	for _, b := range c.blocks {
		// skip this block
		if maxt < b.mint || b.maxt < mint {
			continue
		}

		if b.mint < lastMax {
			ordered = false
		}
		lastMax = b.maxt
		its = append(its, encBlock{c.encoding, c.format, c.symbolizer, b}.SampleIterator(ctx, extractor))
	}

	if !c.head.IsEmpty() {
		from, _ := c.head.Bounds()
		if from < lastMax {
			ordered = false
		}
		its = append(its, c.head.SampleIterator(ctx, mint, maxt, extractor))
	}

	var it iter.SampleIterator
	if ordered {
		it = iter.NewNonOverlappingSampleIterator(its)
	} else {
		it = iter.NewSortSampleIterator(its)
	}

	return iter.NewTimeRangedSampleIterator(
		it,
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
			blocks = append(blocks, encBlock{c.encoding, c.format, c.symbolizer, b})
		}
	}
	return blocks
}

// Rebound builds a smaller chunk with logs having timestamp from start and end(both inclusive)
func (c *MemChunk) Rebound(start, end time.Time, filter filter.Func) (Chunk, error) {
	// add a millisecond to end time because the Chunk.Iterator considers end time to be non-inclusive.
	itr, err := c.Iterator(context.Background(), start, end.Add(time.Millisecond), logproto.FORWARD, log.NewNoopPipeline().ForStream(labels.Labels{}))
	if err != nil {
		return nil, err
	}

	var newChunk *MemChunk
	// as close as possible, respect the block/target sizes specified. However,
	// if the blockSize is not set, use reasonable defaults.
	if c.blockSize > 0 {
		newChunk = NewMemChunk(c.format, c.Encoding(), c.headFmt, c.blockSize, c.targetSize)
	} else {
		// Using defaultBlockSize for target block size.
		// The alternative here could be going over all the blocks and using the size of the largest block as target block size but I(Sandeep) feel that it is not worth the complexity.
		// For target chunk size I am using compressed size of original chunk since the newChunk should anyways be lower in size than that.
		newChunk = NewMemChunk(c.format, c.Encoding(), c.headFmt, defaultBlockSize, c.CompressedSize())
	}

	for itr.Next() {
		entry := itr.At()
		if filter != nil && filter(entry.Timestamp, entry.Line, logproto.FromLabelAdaptersToLabels(entry.StructuredMetadata)...) {
			continue
		}
		if _, err := newChunk.Append(&entry); err != nil {
			return nil, err
		}
	}

	if newChunk.Size() == 0 {
		return nil, chunk.ErrSliceNoDataInRange
	}

	if err := newChunk.Close(); err != nil {
		return nil, err
	}

	return newChunk, nil
}

// encBlock is an internal wrapper for a block, mainly to avoid binding an encoding in a block itself.
// This may seem roundabout, but the encoding is already a field on the parent MemChunk type. encBlock
// then allows us to bind a decoding context to a block when requested, but otherwise helps reduce the
// chances of chunk<>block encoding drift in the codebase as the latter is parameterized by the former.
type encBlock struct {
	enc        Encoding
	format     byte
	symbolizer *symbolizer
	block
}

func (b encBlock) Iterator(ctx context.Context, pipeline log.StreamPipeline) iter.EntryIterator {
	if len(b.b) == 0 {
		return iter.NoopEntryIterator
	}
	return newEntryIterator(ctx, GetReaderPool(b.enc), b.b, pipeline, b.format, b.symbolizer)
}

func (b encBlock) SampleIterator(ctx context.Context, extractor log.StreamSampleExtractor) iter.SampleIterator {
	if len(b.b) == 0 {
		return iter.NoopSampleIterator
	}
	return newSampleIterator(ctx, GetReaderPool(b.enc), b.b, b.format, extractor, b.symbolizer)
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

func (hb *headBlock) Iterator(ctx context.Context, direction logproto.Direction, mint, maxt int64, pipeline log.StreamPipeline) iter.EntryIterator {
	if hb.IsEmpty() || (maxt < hb.mint || hb.maxt < mint) {
		return iter.NoopEntryIterator
	}

	stats := stats.FromContext(ctx)

	// We are doing a copy everytime, this is because b.entries could change completely,
	// the alternate would be that we allocate a new b.entries everytime we cut a block,
	// but the tradeoff is that queries to near-realtime data would be much lower than
	// cutting of blocks.
	stats.AddHeadChunkLines(int64(len(hb.entries)))
	streams := map[string]*logproto.Stream{}
	baseHash := pipeline.BaseLabels().Hash()
	process := func(e entry) {
		// apply time filtering
		if e.t < mint || e.t >= maxt {
			return
		}
		stats.AddHeadChunkBytes(int64(len(e.s)))
		newLine, parsedLbs, matches := pipeline.ProcessString(e.t, e.s, e.structuredMetadata...)
		if !matches {
			return
		}
		stats.AddPostFilterLines(1)
		var stream *logproto.Stream
		labels := parsedLbs.String()
		var ok bool
		if stream, ok = streams[labels]; !ok {
			stream = &logproto.Stream{
				Labels: labels,
				Hash:   baseHash,
			}
			streams[labels] = stream
		}
		stream.Entries = append(stream.Entries, logproto.Entry{
			Timestamp:          time.Unix(0, e.t),
			Line:               newLine,
			StructuredMetadata: logproto.FromLabelsToLabelAdapters(e.structuredMetadata),
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

	if pipeline.ReferencedStructuredMetadata() {
		stats.SetQueryReferencedStructuredMetadata()
	}

	if len(streams) == 0 {
		return iter.NoopEntryIterator
	}
	streamsResult := make([]logproto.Stream, 0, len(streams))
	for _, stream := range streams {
		streamsResult = append(streamsResult, *stream)
	}
	return iter.NewStreamsIterator(streamsResult, direction)
}

func (hb *headBlock) SampleIterator(ctx context.Context, mint, maxt int64, extractor log.StreamSampleExtractor) iter.SampleIterator {
	if hb.IsEmpty() || (maxt < hb.mint || hb.maxt < mint) {
		return iter.NoopSampleIterator
	}
	stats := stats.FromContext(ctx)
	stats.AddHeadChunkLines(int64(len(hb.entries)))
	series := map[string]*logproto.Series{}
	baseHash := extractor.BaseLabels().Hash()

	for _, e := range hb.entries {
		stats.AddHeadChunkBytes(int64(len(e.s)))
		value, parsedLabels, ok := extractor.ProcessString(e.t, e.s, e.structuredMetadata...)
		if !ok {
			continue
		}
		stats.AddPostFilterLines(1)
		var (
			found bool
			s     *logproto.Series
		)

		lbs := parsedLabels.String()
		if s, found = series[lbs]; !found {
			s = &logproto.Series{
				Labels:     lbs,
				Samples:    SamplesPool.Get(len(hb.entries)).([]logproto.Sample)[:0],
				StreamHash: baseHash,
			}
			series[lbs] = s
		}

		s.Samples = append(s.Samples, logproto.Sample{
			Timestamp: e.t,
			Value:     value,
			Hash:      xxhash.Sum64(unsafeGetBytes(e.s)),
		})
	}

	if extractor.ReferencedStructuredMetadata() {
		stats.SetQueryReferencedStructuredMetadata()
	}
	if len(series) == 0 {
		return iter.NoopSampleIterator
	}
	seriesRes := make([]logproto.Series, 0, len(series))
	for _, s := range series {
		seriesRes = append(seriesRes, *s)
	}
	return iter.SampleIteratorWithClose(iter.NewMultiSeriesIterator(seriesRes), func() error {
		for _, s := range series {
			SamplesPool.Put(s.Samples)
		}
		return nil
	})
}

func unsafeGetBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

type bufferedIterator struct {
	origBytes []byte
	stats     *stats.Context

	reader     io.Reader
	pool       ReaderPool
	symbolizer *symbolizer

	err error

	readBuf      [20]byte // Enough bytes to store two varints.
	readBufValid int      // How many bytes are left in readBuf from previous read.

	format   byte
	buf      []byte // The buffer for a single entry.
	currLine []byte // the current line, this is the same as the buffer but sliced the line size.
	currTs   int64

	symbolsBuf             []symbol      // The buffer for a single entry's symbols.
	currStructuredMetadata labels.Labels // The current labels.

	closed bool
}

func newBufferedIterator(ctx context.Context, pool ReaderPool, b []byte, format byte, symbolizer *symbolizer) *bufferedIterator {
	stats := stats.FromContext(ctx)
	stats.AddCompressedBytes(int64(len(b)))
	return &bufferedIterator{
		stats:      stats,
		origBytes:  b,
		reader:     nil, // will be initialized later
		pool:       pool,
		format:     format,
		symbolizer: symbolizer,
	}
}

func (si *bufferedIterator) Next() bool {
	if si.closed {
		return false
	}

	if !si.closed && si.reader == nil {
		// initialize reader now, hopefully reusing one of the previous readers
		var err error
		si.reader, err = si.pool.GetReader(bytes.NewBuffer(si.origBytes))
		if err != nil {
			si.err = err
			return false
		}
	}

	ts, line, structuredMetadata, ok := si.moveNext()
	if !ok {
		si.Close()
		return false
	}

	si.currTs = ts
	si.currLine = line
	si.currStructuredMetadata = structuredMetadata
	return true
}

// moveNext moves the buffer to the next entry
func (si *bufferedIterator) moveNext() (int64, []byte, labels.Labels, bool) {
	var decompressedBytes int64
	var decompressedStructuredMetadataBytes int64
	var ts int64
	var tWidth, lWidth, lineSize, lastAttempt int
	for lWidth == 0 { // Read until both varints have enough bytes.
		n, err := si.reader.Read(si.readBuf[si.readBufValid:])
		si.readBufValid += n
		if err != nil {
			if err != io.EOF {
				si.err = err
				return 0, nil, nil, false
			}
			if si.readBufValid == 0 { // Got EOF and no data in the buffer.
				return 0, nil, nil, false
			}
			if si.readBufValid == lastAttempt { // Got EOF and could not parse same data last time.
				si.err = fmt.Errorf("invalid data in chunk")
				return 0, nil, nil, false
			}
		}
		var l uint64
		ts, tWidth = binary.Varint(si.readBuf[:si.readBufValid])
		l, lWidth = binary.Uvarint(si.readBuf[tWidth:si.readBufValid])
		lineSize = int(l)
		lastAttempt = si.readBufValid
	}

	// TS and line length
	decompressedBytes += 2 * binary.MaxVarintLen64

	if lineSize >= maxLineLength {
		si.err = fmt.Errorf("line too long %d, maximum %d", lineSize, maxLineLength)
		return 0, nil, nil, false
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
			return 0, nil, nil, false
		}
	}
	si.buf = si.buf[:lineSize]
	// Take however many bytes are left in the read buffer.
	n := copy(si.buf, si.readBuf[tWidth+lWidth:si.readBufValid])
	// Shift down what is still left in the fixed-size read buffer, if any.
	si.readBufValid = copy(si.readBuf[:], si.readBuf[tWidth+lWidth+n:si.readBufValid])

	// Then process reading the line.
	for n < lineSize {
		r, err := si.reader.Read(si.buf[n:lineSize])
		n += r
		if err != nil {
			// We might get EOF after reading enough bytes to fill the buffer, which is OK.
			// EOF and zero bytes read when the buffer isn't full is an error.
			if err == io.EOF && r != 0 {
				continue
			}
			si.err = err
			return 0, nil, nil, false
		}
	}

	decompressedBytes += int64(lineSize)

	if si.format < ChunkFormatV4 {
		si.stats.AddDecompressedBytes(decompressedBytes)
		si.stats.AddDecompressedLines(1)
		return ts, si.buf[:lineSize], nil, true
	}

	lastAttempt = 0
	var symbolsSectionLengthWidth, nSymbolsWidth, nSymbols int
	for nSymbolsWidth == 0 { // Read until we have enough bytes for the labels.
		n, err := si.reader.Read(si.readBuf[si.readBufValid:])
		si.readBufValid += n
		if err != nil {
			if err != io.EOF {
				si.err = err
				return 0, nil, nil, false
			}
			if si.readBufValid == 0 { // Got EOF and no data in the buffer.
				return 0, nil, nil, false
			}
			if si.readBufValid == lastAttempt { // Got EOF and could not parse same data last time.
				si.err = fmt.Errorf("invalid data in chunk")
				return 0, nil, nil, false
			}
		}
		var l uint64
		_, symbolsSectionLengthWidth = binary.Uvarint(si.readBuf[:si.readBufValid])
		l, nSymbolsWidth = binary.Uvarint(si.readBuf[symbolsSectionLengthWidth:si.readBufValid])
		nSymbols = int(l)
		lastAttempt = si.readBufValid
	}

	// Number of labels
	decompressedStructuredMetadataBytes += binary.MaxVarintLen64
	// Label symbols
	decompressedStructuredMetadataBytes += int64(nSymbols * 2 * binary.MaxVarintLen64)

	// Shift down what is still left in the fixed-size read buffer, if any.
	si.readBufValid = copy(si.readBuf[:], si.readBuf[symbolsSectionLengthWidth+nSymbolsWidth:si.readBufValid])

	/*
		Commented out tested code, which lets us skip reading the symbols section altogether.
		Leaving it here if in case we need it in future.

		symbolsSectionLength -= nSymbolsWidth
		if symbolsSectionLength > 0 {
			readBufValid := si.readBufValid
			if symbolsSectionLength >= si.readBufValid {
				si.readBufValid = 0
			} else {
				si.readBufValid = copy(si.readBuf[:], si.readBuf[symbolsSectionLength:si.readBufValid])
			}
			symbolsSectionLength -= readBufValid - si.readBufValid
			if symbolsSectionLength > 0 {
				_, err := si.reader.Read(make([]byte, symbolsSectionLength))
				if err != nil {
					si.err = err
					return 0, nil, nil, false
				}
			}
			nSymbols = 0
		}
	*/

	// If not enough space for the symbols, create a new buffer slice and put the old one back in the pool.
	if nSymbols > cap(si.symbolsBuf) {
		if si.symbolsBuf != nil {
			SymbolsPool.Put(si.symbolsBuf)
		}
		si.symbolsBuf = SymbolsPool.Get(nSymbols).([]symbol)
		if nSymbols > cap(si.symbolsBuf) {
			si.err = fmt.Errorf("could not get a symbols matrix of size %d, actual %d", nSymbols, cap(si.symbolsBuf))
			return 0, nil, nil, false
		}
	}

	si.symbolsBuf = si.symbolsBuf[:nSymbols]

	// Read all the symbols, into the buffer.
	for i := 0; i < nSymbols; i++ {
		var sName, sValue uint64
		var nWidth, vWidth, lastAttempt int
		for vWidth == 0 { // Read until both varints have enough bytes.
			n, err := si.reader.Read(si.readBuf[si.readBufValid:])
			si.readBufValid += n
			if err != nil {
				if err != io.EOF {
					si.err = err
					return 0, nil, nil, false
				}
				if si.readBufValid == 0 { // Got EOF and no data in the buffer.
					return 0, nil, nil, false
				}
				if si.readBufValid == lastAttempt { // Got EOF and could not parse same data last time.
					si.err = fmt.Errorf("invalid data in chunk")
					return 0, nil, nil, false
				}
			}
			sName, nWidth = binary.Uvarint(si.readBuf[:si.readBufValid])
			sValue, vWidth = binary.Uvarint(si.readBuf[nWidth:si.readBufValid])
			lastAttempt = si.readBufValid
		}

		// Shift down what is still left in the fixed-size read buffer, if any.
		si.readBufValid = copy(si.readBuf[:], si.readBuf[nWidth+vWidth:si.readBufValid])

		si.symbolsBuf[i].Name = uint32(sName)
		si.symbolsBuf[i].Value = uint32(sValue)
	}

	si.stats.AddDecompressedLines(1)
	si.stats.AddDecompressedStructuredMetadataBytes(decompressedStructuredMetadataBytes)
	si.stats.AddDecompressedBytes(decompressedBytes + decompressedStructuredMetadataBytes)

	return ts, si.buf[:lineSize], si.symbolizer.Lookup(si.symbolsBuf[:nSymbols], si.currStructuredMetadata), true
}

func (si *bufferedIterator) Err() error { return si.err }

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

	if si.buf != nil {
		BytesBufferPool.Put(si.buf)
		si.buf = nil
	}

	if si.symbolsBuf != nil {
		SymbolsPool.Put(si.symbolsBuf)
		si.symbolsBuf = nil
	}

	if si.currStructuredMetadata != nil {
		structuredMetadataPool.Put(si.currStructuredMetadata) // nolint:staticcheck
		si.currStructuredMetadata = nil
	}

	si.origBytes = nil
}

func newEntryIterator(ctx context.Context, pool ReaderPool, b []byte, pipeline log.StreamPipeline, format byte, symbolizer *symbolizer) iter.EntryIterator {
	return &entryBufferedIterator{
		bufferedIterator: newBufferedIterator(ctx, pool, b, format, symbolizer),
		pipeline:         pipeline,
		stats:            stats.FromContext(ctx),
	}
}

type entryBufferedIterator struct {
	*bufferedIterator
	pipeline log.StreamPipeline
	stats    *stats.Context

	cur        logproto.Entry
	currLabels log.LabelsResult
}

func (e *entryBufferedIterator) At() logproto.Entry {
	return e.cur
}

func (e *entryBufferedIterator) Labels() string { return e.currLabels.String() }

func (e *entryBufferedIterator) StreamHash() uint64 { return e.pipeline.BaseLabels().Hash() }

func (e *entryBufferedIterator) Next() bool {
	for e.bufferedIterator.Next() {
		newLine, lbs, matches := e.pipeline.Process(e.currTs, e.currLine, e.currStructuredMetadata...)
		if !matches {
			continue
		}

		e.stats.AddPostFilterLines(1)
		e.currLabels = lbs
		e.cur.Timestamp = time.Unix(0, e.currTs)
		e.cur.Line = string(newLine)
		e.cur.StructuredMetadata = logproto.FromLabelsToLabelAdapters(lbs.StructuredMetadata())
		e.cur.Parsed = logproto.FromLabelsToLabelAdapters(lbs.Parsed())

		return true
	}
	return false
}

func (e *entryBufferedIterator) Close() error {
	if e.pipeline.ReferencedStructuredMetadata() {
		e.stats.SetQueryReferencedStructuredMetadata()
	}

	return e.bufferedIterator.Close()
}

func newSampleIterator(ctx context.Context, pool ReaderPool, b []byte, format byte, extractor log.StreamSampleExtractor, symbolizer *symbolizer) iter.SampleIterator {
	return &sampleBufferedIterator{
		bufferedIterator: newBufferedIterator(ctx, pool, b, format, symbolizer),
		extractor:        extractor,
		stats:            stats.FromContext(ctx),
	}
}

type sampleBufferedIterator struct {
	*bufferedIterator

	extractor log.StreamSampleExtractor
	stats     *stats.Context

	cur        logproto.Sample
	currLabels log.LabelsResult
}

func (e *sampleBufferedIterator) Next() bool {
	for e.bufferedIterator.Next() {
		val, labels, ok := e.extractor.Process(e.currTs, e.currLine, e.currStructuredMetadata...)
		if !ok {
			continue
		}
		e.stats.AddPostFilterLines(1)
		e.currLabels = labels
		e.cur.Value = val
		e.cur.Hash = xxhash.Sum64(e.currLine)
		e.cur.Timestamp = e.currTs
		return true
	}
	return false
}

func (e *sampleBufferedIterator) Close() error {
	if e.extractor.ReferencedStructuredMetadata() {
		e.stats.SetQueryReferencedStructuredMetadata()
	}

	return e.bufferedIterator.Close()
}

func (e *sampleBufferedIterator) Labels() string { return e.currLabels.String() }

func (e *sampleBufferedIterator) StreamHash() uint64 { return e.extractor.BaseLabels().Hash() }

func (e *sampleBufferedIterator) At() logproto.Sample {
	return e.cur
}

// validateBlock validates block by doing following checks:
// 1. Offset+length do not overrun size of the chunk from which we are reading the block.
// 2. Checksum of the block we will read matches the stored checksum in the chunk.
func validateBlock(chunkBytes []byte, offset, length int) error {
	if offset+length > len(chunkBytes) {
		return fmt.Errorf("offset %d + length %d exceeds chunk length %d", offset, length, len(chunkBytes))
	}

	blockBytes := chunkBytes[offset : offset+length]
	// Verify checksums.
	expCRC := binary.BigEndian.Uint32(chunkBytes[offset+length:])
	if expCRC != crc32.Checksum(blockBytes, castagnoliTable) {
		return ErrInvalidChecksum
	}

	return nil
}
