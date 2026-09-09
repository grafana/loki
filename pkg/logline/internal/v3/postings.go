package v3

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"slices"
	"sort"
	"sync"

	"github.com/RoaringBitmap/roaring"
	"github.com/klauspost/compress/zstd"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

// PostingsEncoder serializes term->bitmap postings into a single section payload.
type PostingsEncoder interface {
	Encoding() format.PostingsEncoding
	PackingFactor() uint8
	Encode(postings []format.TermBitmapPostings, documentCount uint32) ([]byte, error)
}

// postingsBlockDirEntry maps a contiguous postings block to a term-id range.
// This entry layout is intentionally identical for fast and BIDX postings.
type postingsBlockDirEntry struct {
	FirstTermID    uint32
	NumTerms       uint32
	BlockOffset    uint64
	CompressedSize uint32
}

// StreamingPostingsEncoder serializes postings directly to a writer.
type StreamingPostingsEncoder interface {
	PostingsEncoder
	streamPostingsData(w io.Writer, postings []format.TermBitmapPostings, documentCount uint32) (int64, []postingsBlockDirEntry, error)
	CompressionType() uint32
}

// PostingsReader supports random access retrieval by term position.
type PostingsReader interface {
	GetBitmap(termIndex int) (format.Bitmap, error)
	// GetDocIDs returns decoded doc IDs for termIndex, reusing buf capacity when
	// possible. matchesAll is true for density-filter sentinels (empty payload).
	GetDocIDs(termIndex int, buf []uint32) (ids []uint32, matchesAll bool, err error)
	Close() error
}

// OpenPostingsReader creates a section reader for a specific encoding.
func OpenPostingsReader(
	encoding format.PostingsEncoding,
	_ uint8, // packingFactor, unused by the encodings this reader supports
	reader io.ReaderAt,
	dataOffset int64,
	dataSize uint64,
	dir []postingsBlockDirEntry,
	termCount uint64,
	_ uint32, // documentCount, unused
	_ uint32, // compressionType, unused
) (PostingsReader, error) {
	switch encoding {
	case format.PostingsEncodingFastDeltaVarIntBlocked:
		return openFastPostingsReaderFromDir(encoding, reader, dataOffset, dataSize, termCount, dir)
	default:
		return nil, fmt.Errorf("unsupported postings encoding: %d", encoding)
	}
}

// ---------------------------------------------------------------------------
// Delta-varint encoding: delta-encode sorted doc IDs, varint-encode each value.
// Compact for sparse terms (77% of terms have ≤5 docs).
// ---------------------------------------------------------------------------

// appendDeltaVarInt delta-encodes sorted docIDs and appends the varint bytes
// to buf. Caller supplies buf (pass nil for a fresh allocation, or a reused
// buffer for amortized encoding); pre-sizes via slices.Grow so the append loop
// never reallocates.
func appendDeltaVarInt(buf []byte, docIDs []uint32) []byte {
	if len(docIDs) == 0 {
		return buf
	}
	buf = slices.Grow(buf, len(docIDs)*3)
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tmp[:], uint64(docIDs[0]))
	buf = append(buf, tmp[:n]...)
	prev := docIDs[0]
	for _, id := range docIDs[1:] {
		delta := id - prev
		n = binary.PutUvarint(tmp[:], uint64(delta))
		buf = append(buf, tmp[:n]...)
		prev = id
	}
	return buf
}

func decodeDeltaVarInt(payload []byte) ([]uint32, error) {
	return decodeDeltaVarIntInto(nil, payload)
}

// decodeDeltaVarIntInto delta-decodes payload into dst, reusing dst capacity.
func decodeDeltaVarIntInto(dst []uint32, payload []byte) ([]uint32, error) {
	if len(payload) == 0 {
		return dst[:0], nil
	}

	docIDs := dst[:0]
	pos := 0
	var prev uint32

	for pos < len(payload) {
		v, n := binary.Uvarint(payload[pos:])
		if n <= 0 {
			return nil, fmt.Errorf("invalid varint at offset %d", pos)
		}
		pos += n
		if len(docIDs) == 0 {
			prev = uint32(v)
		} else {
			prev += uint32(v)
		}
		docIDs = append(docIDs, prev)
	}
	return docIDs, nil
}

// ---------------------------------------------------------------------------
// Fast postings encoder (zstd-compressed blocks of delta-varint payloads)
// ---------------------------------------------------------------------------

const (
	fastPostingsBlockTarget = 4 * 1024 * 1024
)

type FastPostingsEncoder struct {
	encoding    format.PostingsEncoding
	blockTarget int
	session     *fastStreamingSession
}

// fastStreamingSession holds the mutable state for an in-progress streaming
// encode. Lifted from the local variables in streamPostingsData so that the
// same logic can be driven term-by-term via beginStream/writeTerm/endStream.
type fastStreamingSession struct {
	encoder        *zstd.Encoder
	w              io.Writer
	bytesWritten   int64
	dir            []postingsBlockDirEntry
	blockData      bytes.Buffer
	blockOffsets   []uint32
	blockTermCount int
	nextTermID     uint32
	documentCount  uint32
	varintBuf      []byte
}

func NewFastPostingsEncoder(encoding format.PostingsEncoding, blockTarget int) (*FastPostingsEncoder, error) {
	switch encoding {
	case format.PostingsEncodingFastDeltaVarIntBlocked:
	default:
		return nil, fmt.Errorf("invalid fast encoding: %d", encoding)
	}
	if blockTarget <= 0 {
		blockTarget = fastPostingsBlockTarget
	}
	return &FastPostingsEncoder{
		encoding:    encoding,
		blockTarget: blockTarget,
	}, nil
}

func (e *FastPostingsEncoder) Encoding() format.PostingsEncoding {
	return e.encoding
}

func (e *FastPostingsEncoder) PackingFactor() uint8 {
	return 1
}

func (e *FastPostingsEncoder) CompressionType() uint32 {
	return 1
}

// beginStream initialises a streaming session writing compressed postings to w.
// documentCount is stored for encodings that need it (bitset, hybrid-bitset).
// Returns an error if a session is already open.
func (e *FastPostingsEncoder) beginStream(w io.Writer, documentCount uint32) error {
	if e.session != nil {
		return fmt.Errorf("streaming session already open")
	}
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
	if err != nil {
		return fmt.Errorf("create zstd encoder: %w", err)
	}
	e.session = &fastStreamingSession{
		encoder:       enc,
		w:             w,
		blockOffsets:  make([]uint32, 1, 4096),
		documentCount: documentCount,
	}
	e.session.blockOffsets[0] = 0
	return nil
}

// writeTerm encodes a single term's bitmap into the current streaming session.
// bm must not be retained after writeTerm returns — it is serialized immediately.
// A MatchesAll bitmap is stored as a zero-length payload (offsets[i]==offsets[i+1])
// so that GetBitmap returns MatchesAll for that term.
func (e *FastPostingsEncoder) writeTerm(_ [8]byte, bm format.Bitmap) error {
	if bm.MatchesAll || bm.Roaring == nil {
		return e.appendPayload(nil)
	}
	var payload []byte
	switch e.encoding {
	case format.PostingsEncodingFastDeltaVarIntBlocked:
		payload = appendDeltaVarInt(nil, bm.Roaring.ToArray())
	default:
		return fmt.Errorf("unsupported fast postings encoding: %d", e.encoding)
	}
	return e.appendPayload(payload)
}

// writeTermDocIDs encodes a single term's docIDs into the current streaming session,
// bypassing roaring bitmap construction. matchesAll causes a zero-length payload to
// be stored (density-filter sentinel).
func (e *FastPostingsEncoder) writeTermDocIDs(_ [8]byte, docIDs []uint32, matchesAll bool) error {
	if matchesAll || len(docIDs) == 0 {
		return e.appendPayload(nil)
	}
	switch e.encoding {
	case format.PostingsEncodingFastDeltaVarIntBlocked:
		e.session.varintBuf = appendDeltaVarInt(e.session.varintBuf[:0], docIDs)
		return e.appendPayload(e.session.varintBuf)
	default:
		return fmt.Errorf("unsupported fast postings encoding: %d", e.encoding)
	}
}

// endStream finalises the streaming session: flushes the last partial block,
// closes the zstd encoder, and returns the bytes written plus the directory.
func (e *FastPostingsEncoder) endStream() (int64, []postingsBlockDirEntry, error) {
	s := e.session
	e.session = nil
	defer s.encoder.Close()

	if err := e.flushBlock(s); err != nil {
		return 0, nil, err
	}
	return s.bytesWritten, s.dir, nil
}

func (e *FastPostingsEncoder) appendPayload(payload []byte) error {
	s := e.session
	// Account for block header + offset table growth while accumulating.
	offsetTableSize := 4 + 4*(s.blockTermCount+2)
	if s.blockTermCount > 0 && s.blockData.Len()+len(payload)+offsetTableSize >= e.blockTarget {
		if err := e.flushBlock(s); err != nil {
			return err
		}
	}
	if _, err := s.blockData.Write(payload); err != nil {
		return fmt.Errorf("write block payload: %w", err)
	}
	s.blockOffsets = append(s.blockOffsets, uint32(s.blockData.Len()))
	s.blockTermCount++
	return nil
}

func (e *FastPostingsEncoder) flushBlock(s *fastStreamingSession) error {
	if s.blockTermCount == 0 {
		return nil
	}

	var raw bytes.Buffer
	if err := binary.Write(&raw, binary.LittleEndian, uint32(s.blockTermCount)); err != nil {
		return fmt.Errorf("write term count: %w", err)
	}
	for _, off := range s.blockOffsets {
		if err := binary.Write(&raw, binary.LittleEndian, off); err != nil {
			return fmt.Errorf("write term offsets: %w", err)
		}
	}
	if _, err := raw.Write(s.blockData.Bytes()); err != nil {
		return fmt.Errorf("write block data: %w", err)
	}

	compressed := s.encoder.EncodeAll(raw.Bytes(), nil)
	entry := postingsBlockDirEntry{
		FirstTermID:    s.nextTermID,
		NumTerms:       uint32(s.blockTermCount),
		BlockOffset:    uint64(s.bytesWritten),
		CompressedSize: uint32(4 + len(compressed)),
	}
	if entry.CompressedSize > format.MaxQueryRequestBytes {
		return fmt.Errorf("compressed postings block exceeds request cap: %d", entry.CompressedSize)
	}

	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(compressed)))
	if err := writeFull(s.w, &s.bytesWritten, lenBuf[:]); err != nil {
		return fmt.Errorf("write compressed block length: %w", err)
	}
	if err := writeFull(s.w, &s.bytesWritten, compressed); err != nil {
		return fmt.Errorf("write compressed block payload: %w", err)
	}
	s.dir = append(s.dir, entry)

	s.nextTermID += uint32(s.blockTermCount)
	s.blockData.Reset()
	s.blockOffsets = s.blockOffsets[:1]
	s.blockOffsets[0] = 0
	s.blockTermCount = 0
	return nil
}

// writeFull writes all of p to w, updating bytesWritten.
func writeFull(w io.Writer, bytesWritten *int64, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		*bytesWritten += int64(n)
		p = p[n:]
	}
	return nil
}

func (e *FastPostingsEncoder) streamPostingsData(
	w io.Writer,
	postings []format.TermBitmapPostings,
	documentCount uint32,
) (int64, []postingsBlockDirEntry, error) {
	if err := e.beginStream(w, documentCount); err != nil {
		return 0, nil, err
	}
	for _, posting := range postings {
		if err := e.writeTerm(posting.Term, format.Bitmap{Roaring: posting.Bitmap}); err != nil {
			// Release encoder resources explicitly rather than relying on GC.
			_, _, _ = e.endStream()
			return 0, nil, err
		}
	}
	return e.endStream()
}

func (e *FastPostingsEncoder) Encode(postings []format.TermBitmapPostings, _ uint32) ([]byte, error) {
	var out bytes.Buffer
	_, _, err := e.streamPostingsData(&out, postings, 0)
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// ---------------------------------------------------------------------------
// Fast postings reader (zstd-compressed blocks with LRU cache)
// ---------------------------------------------------------------------------

type decodedFastBlock struct {
	offsets []uint32
	data    []byte
}

// maxFastBlockCacheSize bounds the number of decompressed postings blocks held
// in memory. Sequential iteration (streaming merge) only ever needs the current
// block, but we keep a small window so that random-access queries hitting two
// adjacent blocks don't thrash. During a k-way merge of N inputs the total
// cached blocks is at most N * maxFastBlockCacheSize.
const maxFastBlockCacheSize = 2

type fastPostingsReader struct {
	reader     io.ReaderAt
	dataOffset int64
	dataSize   uint64
	encoding   format.PostingsEncoding
	termCount  uint64
	dir        []postingsBlockDirEntry
	decoder    *zstd.Decoder

	cacheMu  sync.Mutex
	cache    map[int]*decodedFastBlock
	cacheOrd []int // insertion order for eviction
}

func openFastPostingsReaderFromDir(
	expectedEncoding format.PostingsEncoding,
	reader io.ReaderAt,
	dataOffset int64,
	dataSize uint64,
	termCount uint64,
	dir []postingsBlockDirEntry,
) (PostingsReader, error) {
	switch expectedEncoding {
	case format.PostingsEncodingFastDeltaVarIntBlocked:
	default:
		return nil, fmt.Errorf("invalid fast postings encoding: %d", expectedEncoding)
	}
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("create zstd decoder: %w", err)
	}
	return &fastPostingsReader{
		reader:     reader,
		dataOffset: dataOffset,
		dataSize:   dataSize,
		encoding:   expectedEncoding,
		termCount:  termCount,
		dir:        dir,
		decoder:    decoder,
		cache:      make(map[int]*decodedFastBlock),
	}, nil
}

func (r *fastPostingsReader) GetBitmap(termIndex int) (format.Bitmap, error) {
	docIDs, matchesAll, err := r.GetDocIDs(termIndex, nil)
	if err != nil {
		return format.Bitmap{}, err
	}
	if matchesAll {
		return format.Bitmap{MatchesAll: true}, nil
	}
	bm := roaring.New()
	bm.AddMany(docIDs)
	return format.Bitmap{Roaring: bm}, nil
}

// GetDocIDs returns decoded doc IDs for termIndex without building a roaring
// bitmap. buf capacity is reused when non-nil. Zero-length payloads are the
// density-filter sentinel (matchesAll=true, ids empty).
func (r *fastPostingsReader) GetDocIDs(termIndex int, buf []uint32) ([]uint32, bool, error) {
	if termIndex < 0 || uint64(termIndex) >= r.termCount {
		return nil, false, fmt.Errorf("term index out of bounds: %d", termIndex)
	}
	blockIdx := sort.Search(len(r.dir), func(i int) bool {
		entry := r.dir[i]
		return uint32(termIndex) < entry.FirstTermID+entry.NumTerms
	})
	if blockIdx >= len(r.dir) {
		return nil, false, fmt.Errorf("failed to locate postings block for term: %d", termIndex)
	}
	entry := r.dir[blockIdx]
	if uint32(termIndex) < entry.FirstTermID {
		return nil, false, fmt.Errorf("invalid postings directory for term: %d", termIndex)
	}

	block, err := r.loadBlock(blockIdx, entry)
	if err != nil {
		return nil, false, err
	}
	rel := termIndex - int(entry.FirstTermID)
	if rel < 0 || rel+1 >= len(block.offsets) {
		return nil, false, fmt.Errorf("term offset out of bounds: %d", termIndex)
	}

	start := block.offsets[rel]
	end := block.offsets[rel+1]
	if end < start || int(end) > len(block.data) {
		return nil, false, fmt.Errorf("invalid term payload bounds")
	}
	payload := block.data[start:end]

	// Zero-length payload is the density-filter sentinel: matches all documents.
	if len(payload) == 0 {
		return buf[:0], true, nil
	}

	switch r.encoding {
	case format.PostingsEncodingFastDeltaVarIntBlocked:
		docIDs, err := decodeDeltaVarIntInto(buf, payload)
		if err != nil {
			return nil, false, fmt.Errorf("decode delta-varint payload: %w", err)
		}
		return docIDs, false, nil
	default:
		return nil, false, fmt.Errorf("unsupported fast postings encoding in reader: %d", r.encoding)
	}
}

func (r *fastPostingsReader) loadBlock(blockIdx int, entry postingsBlockDirEntry) (*decodedFastBlock, error) {
	r.cacheMu.Lock()
	if cached, ok := r.cache[blockIdx]; ok {
		r.cacheMu.Unlock()
		return cached, nil
	}
	r.cacheMu.Unlock()

	if entry.CompressedSize > format.MaxQueryRequestBytes {
		return nil, fmt.Errorf("postings block request exceeds cap: %d", entry.CompressedSize)
	}
	if entry.BlockOffset+uint64(entry.CompressedSize) > r.dataSize {
		return nil, fmt.Errorf("invalid postings block bounds")
	}

	blockBytes := make([]byte, entry.CompressedSize)
	if _, err := r.reader.ReadAt(blockBytes, r.dataOffset+int64(entry.BlockOffset)); err != nil {
		return nil, fmt.Errorf("read postings block: %w", err)
	}

	if len(blockBytes) < 4 {
		return nil, fmt.Errorf("invalid postings block size")
	}
	compressedLen := binary.LittleEndian.Uint32(blockBytes[:4])
	if int(compressedLen)+4 > len(blockBytes) {
		return nil, fmt.Errorf("invalid postings block compressed length")
	}
	raw, err := r.decoder.DecodeAll(blockBytes[4:4+compressedLen], nil)
	if err != nil {
		return nil, fmt.Errorf("decompress postings block: %w", err)
	}
	if len(raw) < 4 {
		return nil, fmt.Errorf("invalid postings raw block")
	}
	numTerms := int(binary.LittleEndian.Uint32(raw[:4]))
	offsetBytes := 4 * (numTerms + 1)
	if len(raw) < 4+offsetBytes {
		return nil, fmt.Errorf("invalid postings offset table")
	}
	offsets := make([]uint32, numTerms+1)
	base := 4
	for i := 0; i < numTerms+1; i++ {
		offsets[i] = binary.LittleEndian.Uint32(raw[base+i*4 : base+(i+1)*4])
	}
	data := raw[4+offsetBytes:]
	block := &decodedFastBlock{
		offsets: offsets,
		data:    data,
	}

	r.cacheMu.Lock()
	// Evict oldest entries when the cache is full.
	for len(r.cache) >= maxFastBlockCacheSize && len(r.cacheOrd) > 0 {
		evict := r.cacheOrd[0]
		r.cacheOrd = r.cacheOrd[1:]
		delete(r.cache, evict)
	}
	r.cache[blockIdx] = block
	r.cacheOrd = append(r.cacheOrd, blockIdx)
	r.cacheMu.Unlock()
	return block, nil
}

func (r *fastPostingsReader) Close() error {
	if r.decoder != nil {
		r.decoder.Close()
	}
	return nil
}
