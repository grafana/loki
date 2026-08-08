package index

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc/filepool"
)

// StreamReader is the file-streaming counterpart to ByteSliceReader.
//
// Currently, StreamReader delegates some calls to a *ByteSliceReader.
// Follow-up changes will progressively replace embedded calls with streaming implementations
// backed by a file-handle pool.
type StreamReader struct {
	mmapReader *ByteSliceReader

	factory *streamenc.FilePoolDecbufFactory
	path    string
	size    int64
	version int

	toc                *TOC
	symbols            *streamSymbols
	postings           *streamPostings
	fingerprintOffsets FingerprintOffsets
}

// NewStreamFileReader constructs a StreamReader against the given index file.
func NewStreamFileReader(path string) (*StreamReader, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat index file: %w", err)
	}
	size := fileInfo.Size()

	factory := streamenc.NewFilePoolDecbufFactory(path, 0, filepool.NewFilePoolMetrics(nil))

	reader := &StreamReader{
		factory: factory,
		path:    path,
		size:    size,
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

	reader.symbols, err = newStreamSymbols(context.Background(), factory, int(toc.Symbols))
	if err != nil {
		_ = factory.Close()
		return nil, err
	}

	reader.postings, err = newStreamPostings(context.Background(), factory, int(toc.PostingsTable))
	if err != nil {
		_ = factory.Close()
		return nil, err
	}

	reader.fingerprintOffsets, err = reader.readFingerprintOffsetsTable(int(toc.FingerprintOffsets))
	if err != nil {
		_ = factory.Close()
		return nil, err
	}

	// Fallback used by not-yet-ported methods
	mmapReader, err := NewMmapFileReader(path)
	if err != nil {
		_ = factory.Close()
		return nil, err
	}
	reader.mmapReader = mmapReader

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
	return s.mmapReader.LabelValues(name, matchers...)
}

func (s StreamReader) LabelNames(matchers ...*labels.Matcher) ([]string, error) {
	return s.mmapReader.LabelNames(matchers...)
}

func (s StreamReader) LabelValueFor(id storage.SeriesRef, label string) (string, error) {
	return s.mmapReader.LabelValueFor(id, label)
}

func (s StreamReader) LabelNamesFor(ids ...storage.SeriesRef) ([]string, error) {
	return s.mmapReader.LabelNamesFor(ids...)
}

func (s StreamReader) Series(id storage.SeriesRef, from int64, through int64, lbls *labels.Labels, chks *[]ChunkMeta) (uint64, error) {
	return s.mmapReader.Series(id, from, through, lbls, chks)
}

func (s StreamReader) ChunkStats(id storage.SeriesRef, from, through int64, lbls *labels.Labels, by map[string]struct{}) (uint64, ChunkStats, error) {
	return s.mmapReader.ChunkStats(id, from, through, lbls, by)
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
	mmapErr := s.mmapReader.Close()
	factoryErr := s.factory.Close()
	return errors.Join(mmapErr, factoryErr)
}
