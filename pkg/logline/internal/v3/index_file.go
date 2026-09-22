package v3

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/klauspost/compress/zstd"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

const (
	// IndexMagic identifies a unified index file.
	IndexMagic uint32 = 0x4C4F474C // "LOGL"
	// IndexVersion is the v3 on-disk format version.
	IndexVersion uint32 = 4
	// IndexFooterSize is the size of the 256-byte footer appended at end of file.
	// v2 files have no header prefix; data starts at offset 0 and the footer is at EOF.
	IndexFooterSize = 256
)

type IndexFooter struct {
	// First half (128B): identity/count fields + reserved.
	Magic               uint32
	Version             uint32
	Flags               uint32
	DocumentCount       uint32
	TermBlockCount      uint32
	PostingsBlockCount  uint32
	PostingsCompression uint32
	Reserved0           uint32
	ReservedMid         [96]byte

	// Second half (128B): size fields + reserved.
	TermCount            uint64
	PostingsDataSize     uint64
	TermDataSize         uint64
	DocMetadataSize      uint64
	TermBlockDirSize     uint64
	PostingsBlockDirSize uint64
	ReservedEnd          [80]byte
}

// Info returns a JSON-friendly header summary.
func (h IndexFooter) Info() format.HeaderInfo {
	return format.HeaderInfo{
		Version:              h.Version,
		Flags:                h.Flags,
		DocumentCount:        h.DocumentCount,
		TermBlockCount:       h.TermBlockCount,
		PostingsBlockCount:   h.PostingsBlockCount,
		PostingsCompression:  h.PostingsCompression,
		TermCount:            h.TermCount,
		PostingsDataSize:     h.PostingsDataSize,
		TermDataSize:         h.TermDataSize,
		DocMetadataSize:      h.DocMetadataSize,
		TermBlockDirSize:     h.TermBlockDirSize,
		PostingsBlockDirSize: h.PostingsBlockDirSize,
	}
}

type IndexWriteConfig struct {
	Encoding         format.PostingsEncoding
	PackingFactor    uint8
	FastBlockTarget  int
	DensityThreshold float32       // 0 = disabled; >0 = filter terms exceeding this fraction of a full day's docs
	DocumentInterval time.Duration // time per document; used with DensityThreshold to compute day-based sentinel cutoff
}

// NewWriter creates a file-backed streaming writer with the given docs,
// applying overrides from cfg on top of v2 defaults. cfg may be nil for
// defaults.
func NewWriter(path string, docs []format.DocumentMetadata, cfg *format.WriterConfig) (*StreamingIndexWriter, error) {
	w, err := newStreamingIndexWriter(path, applyWriterConfig(cfg), uint32(len(docs)))
	if err != nil {
		return nil, err
	}
	w.AddDocuments(docs)
	return w, nil
}

// Merge merges multiple indexes into out with v2 defaults, applying overrides
// from cfg. The caller owns out. Returns the HeaderInfo describing the
// merged index. cfg may be nil for defaults.
func Merge(ctx context.Context, readers []io.ReaderAt, sizes []int64, out io.Writer, cfg *format.WriterConfig) (format.HeaderInfo, error) {
	return StreamingMergeIndexReaders(ctx, readers, sizes, out, applyWriterConfig(cfg))
}

// applyWriterConfig returns a v2 IndexWriteConfig with defaults overridden by
// the non-zero fields of cfg. cfg may be nil.
func applyWriterConfig(cfg *format.WriterConfig) IndexWriteConfig {
	icfg := DefaultFastIndexWriteConfig()
	if cfg == nil {
		return icfg
	}
	if cfg.Encoding != 0 {
		icfg.Encoding = cfg.Encoding
	}
	if cfg.DensityThreshold != 0 {
		icfg.DensityThreshold = cfg.DensityThreshold
	}
	if cfg.DocumentInterval != 0 {
		icfg.DocumentInterval = cfg.DocumentInterval
	}
	return icfg
}

func DefaultFastIndexWriteConfig() IndexWriteConfig {
	return IndexWriteConfig{
		Encoding:         format.PostingsEncodingFastDeltaVarIntBlocked,
		PackingFactor:    1,
		FastBlockTarget:  fastPostingsBlockTarget,
		DensityThreshold: 0.20,
	}
}

func buildPostingsEncoder(cfg IndexWriteConfig) (StreamingPostingsEncoder, error) {
	switch cfg.Encoding {
	case format.PostingsEncodingFastDeltaVarIntBlocked:
		return NewFastPostingsEncoder(cfg.Encoding, cfg.FastBlockTarget)
	default:
		return nil, fmt.Errorf("unsupported index encoding: %d", cfg.Encoding)
	}
}

func composeIndexFlags(encoding format.PostingsEncoding, packing uint8) uint32 {
	flags := uint32(encoding) & 0x0F
	if packing <= 1 {
		return flags
	}
	flags |= (uint32(packing) << 4) & 0xF0
	return flags
}

func parseIndexFlags(flags uint32) (format.PostingsEncoding, uint8) {
	return format.ParseFlags(flags)
}

func encodeDocumentMetadata(docs []format.DocumentMetadata) []byte {
	if len(docs) == 0 {
		return nil
	}
	sort.Slice(docs, func(i, j int) bool {
		return docs[i].ID < docs[j].ID
	})

	buf := bytes.NewBuffer(make([]byte, 0, len(docs)*20))
	for _, doc := range docs {
		_ = binary.Write(buf, binary.LittleEndian, doc.ID)
		_ = binary.Write(buf, binary.LittleEndian, doc.MinTimeUnix)
		_ = binary.Write(buf, binary.LittleEndian, doc.MaxTimeUnix)
	}
	return buf.Bytes()
}

func writeIndexHeader(w io.Writer, h IndexFooter) error {
	buf := make([]byte, IndexFooterSize)

	binary.LittleEndian.PutUint32(buf[0:4], h.Magic)
	binary.LittleEndian.PutUint32(buf[4:8], h.Version)
	binary.LittleEndian.PutUint32(buf[8:12], h.Flags)
	binary.LittleEndian.PutUint32(buf[12:16], h.DocumentCount)
	binary.LittleEndian.PutUint32(buf[16:20], h.TermBlockCount)
	binary.LittleEndian.PutUint32(buf[20:24], h.PostingsBlockCount)
	binary.LittleEndian.PutUint32(buf[24:28], h.PostingsCompression)
	binary.LittleEndian.PutUint32(buf[28:32], h.Reserved0)
	copy(buf[32:128], h.ReservedMid[:])

	binary.LittleEndian.PutUint64(buf[128:136], h.TermCount)
	binary.LittleEndian.PutUint64(buf[136:144], h.PostingsDataSize)
	binary.LittleEndian.PutUint64(buf[144:152], h.TermDataSize)
	binary.LittleEndian.PutUint64(buf[152:160], h.DocMetadataSize)
	binary.LittleEndian.PutUint64(buf[160:168], h.TermBlockDirSize)
	binary.LittleEndian.PutUint64(buf[168:176], h.PostingsBlockDirSize)
	copy(buf[176:256], h.ReservedEnd[:])

	n, err := w.Write(buf)
	if err != nil {
		return err
	}
	if n != len(buf) {
		return io.ErrShortWrite
	}
	return nil
}

func parseIndexHeader(buf []byte) (IndexFooter, error) {
	if len(buf) < IndexFooterSize {
		return IndexFooter{}, fmt.Errorf("index header too small: %d", len(buf))
	}
	h := IndexFooter{
		Magic:                binary.LittleEndian.Uint32(buf[0:4]),
		Version:              binary.LittleEndian.Uint32(buf[4:8]),
		Flags:                binary.LittleEndian.Uint32(buf[8:12]),
		DocumentCount:        binary.LittleEndian.Uint32(buf[12:16]),
		TermBlockCount:       binary.LittleEndian.Uint32(buf[16:20]),
		PostingsBlockCount:   binary.LittleEndian.Uint32(buf[20:24]),
		PostingsCompression:  binary.LittleEndian.Uint32(buf[24:28]),
		Reserved0:            binary.LittleEndian.Uint32(buf[28:32]),
		TermCount:            binary.LittleEndian.Uint64(buf[128:136]),
		PostingsDataSize:     binary.LittleEndian.Uint64(buf[136:144]),
		TermDataSize:         binary.LittleEndian.Uint64(buf[144:152]),
		DocMetadataSize:      binary.LittleEndian.Uint64(buf[152:160]),
		TermBlockDirSize:     binary.LittleEndian.Uint64(buf[160:168]),
		PostingsBlockDirSize: binary.LittleEndian.Uint64(buf[168:176]),
	}
	copy(h.ReservedMid[:], buf[32:128])
	copy(h.ReservedEnd[:], buf[176:256])
	return h, nil
}

type IndexReader struct {
	reader   io.ReaderAt
	closer   io.Closer
	header   IndexFooter
	docs     []format.DocumentMetadata
	metadata *IndexMetadata
	terms    *termBlockIndex
	postings PostingsReader
	layout   indexLayout
}

// IndexMetadata contains pre-parsed metadata sections from a LOGL index file.
// It can be cached and reused to open immutable indexes without re-reading the
// metadata region.
type IndexMetadata struct {
	docs        []format.DocumentMetadata
	termDir     []termBlockDirEntry
	postingsDir []postingsBlockDirEntry
}

type indexLayout struct {
	postingsOffset int64
	termOffset     int64
	metadataOffset int64
	metadataSize   int
	// headerStart/headerEnd bracket the header/footer region for ClassifyRead.
	// v1: header is at the start of the file; v2: footer is at the end.
	headerStart int64
	headerEnd   int64
}

// ReadIndexHeader reads and validates the 256-byte footer from an index file
// without loading the full index. Useful for populating metadata before upload.
func ReadIndexHeader(path string) (IndexFooter, error) {
	f, err := os.Open(path)
	if err != nil {
		return IndexFooter{}, fmt.Errorf("open index file: %w", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return IndexFooter{}, fmt.Errorf("stat index file: %w", err)
	}
	return ReadIndexFooterFrom(f, fi.Size())
}

// ReadIndexFooterFrom reads and validates the 256-byte footer from the last
// IndexFooterSize bytes of an io.ReaderAt. Returns an error if the magic or
// version is invalid.
func ReadIndexFooterFrom(r io.ReaderAt, size int64) (IndexFooter, error) {
	if size < IndexFooterSize {
		return IndexFooter{}, fmt.Errorf("index file too small: %d", size)
	}
	buf := make([]byte, IndexFooterSize)
	if _, err := r.ReadAt(buf, size-IndexFooterSize); err != nil {
		return IndexFooter{}, fmt.Errorf("read index footer: %w", err)
	}
	h, err := parseIndexHeader(buf)
	if err != nil {
		return IndexFooter{}, err
	}
	if h.Magic != IndexMagic {
		return IndexFooter{}, fmt.Errorf("invalid index magic: %x", h.Magic)
	}
	if h.Version != IndexVersion {
		return IndexFooter{}, fmt.Errorf("index version mismatch: got %d, want %d", h.Version, IndexVersion)
	}
	return h, nil
}

func OpenIndexFile(path string) (*IndexReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open index file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("stat index file: %w", err)
	}
	reader, err := OpenIndexAt(f, 0, info.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	reader.closer = f
	return reader, nil
}

func OpenIndexAt(reader io.ReaderAt, baseOffset int64, totalSize int64) (*IndexReader, error) {
	if totalSize < IndexFooterSize {
		return nil, fmt.Errorf("index file too small: %d", totalSize)
	}

	// v2: footer is at the last IndexFooterSize bytes, not at the start.
	footerBuf := make([]byte, IndexFooterSize)
	if _, err := reader.ReadAt(footerBuf, baseOffset+totalSize-IndexFooterSize); err != nil {
		return nil, fmt.Errorf("read index footer: %w", err)
	}
	header, err := parseIndexHeader(footerBuf)
	if err != nil {
		return nil, err
	}
	if header.Magic != IndexMagic {
		return nil, fmt.Errorf("invalid index magic: %x", header.Magic)
	}
	if header.Version != IndexVersion {
		return nil, fmt.Errorf("unsupported index version: %d", header.Version)
	}

	layout, err := computeIndexLayout(header, baseOffset, totalSize)
	if err != nil {
		return nil, err
	}

	metadata, err := readMetadataRegion(reader, layout, header)
	if err != nil {
		return nil, err
	}

	return openIndexReaderWithMetadata(reader, header, layout, metadata)
}

// OpenIndexAtWithHeader opens an index using header information from meta.json,
// skipping the fixed-size header range read.
func OpenIndexAtWithHeader(
	reader io.ReaderAt,
	baseOffset int64,
	totalSize int64,
	info format.HeaderInfo,
) (*IndexReader, error) {
	header := IndexFooter{
		Magic:                IndexMagic,
		Version:              info.Version,
		Flags:                info.Flags,
		DocumentCount:        info.DocumentCount,
		TermBlockCount:       info.TermBlockCount,
		PostingsBlockCount:   info.PostingsBlockCount,
		PostingsCompression:  info.PostingsCompression,
		TermCount:            info.TermCount,
		PostingsDataSize:     info.PostingsDataSize,
		TermDataSize:         info.TermDataSize,
		DocMetadataSize:      info.DocMetadataSize,
		TermBlockDirSize:     info.TermBlockDirSize,
		PostingsBlockDirSize: info.PostingsBlockDirSize,
	}

	layout, err := computeIndexLayout(header, baseOffset, totalSize)
	if err != nil {
		return nil, err
	}

	metadata, err := readMetadataRegion(reader, layout, header)
	if err != nil {
		return nil, err
	}

	return openIndexReaderWithMetadata(reader, header, layout, metadata)
}

// OpenIndexAtWithMetadata opens an index using pre-parsed metadata, skipping
// both the header and metadata range reads.
func OpenIndexAtWithMetadata(
	reader io.ReaderAt,
	baseOffset int64,
	totalSize int64,
	header IndexFooter,
	metadata *IndexMetadata,
) (*IndexReader, error) {
	layout, err := computeIndexLayout(header, baseOffset, totalSize)
	if err != nil {
		return nil, err
	}
	return openIndexReaderWithMetadata(reader, header, layout, metadata)
}

func computeIndexLayout(header IndexFooter, baseOffset, totalSize int64) (indexLayout, error) {
	if header.Magic != IndexMagic {
		return indexLayout{}, fmt.Errorf("invalid index magic: %x", header.Magic)
	}
	if header.Version != IndexVersion {
		return indexLayout{}, fmt.Errorf("unsupported index version: %d", header.Version)
	}

	metadataSize := header.DocMetadataSize + header.TermBlockDirSize + header.PostingsBlockDirSize
	// v2: no header prefix; footer at end. Postings start at baseOffset.
	minSize := int64(header.PostingsDataSize) + int64(header.TermDataSize) + int64(metadataSize) + IndexFooterSize
	if minSize > totalSize {
		return indexLayout{}, fmt.Errorf("index file truncated: need=%d have=%d", minSize, totalSize)
	}
	if metadataSize > uint64(^uint(0)>>1) {
		return indexLayout{}, fmt.Errorf("metadata region too large: %d", metadataSize)
	}

	postingsOffset := baseOffset // no header prefix in v2
	termOffset := postingsOffset + int64(header.PostingsDataSize)
	metadataOffset := termOffset + int64(header.TermDataSize)
	// v2: footer occupies the last IndexFooterSize bytes.
	footerStart := baseOffset + totalSize - IndexFooterSize

	return indexLayout{
		postingsOffset: postingsOffset,
		termOffset:     termOffset,
		metadataOffset: metadataOffset,
		metadataSize:   int(metadataSize),
		headerStart:    footerStart,
		headerEnd:      baseOffset + totalSize,
	}, nil
}

func readMetadataRegion(reader io.ReaderAt, layout indexLayout, header IndexFooter) (*IndexMetadata, error) {
	metaBuf := make([]byte, layout.metadataSize)
	if _, err := reader.ReadAt(metaBuf, layout.metadataOffset); err != nil {
		return nil, fmt.Errorf("read metadata region: %w", err)
	}

	docEnd := int(header.DocMetadataSize)
	termDirEnd := docEnd + int(header.TermBlockDirSize)
	postingsDirEnd := termDirEnd + int(header.PostingsBlockDirSize)
	if postingsDirEnd != len(metaBuf) {
		return nil, fmt.Errorf("invalid metadata region sizing")
	}

	docs, err := decodeDocumentMetadata(metaBuf[:docEnd], header.DocumentCount)
	if err != nil {
		return nil, err
	}
	termDir, err := decodeTermBlockDirEntries(metaBuf[docEnd:termDirEnd], header.TermBlockCount)
	if err != nil {
		return nil, fmt.Errorf("decode term directory: %w", err)
	}
	postingsDir, err := decodePostingsBlockDirEntries(metaBuf[termDirEnd:postingsDirEnd], header.PostingsBlockCount)
	if err != nil {
		return nil, fmt.Errorf("decode postings directory: %w", err)
	}
	return &IndexMetadata{
		docs:        docs,
		termDir:     termDir,
		postingsDir: postingsDir,
	}, nil
}

func openIndexReaderWithMetadata(
	reader io.ReaderAt,
	header IndexFooter,
	layout indexLayout,
	metadata *IndexMetadata,
) (*IndexReader, error) {
	if metadata == nil {
		return nil, fmt.Errorf("index metadata cannot be nil")
	}
	if header.TermCount > uint64(^uint32(0)) {
		return nil, fmt.Errorf("term count overflows uint32: %d", header.TermCount)
	}

	terms, err := openTermBlockIndex(reader, layout.termOffset, header.TermDataSize, uint32(header.TermCount), metadata.termDir)
	if err != nil {
		return nil, fmt.Errorf("open term dictionary index: %w", err)
	}

	encoding, packing := parseIndexFlags(header.Flags)
	postingsReader, err := OpenPostingsReader(
		encoding,
		packing,
		reader,
		layout.postingsOffset,
		header.PostingsDataSize,
		metadata.postingsDir,
		header.TermCount,
		header.DocumentCount,
		header.PostingsCompression,
	)
	if err != nil {
		terms.Close()
		return nil, fmt.Errorf("open postings reader: %w", err)
	}

	return &IndexReader{
		reader:   reader,
		header:   header,
		docs:     metadata.docs,
		metadata: metadata,
		terms:    terms,
		postings: postingsReader,
		layout:   layout,
	}, nil
}

func decodeDocumentMetadata(data []byte, count uint32) ([]format.DocumentMetadata, error) {
	if count == 0 || len(data) == 0 {
		return nil, nil
	}
	if len(data)%20 != 0 {
		return nil, fmt.Errorf("invalid document metadata size")
	}
	n := uint32(len(data) / 20)
	if n > count {
		return nil, fmt.Errorf("document metadata entries exceed declared document count")
	}
	docs := make([]format.DocumentMetadata, n)
	for i := range n {
		off := int(i) * 20
		docs[i] = format.DocumentMetadata{
			ID:          binary.LittleEndian.Uint32(data[off : off+4]),
			MinTimeUnix: int64(binary.LittleEndian.Uint64(data[off+4 : off+12])),
			MaxTimeUnix: int64(binary.LittleEndian.Uint64(data[off+12 : off+20])),
		}
	}
	return docs, nil
}

func (r *IndexReader) Header() IndexFooter {
	return r.header
}

// ReadHeader returns the JSON-friendly header summary.
func (r *IndexReader) ReadHeader() format.HeaderInfo {
	return r.header.Info()
}

func (r *IndexReader) Documents() []format.DocumentMetadata {
	return r.docs
}

// Metadata returns parsed metadata sections that can be reused to reopen this
// immutable index without re-reading its metadata region.
func (r *IndexReader) Metadata() *IndexMetadata {
	return r.metadata
}

// cachedReaderState holds opaque state for fast index reopens.
type cachedReaderState struct {
	header   IndexFooter
	metadata *IndexMetadata
}

// CachedState returns opaque state that can be passed to OpenIndexAtCached
// for fast reopens without re-reading metadata.
func (r *IndexReader) CachedState() any {
	return &cachedReaderState{header: r.header, metadata: r.metadata}
}

// OpenIndexAtCached opens an index reader using opaque cached state from a
// prior CachedState() call.
func OpenIndexAtCached(reader io.ReaderAt, baseOffset, totalSize int64, cached any) (*IndexReader, error) {
	state, ok := cached.(*cachedReaderState)
	if !ok {
		return nil, fmt.Errorf("invalid cached state type for v2: %T", cached)
	}
	return OpenIndexAtWithMetadata(reader, baseOffset, totalSize, state.header, state.metadata)
}

func (r *IndexReader) TermCount() uint32 {
	return uint32(r.header.TermCount)
}

func (r *IndexReader) FindTerm(term string) (int, error) {
	var key [NgramLength]byte
	copy(key[:], term)
	return r.terms.FindTerm(key)
}

// GetBitmap returns the bitmap for a term index.
// When the result has MatchesAll set, the term is density-filtered and matches all documents.
func (r *IndexReader) GetBitmap(termIndex int) (format.Bitmap, error) {
	return r.postings.GetBitmap(termIndex)
}

func (r *IndexReader) query(term string) ([]uint32, error) {
	pos, err := r.FindTerm(term)
	if err != nil {
		return nil, err
	}
	if pos < 0 {
		return nil, nil
	}
	res, err := r.GetBitmap(pos)
	if err != nil {
		return nil, err
	}
	if res.MatchesAll {
		docIDs := make([]uint32, len(r.docs))
		for i, doc := range r.docs {
			docIDs[i] = doc.ID
		}
		return docIDs, nil
	}
	return res.Roaring.ToArray(), nil
}

func (r *IndexReader) ClassifyRead(offset, _ int64) format.ReadSection {
	switch {
	case offset >= r.layout.headerStart && offset < r.layout.headerEnd:
		return format.ReadSectionHeader
	case offset < r.layout.termOffset:
		return format.ReadSectionPostings
	case offset < r.layout.metadataOffset:
		return format.ReadSectionTermDict
	default:
		return format.ReadSectionMetadata
	}
}

func (r *IndexReader) Close() error {
	if r.postings != nil {
		_ = r.postings.Close()
	}
	if r.terms != nil {
		_ = r.terms.Close()
	}
	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}

type IndexTermIterator struct {
	reader      *IndexReader
	idx         *termBlockIndex     // term block index; used for lazy decoding and eviction
	blockIdx    int                 // index into idx.dir for the current block
	blockTerms  [][NgramLength]byte // decoded terms for the current block only
	posInBlock  int                 // position within blockTerms
	globalPos   int                 // position across all terms; passed to GetDocIDs
	currentTerm [8]byte
	// currentDocIDs holds decoded postings for the current term (reused across Next).
	// matchesAll is the density-filter sentinel. Bitmap() builds roaring lazily.
	currentDocIDs []uint32
	matchesAll    bool
	currentBM     format.Bitmap // lazy; valid only when bmValid
	bmValid       bool
	err           error
}

func (r *IndexReader) NewTermIterator() (format.TermIterator, error) {
	return r.newTermIterator()
}

func (r *IndexReader) newTermIterator() (*IndexTermIterator, error) {
	it := &IndexTermIterator{
		reader:     r,
		idx:        r.terms,
		blockIdx:   0,
		posInBlock: -1,
		globalPos:  -1,
	}
	// Empty index: nothing to decode.
	if len(r.terms.dir) == 0 {
		return it, nil
	}
	// Decode block 0 eagerly so construction errors surface immediately,
	// consistent with the previous behaviour where AllTerms() errors surfaced here.
	blockTerms, err := r.terms.decodeBlock(0)
	if err != nil {
		return nil, err
	}
	it.blockTerms = blockTerms
	return it, nil
}

func (it *IndexTermIterator) Next() bool {
	if it.err != nil {
		return false
	}
	// Empty indexes skip eager decoding in NewTermIterator, so Next needs a
	// fast exit instead of relying on the block-advance loop's nil-slice path.
	if len(it.idx.dir) == 0 {
		return false
	}
	it.posInBlock++
	it.globalPos++

	// Advance to next block when current block is exhausted.
	for it.posInBlock >= len(it.blockTerms) {
		it.idx.evictBlock(it.blockIdx)
		it.blockIdx++
		if it.blockIdx >= len(it.idx.dir) {
			// All blocks exhausted.
			return false
		}
		blockTerms, err := it.idx.decodeBlock(it.blockIdx)
		if err != nil {
			it.err = err
			return false
		}
		it.blockTerms = blockTerms
		it.posInBlock = 0
	}

	it.currentTerm = [8]byte{}
	copy(it.currentTerm[:NgramLength], it.blockTerms[it.posInBlock][:])
	ids, matchesAll, err := it.reader.postings.GetDocIDs(it.globalPos, it.currentDocIDs)
	if err != nil {
		it.err = err
		return false
	}
	it.currentDocIDs = ids
	it.matchesAll = matchesAll
	it.bmValid = false
	it.currentBM = format.Bitmap{}
	return true
}

func (it *IndexTermIterator) Term() [8]byte {
	return it.currentTerm
}

// DocIDs returns the decoded doc IDs for the current term and whether the term
// is a MatchesAll density sentinel. The returned slice is owned by the iterator
// and is invalidated by the next call to Next.
func (it *IndexTermIterator) DocIDs() ([]uint32, bool) {
	return it.currentDocIDs, it.matchesAll
}

// Bitmap returns a roaring Bitmap for the current term. Built lazily from
// DocIDs so the merge path can avoid roaring entirely.
func (it *IndexTermIterator) Bitmap() format.Bitmap {
	if it.bmValid {
		return it.currentBM
	}
	if it.matchesAll {
		it.currentBM = format.Bitmap{MatchesAll: true}
		it.bmValid = true
		return it.currentBM
	}
	bm := roaring.New()
	bm.AddMany(it.currentDocIDs)
	it.currentBM = format.Bitmap{Roaring: bm}
	it.bmValid = true
	return it.currentBM
}

func (it *IndexTermIterator) Err() error {
	return it.err
}

// docTimeKey identifies a document by its time bounds. Documents with
// identical time bounds across different source indexes represent the
// same logical time bucket and are merged into a single document.
type docTimeKey struct {
	minTimeUnix int64
	maxTimeUnix int64
}

// Merge helpers are in merge.go.

func compareTerm8(a, b [8]byte) int {
	for i := range 8 {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

const (
	termBlockEntrySize = 26 // [6]byte + u32 + u64 + u32 + u32
)

type termBlockDirEntry struct {
	FirstTerm      [NgramLength]byte
	FirstTermID    uint32
	BlockOffset    uint64
	CompressedSize uint32
	TermCount      uint32
}

func buildTermData(terms [][NgramLength]byte) ([]byte, []termBlockDirEntry, error) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
	if err != nil {
		return nil, nil, fmt.Errorf("create term zstd encoder: %w", err)
	}
	defer encoder.Close()

	var out bytes.Buffer
	entries := make([]termBlockDirEntry, 0, len(terms)/TermDictBlockSize+1)
	for start := 0; start < len(terms); start += TermDictBlockSize {
		end := min(start+TermDictBlockSize, len(terms))
		blockTerms := terms[start:end]

		raw := make([]byte, len(blockTerms)*NgramLength)
		for i, term := range blockTerms {
			copy(raw[i*NgramLength:(i+1)*NgramLength], term[:])
		}
		compressed := encoder.EncodeAll(raw, nil)
		blockOffset := out.Len()
		entry := termBlockDirEntry{
			FirstTerm:      blockTerms[0],
			FirstTermID:    uint32(start),
			BlockOffset:    uint64(blockOffset),
			CompressedSize: uint32(4 + len(compressed)),
			TermCount:      uint32(len(blockTerms)),
		}
		if entry.CompressedSize > format.MaxQueryRequestBytes {
			return nil, nil, fmt.Errorf("term block exceeds request cap: %d", entry.CompressedSize)
		}
		entries = append(entries, entry)
		var szBuf [4]byte
		binary.LittleEndian.PutUint32(szBuf[:], uint32(len(compressed)))
		if _, err := out.Write(szBuf[:]); err != nil {
			return nil, nil, fmt.Errorf("write term block size: %w", err)
		}
		if _, err := out.Write(compressed); err != nil {
			return nil, nil, fmt.Errorf("write term block: %w", err)
		}
	}
	return out.Bytes(), entries, nil
}

func encodeTermBlockDirEntries(entries []termBlockDirEntry) ([]byte, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	var out bytes.Buffer
	out.Grow(len(entries) * termBlockEntrySize)
	for _, entry := range entries {
		if _, err := out.Write(entry.FirstTerm[:]); err != nil {
			return nil, fmt.Errorf("write term dir first term: %w", err)
		}
		if err := binary.Write(&out, binary.LittleEndian, entry.FirstTermID); err != nil {
			return nil, fmt.Errorf("write term dir first id: %w", err)
		}
		if err := binary.Write(&out, binary.LittleEndian, entry.BlockOffset); err != nil {
			return nil, fmt.Errorf("write term dir block offset: %w", err)
		}
		if err := binary.Write(&out, binary.LittleEndian, entry.CompressedSize); err != nil {
			return nil, fmt.Errorf("write term dir compressed size: %w", err)
		}
		if err := binary.Write(&out, binary.LittleEndian, entry.TermCount); err != nil {
			return nil, fmt.Errorf("write term dir term count: %w", err)
		}
	}
	return out.Bytes(), nil
}

func decodeTermBlockDirEntries(data []byte, count uint32) ([]termBlockDirEntry, error) {
	if count == 0 || len(data) == 0 {
		return nil, nil
	}
	expectedDirSize := int(count) * termBlockEntrySize
	if len(data) != expectedDirSize {
		return nil, fmt.Errorf("term block directory size mismatch: got=%d expected=%d", len(data), expectedDirSize)
	}
	dir := make([]termBlockDirEntry, 0, count)
	for i := 0; i < int(count); i++ {
		off := i * termBlockEntrySize
		var first [NgramLength]byte
		copy(first[:], data[off:off+NgramLength])
		entry := termBlockDirEntry{
			FirstTerm:      first,
			FirstTermID:    binary.LittleEndian.Uint32(data[off+NgramLength : off+NgramLength+4]),
			BlockOffset:    binary.LittleEndian.Uint64(data[off+NgramLength+4 : off+NgramLength+12]),
			CompressedSize: binary.LittleEndian.Uint32(data[off+NgramLength+12 : off+NgramLength+16]),
			TermCount:      binary.LittleEndian.Uint32(data[off+NgramLength+16 : off+NgramLength+20]),
		}
		dir = append(dir, entry)
	}
	return dir, nil
}

func encodePostingsBlockDirEntries(entries []postingsBlockDirEntry) ([]byte, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	const postingsEntrySize = 20
	var out bytes.Buffer
	out.Grow(len(entries) * postingsEntrySize)
	for _, entry := range entries {
		if err := binary.Write(&out, binary.LittleEndian, entry.FirstTermID); err != nil {
			return nil, fmt.Errorf("write postings dir first id: %w", err)
		}
		if err := binary.Write(&out, binary.LittleEndian, entry.NumTerms); err != nil {
			return nil, fmt.Errorf("write postings dir num terms: %w", err)
		}
		if err := binary.Write(&out, binary.LittleEndian, entry.BlockOffset); err != nil {
			return nil, fmt.Errorf("write postings dir block offset: %w", err)
		}
		if err := binary.Write(&out, binary.LittleEndian, entry.CompressedSize); err != nil {
			return nil, fmt.Errorf("write postings dir compressed size: %w", err)
		}
	}
	return out.Bytes(), nil
}

func decodePostingsBlockDirEntries(data []byte, count uint32) ([]postingsBlockDirEntry, error) {
	if count == 0 || len(data) == 0 {
		return nil, nil
	}
	const postingsEntrySize = 20
	expected := int(count) * postingsEntrySize
	if len(data) != expected {
		return nil, fmt.Errorf("postings block directory size mismatch: got=%d expected=%d", len(data), expected)
	}
	dir := make([]postingsBlockDirEntry, 0, count)
	for i := 0; i < int(count); i++ {
		off := i * postingsEntrySize
		dir = append(dir, postingsBlockDirEntry{
			FirstTermID:    binary.LittleEndian.Uint32(data[off : off+4]),
			NumTerms:       binary.LittleEndian.Uint32(data[off+4 : off+8]),
			BlockOffset:    binary.LittleEndian.Uint64(data[off+8 : off+16]),
			CompressedSize: binary.LittleEndian.Uint32(data[off+16 : off+20]),
		})
	}
	return dir, nil
}

type termBlockIndex struct {
	reader        io.ReaderAt
	sectionOffset int64
	sectionSize   uint64
	termCount     uint32
	dir           []termBlockDirEntry
	decoder       *zstd.Decoder

	cacheMu sync.Mutex
	cache   map[int][][NgramLength]byte
}

func openTermBlockIndex(
	reader io.ReaderAt,
	sectionOffset int64,
	sectionSize uint64,
	termCount uint32,
	dir []termBlockDirEntry,
) (*termBlockIndex, error) {
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	return &termBlockIndex{
		reader:        reader,
		sectionOffset: sectionOffset,
		sectionSize:   sectionSize,
		termCount:     termCount,
		dir:           dir,
		decoder:       decoder,
		cache:         make(map[int][][NgramLength]byte),
	}, nil
}

func (idx *termBlockIndex) FindTerm(term [NgramLength]byte) (int, error) {
	if idx.termCount == 0 {
		return -1, nil
	}
	blockIdx := max(sort.Search(len(idx.dir), func(i int) bool {
		return bytes.Compare(idx.dir[i].FirstTerm[:], term[:]) > 0
	})-1, 0)
	if blockIdx >= len(idx.dir) {
		return -1, nil
	}

	terms, err := idx.decodeBlock(blockIdx)
	if err != nil {
		return -1, err
	}
	pos := sort.Search(len(terms), func(i int) bool {
		return bytes.Compare(terms[i][:], term[:]) >= 0
	})
	if pos >= len(terms) || terms[pos] != term {
		return -1, nil
	}
	return int(idx.dir[blockIdx].FirstTermID) + pos, nil
}

func (idx *termBlockIndex) decodeBlock(blockIdx int) ([][NgramLength]byte, error) {
	idx.cacheMu.Lock()
	if cached, ok := idx.cache[blockIdx]; ok {
		idx.cacheMu.Unlock()
		return cached, nil
	}
	idx.cacheMu.Unlock()

	entry := idx.dir[blockIdx]
	if entry.TermCount == 0 {
		return nil, fmt.Errorf("term block %d has zero terms (possible corruption)", blockIdx)
	}
	if entry.CompressedSize > format.MaxQueryRequestBytes {
		return nil, fmt.Errorf("term block request exceeds cap: %d", entry.CompressedSize)
	}
	if entry.BlockOffset+uint64(entry.CompressedSize) > idx.sectionSize {
		return nil, fmt.Errorf("term block out of bounds")
	}

	blockBytes := make([]byte, entry.CompressedSize)
	if _, err := idx.reader.ReadAt(blockBytes, idx.sectionOffset+int64(entry.BlockOffset)); err != nil {
		return nil, fmt.Errorf("read term block: %w", err)
	}
	if len(blockBytes) < 4 {
		return nil, fmt.Errorf("term block too small")
	}
	compressedLen := binary.LittleEndian.Uint32(blockBytes[:4])
	if int(compressedLen)+4 > len(blockBytes) {
		return nil, fmt.Errorf("invalid compressed term block length")
	}
	raw, err := idx.decoder.DecodeAll(blockBytes[4:4+compressedLen], nil)
	if err != nil {
		return nil, fmt.Errorf("decompress term block: %w", err)
	}
	expected := int(entry.TermCount) * NgramLength
	if len(raw) != expected {
		return nil, fmt.Errorf("term block size mismatch: got=%d expected=%d", len(raw), expected)
	}

	terms := make([][NgramLength]byte, entry.TermCount)
	for i := range terms {
		copy(terms[i][:], raw[i*NgramLength:(i+1)*NgramLength])
	}

	idx.cacheMu.Lock()
	idx.cache[blockIdx] = terms
	idx.cacheMu.Unlock()
	return terms, nil
}

// evictBlock removes blockIdx from the decoded block cache. This is called by the
// lazy term iterator after advancing past a block to bound peak memory usage.
// Safe to call with a blockIdx that was never cached; delete on a missing key is a no-op.
func (idx *termBlockIndex) evictBlock(blockIdx int) {
	idx.cacheMu.Lock()
	delete(idx.cache, blockIdx)
	idx.cacheMu.Unlock()
}

func (idx *termBlockIndex) Close() error {
	if idx.decoder != nil {
		idx.decoder.Close()
	}
	return nil
}
