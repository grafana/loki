package index

import (
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sort"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

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

// NewStreamFileReader constructs a StreamReader against the given index file.
func NewStreamFileReader(path string) (*StreamReader, error) {
	factory, err := streamenc.NewFilePoolDecbufFactory(path, 0, filepool.NewFilePoolMetrics(nil))
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
func (s StreamReader) readHeader() (int, error) {
	// Validate header size
	if s.size < int64(HeaderLen) {
		return 0, fmt.Errorf("index header: %w", streamenc.ErrInvalidSize)
	}
	// Construct decbuf
	decbuf := s.factory.NewRawDecbuf(context.Background())
	defer func() { _ = decbuf.Close() }()
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
func (s StreamReader) readTOC() (*TOC, error) {
	// Validate size
	if s.size < int64(indexTOCLen) {
		return nil, fmt.Errorf("index toc: %w", streamenc.ErrInvalidSize)
	}
	// Create decbuf
	decbuf := s.factory.NewRawDecbuf(context.Background())
	defer func() { _ = decbuf.Close() }()
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
func (s StreamReader) readFingerprintOffsetsTable(offset int) (FingerprintOffsets, error) {
	decbuf := s.factory.NewDecbufAtChecked(context.Background(), offset, castagnoliTable)
	defer func() { _ = decbuf.Close() }()
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

// seriesOffset returns the absolute file offset of the series record for the
// given series ref.
// Series records are padded to a 16-byte alignment and a ref is the record's
// file offset divided by 16, which is what lets a 4-byte ref address a much
// larger file.
// See Creator.AddSeries.
func seriesOffset(id storage.SeriesRef) int {
	return int(id) * 16
}

// readSeriesRecord reads the series record at the given absolute file offset
// and returns its content bytes, having validated the record's CRC32.
//
// On disk a series record is a uvarint content length, the content, then
// a CRC32 over the content alone.
// The uvarint length prefix is why this can't go through the streamenc
// factory: NewDecbufAtChecked assumes a 4-byte big-endian length, so we open
// a raw Decbuf over the whole file and decode the record ourselves.
func (s StreamReader) readSeriesRecord(offset int) ([]byte, error) {
	decbuf := s.factory.NewRawDecbuf(context.Background())
	if err := decbuf.Err(); err != nil {
		return nil, fmt.Errorf("open series decbuf: %w", err)
	}
	defer func() { _ = decbuf.Close() }()

	if decbuf.ResetAt(offset); decbuf.Err() != nil {
		return nil, fmt.Errorf("go to start of series record: %w", decbuf.Err())
	}

	contentLen := decbuf.Uvarint64()
	if err := decbuf.Err(); err != nil {
		return nil, fmt.Errorf("read series record length: %w", err)
	}
	// Bound the claimed length against what is left in the file before
	// allocating, so a corrupt length prefix can't ask for an arbitrarily
	// large buffer.
	remaining := decbuf.Len()
	if remaining < crc32.Size || contentLen > uint64(remaining-crc32.Size) {
		return nil, fmt.Errorf(
			"series record at %d claims %d content bytes but only %d remain: %w",
			offset, contentLen, remaining, streamenc.ErrInvalidSize,
		)
	}

	content := make([]byte, contentLen)
	decbuf.ReadInto(content)
	expectedCRC := decbuf.Be32()
	if err := decbuf.Err(); err != nil {
		return nil, fmt.Errorf("read series record: %w", err)
	}
	if crc32.Checksum(content, castagnoliTable) != expectedCRC {
		return nil, fmt.Errorf("series record at %d: %w", offset, streamenc.ErrInvalidChecksum)
	}
	return content, nil
}

func (s StreamReader) Version() int {
	return s.version
}

// RawFileReader opens a fresh file handle over the index file.
// The caller owns closing it.
func (s StreamReader) RawFileReader() (io.ReadSeekCloser, error) {
	return os.Open(s.path)
}

func (s StreamReader) Bounds() (int64, int64) {
	return s.toc.Metadata.From, s.toc.Metadata.Through
}

func (s StreamReader) Checksum() uint32 {
	return s.toc.Metadata.Checksum
}

func (s StreamReader) LabelValues(name string, matchers ...*labels.Matcher) ([]string, error) {
	if len(matchers) > 0 {
		return nil, fmt.Errorf("matchers parameter is not implemented: %+v", matchers)
	}
	return s.postings.labelValuesFor(name)
}

func (s StreamReader) LabelNames(matchers ...*labels.Matcher) ([]string, error) {
	if len(matchers) > 0 {
		return nil, fmt.Errorf("matchers parameter is not implemented: %+v", matchers)
	}
	return s.postings.labelNames(), nil
}

func (s StreamReader) LabelValueFor(id storage.SeriesRef, label string) (string, error) {
	content, err := s.readSeriesRecord(seriesOffset(id))
	if err != nil {
		return "", fmt.Errorf("label values for: %w", err)
	}

	value, err := s.decoder.LabelValueFor(content, label)
	if err != nil {
		return "", storage.ErrNotFound
	}
	if value == "" {
		return "", storage.ErrNotFound
	}
	return value, nil
}

func (s StreamReader) LabelNamesFor(ids ...storage.SeriesRef) ([]string, error) {
	// Gather the name offsets in the symbol table first, so each distinct name
	// is only resolved once no matter how many series carry it.
	offsetsMap := make(map[uint32]struct{})
	for _, id := range ids {
		content, err := s.readSeriesRecord(seriesOffset(id))
		if err != nil {
			return nil, fmt.Errorf("get buffer for series: %w", err)
		}

		offsets, err := s.decoder.LabelNamesOffsetsFor(content)
		if err != nil {
			return nil, fmt.Errorf("get label name offsets: %w", err)
		}
		for _, offset := range offsets {
			offsetsMap[offset] = struct{}{}
		}
	}

	names := make([]string, 0, len(offsetsMap))
	for offset := range offsetsMap {
		name, err := s.symbols.Lookup(offset)
		if err != nil {
			return nil, fmt.Errorf("lookup symbol in LabelNamesFor: %w", err)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (s StreamReader) Series(id storage.SeriesRef, from int64, through int64, lbls *labels.Labels, chks *[]ChunkMeta) (uint64, error) {
	content, err := s.readSeriesRecord(seriesOffset(id))
	if err != nil {
		return 0, err
	}

	fingerprint, err := s.decoder.Series(s.version, content, id, from, through, lbls, chks)
	if err != nil {
		return 0, fmt.Errorf("decode series: %w", err)
	}
	return fingerprint, nil
}

func (s StreamReader) ChunkStats(id storage.SeriesRef, from, through int64, lbls *labels.Labels, by map[string]struct{}) (uint64, ChunkStats, error) {
	content, err := s.readSeriesRecord(seriesOffset(id))
	if err != nil {
		return 0, ChunkStats{}, err
	}

	return s.decoder.ChunkStats(s.version, content, id, from, through, lbls, by)
}

// Postings returns a postings iterator for the given label name and values.
// Values must be provided in ascending order.
//
// A non-nil FingerprintFilter restricts the result to the requested shard by
// bounding the merged postings with the fingerprint offsets table.
func (s StreamReader) Postings(labelName string, fpFilter FingerprintFilter, labelValues ...string) (Postings, error) {
	postings, err := s.postings.postingsFor(labelName, labelValues...)
	if err != nil {
		return nil, err
	}
	if fpFilter != nil {
		return NewShardedPostings(postings, fpFilter, s.fingerprintOffsets), nil
	}
	return postings, nil
}

func (s StreamReader) Size() int64 {
	return s.size
}

func (s StreamReader) Close() error {
	return s.factory.Close()
}
