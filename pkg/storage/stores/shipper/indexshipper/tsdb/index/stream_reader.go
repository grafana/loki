package index

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc/filepool"
)

// StreamReader is the file-streaming counterpart to ByteSliceReader.
//
// It serves every read through a pool of file handles rather than a memory
// mapping, so the Go scheduler can observe a blocking read and run other work
// while it completes. A page fault on a memory mapping is invisible to the
// runtime and blocks the whole OS thread instead.
//
// The sections that are small enough to hold in memory — the TOC and the
// fingerprint offsets — are read once at construction.
// The symbols section and the postings offset table are scanned once to
// build a sparse offset table, so later lookups seek close to their target
// and read only a bounded window.
// Series records are read on demand, one per lookup.
type StreamReader struct {
	factory *streamenc.FilePoolDecbufFactory
	path    string
	size    int64
	version int

	toc                *TOC
	symbols            *streamSymbols
	postings           *streamPostings
	fingerprintOffsets FingerprintOffsets
	decoder            *Decoder
}

const DefaultMaxIdleFileHandles = 16

// StreamOptions selects the streaming reader and tunes it.
type StreamOptions struct {
	// MaxIdleFileHandles is the number of idle file handles the reader keeps
	// open for its index file.
	// Zero disables pooling entirely: every read opens and closes the file.
	MaxIdleFileHandles uint
}

func DefaultStreamOptions() StreamOptions {
	return StreamOptions{MaxIdleFileHandles: DefaultMaxIdleFileHandles}
}

// OpenReader implements ReaderOptions.
func (o StreamOptions) OpenReader(path string) (Reader, error) {
	r, err := NewStreamFileReader(path, o)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// NewStreamFileReader constructs a StreamReader against the given index file.
func NewStreamFileReader(path string, opts StreamOptions) (*StreamReader, error) {
	factory, err := streamenc.NewFilePoolDecbufFactory(path, opts.MaxIdleFileHandles, filepool.NewFilePoolMetrics(nil))
	if err != nil {
		return nil, fmt.Errorf("index file decbuf factory: %w", err)
	}

	reader := &StreamReader{
		factory: factory,
		path:    path,
		size:    factory.FileSize(),
	}

	version, err := reader.readHeader()
	if err != nil {
		_ = factory.Close()
		return nil, err
	}
	reader.version = version

	toc, err := reader.readTOC()
	if err != nil {
		_ = factory.Close()
		return nil, err
	}
	reader.toc = toc

	reader.postings, err = newStreamPostings(context.Background(), factory, int(toc.PostingsTable))
	if err != nil {
		_ = factory.Close()
		return nil, err
	}

	reader.symbols, err = newStreamSymbols(context.Background(), factory, int(toc.Symbols), reader.postings.isLabelName)
	if err != nil {
		_ = factory.Close()
		return nil, err
	}

	reader.fingerprintOffsets, err = reader.readFingerprintOffsetsTable(int(toc.FingerprintOffsets))
	if err != nil {
		_ = factory.Close()
		return nil, err
	}

	reader.decoder = newDecoder(reader.symbols.Lookup, DefaultMaxChunksToBypassMarkerLookup)

	return reader, nil
}

// readHeader validates the magic bytes and returns the on-disk format version.
func (s *StreamReader) readHeader() (int, error) {
	// Validate header size
	if s.size < int64(HeaderLen) {
		return 0, fmt.Errorf("index header: %w", streamenc.ErrInvalidSize)
	}
	// Construct decbuf
	decbuf := s.factory.NewRawDecbuf(context.Background())
	defer decbuf.Close()
	if err := decbuf.Err(); err != nil {
		return 0, fmt.Errorf("open header decbuf: %w", err)
	}
	// Extract and validate magic
	magic := decbuf.Be32()
	if err := decbuf.Err(); err != nil {
		return 0, fmt.Errorf("read header magic: %w", err)
	}
	if magic != MagicIndex {
		return 0, fmt.Errorf("invalid magic number %x", magic)
	}
	// Extract and validate version
	version := int(decbuf.Byte())
	if err := decbuf.Err(); err != nil {
		return 0, fmt.Errorf("read header version: %w", err)
	}
	if version != FormatV2 && version != FormatV3 && version != FormatV4 {
		return 0, fmt.Errorf("unknown index file version %d", version)
	}
	return version, nil
}

// readTOC reads the fixed-size TOC record from the tail of
// the file and validates its CRC32.
func (s *StreamReader) readTOC() (*TOC, error) {
	// Validate size
	if s.size < int64(indexTOCLen) {
		return nil, fmt.Errorf("index toc: %w", streamenc.ErrInvalidSize)
	}
	// Create decbuf
	decbuf := s.factory.NewRawDecbuf(context.Background())
	defer decbuf.Close()
	if err := decbuf.Err(); err != nil {
		return nil, fmt.Errorf("open toc decbuf: %w", err)
	}
	// Validate CRC32
	tocStart := int(s.size) - indexTOCLen
	if decbuf.ResetAt(tocStart); decbuf.Err() != nil {
		return nil, fmt.Errorf("go to start of toc for crc: %w", decbuf.Err())
	}
	if decbuf.CheckCrc32(castagnoliTable); decbuf.Err() != nil {
		return nil, fmt.Errorf("check toc crc: %w", decbuf.Err())
	}
	// Read TOC
	if decbuf.ResetAt(tocStart); decbuf.Err() != nil {
		return nil, fmt.Errorf("go to start of toc to read it: %w", decbuf.Err())
	}
	toc := &TOC{
		Symbols:            decbuf.Be64(),
		Series:             decbuf.Be64(),
		LabelIndices:       decbuf.Be64(),
		LabelIndicesTable:  decbuf.Be64(),
		Postings:           decbuf.Be64(),
		PostingsTable:      decbuf.Be64(),
		FingerprintOffsets: decbuf.Be64(),
		Metadata: Metadata{
			From:     int64(decbuf.Be64()),
			Through:  int64(decbuf.Be64()),
			Checksum: decbuf.Be32(),
		},
	}
	if err := decbuf.Err(); err != nil {
		return nil, fmt.Errorf("read toc: %w", err)
	}
	return toc, nil
}

// readFingerprintOffsetsTable reads the fingerprint-offsets section at the
// given absolute file offset into memory.
// It is the StreamReader's counterpart to readFingerprintOffsetsTable in index.go.
// On disk the section is a 4-byte big-endian count N, followed by N
// (seriesRef, fingerprint) pairs of 8-byte big-endian values, then the section CRC,
// which NewDecbufAtChecked validates while opening.
func (s *StreamReader) readFingerprintOffsetsTable(offset int) (FingerprintOffsets, error) {
	decbuf := s.factory.NewDecbufAtChecked(context.Background(), offset, castagnoliTable)
	defer decbuf.Close()
	if err := decbuf.Err(); err != nil {
		return nil, err
	}

	n := decbuf.Be32()
	result := make(FingerprintOffsets, 0, int(n))
	for decbuf.Err() == nil && n > 0 {
		result = append(result, [2]uint64{decbuf.Be64(), decbuf.Be64()})
		n--
	}
	return result, decbuf.Err()
}

func (s *StreamReader) Version() int {
	return s.version
}

// RawFileReader opens a fresh file handle over the index file.
// The caller owns closing it.
func (s *StreamReader) RawFileReader() (io.ReadSeekCloser, error) {
	return os.Open(s.path)
}

func (s *StreamReader) Bounds() (int64, int64) {
	return s.toc.Metadata.From, s.toc.Metadata.Through
}

func (s *StreamReader) Checksum() uint32 {
	return s.toc.Metadata.Checksum
}

func (s *StreamReader) LabelValues(name string, matchers ...*labels.Matcher) ([]string, error) {
	if len(matchers) > 0 {
		return nil, fmt.Errorf("matchers parameter is not implemented: %+v", matchers)
	}
	return s.postings.labelValuesFor(name)
}

func (s *StreamReader) LabelNames(matchers ...*labels.Matcher) ([]string, error) {
	if len(matchers) > 0 {
		return nil, fmt.Errorf("matchers parameter is not implemented: %+v", matchers)
	}
	return s.postings.labelNames(), nil
}

// Postings returns a postings iterator for the given label name and values.
// Values must be provided in ascending order.
//
// A non-nil FingerprintFilter restricts the result to the requested shard by
// bounding the merged postings with the fingerprint offsets table.
func (s *StreamReader) Postings(labelName string, fpFilter FingerprintFilter, labelValues ...string) (Postings, error) {
	postings, err := s.postings.postingsFor(labelName, labelValues...)
	if err != nil {
		return nil, err
	}
	if fpFilter != nil {
		return NewShardedPostings(postings, fpFilter, s.fingerprintOffsets), nil
	}
	return postings, nil
}

func (s *StreamReader) Size() int64 {
	return s.size
}

func (s *StreamReader) Close() error {
	return s.factory.Close()
}
