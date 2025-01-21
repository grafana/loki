package dataset

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"sync"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/zstd"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

// Helper types.
type (
	// PageData holds the raw data for a page. Data is formatted as:
	//
	//   <uvarint(presence-bitmap-size)> <presence-bitmap> <values-data>
	//
	// The presence-bitmap is a bitmap-encoded sequence of booleans, where values
	// describe which rows are present (1) or nil (0). The presence bitmap is
	// always stored uncompressed.
	//
	// values-data is then the encoded and optionally compressed sequence of
	// non-NULL values.
	PageData []byte

	// PageInfo describes a page.
	PageInfo struct {
		UncompressedSize int    // UncompressedSize is the size of a page before compression.
		CompressedSize   int    // CompressedSize is the size of a page after compression.
		CRC32            uint32 // CRC32 checksum of the page after encoding and compression.
		RowCount         int    // RowCount is the number of rows in the page, including NULLs.
		ValuesCount      int    // ValuesCount is the number of non-NULL values in the page.

		Encoding datasetmd.EncodingType // Encoding used for values in the page.
		Stats    *datasetmd.Statistics  // Optional statistics for the page.
	}

	// Pages is a set of [Page]s.
	Pages []Page
)

// A Page holds an encoded and optionally compressed sequence of [Value]s
// within a [Column].
type Page interface {
	// PageInfo returns the metadata for the Page.
	PageInfo() *PageInfo

	// ReadPage returns the [PageData] for the Page.
	ReadPage(ctx context.Context) (PageData, error)
}

// MemPage holds an encoded (and optionally compressed) sequence of [Value]
// entries of a common type. Use [ColumnBuilder] to construct sets of pages.
type MemPage struct {
	Info PageInfo // Information about the page.
	Data PageData // Data for the page.
}

var _ Page = (*MemPage)(nil)

// PageInfo implements [Page] and returns p.Info.
func (p *MemPage) PageInfo() *PageInfo {
	return &p.Info
}

// ReadPage implements [Page] and returns p.Data.
func (p *MemPage) ReadPage(_ context.Context) (PageData, error) {
	return p.Data, nil
}

var checksumTable = crc32.MakeTable(crc32.Castagnoli)

// reader returns a reader for decompressed page data. Reader returns an error
// if the CRC32 fails to validate.
func (p *MemPage) reader(compression datasetmd.CompressionType) (presence io.Reader, values io.ReadCloser, err error) {
	if actual := crc32.Checksum(p.Data, checksumTable); p.Info.CRC32 != actual {
		return nil, nil, fmt.Errorf("invalid CRC32 checksum %x, expected %x", actual, p.Info.CRC32)
	}

	bitmapSize, n := binary.Uvarint(p.Data)
	if n <= 0 {
		return nil, nil, fmt.Errorf("reading presence bitmap size: %w", err)
	}

	var (
		bitmapReader         = bytes.NewReader(p.Data[n : n+int(bitmapSize)])
		compressedDataReader = bytes.NewReader(p.Data[n+int(bitmapSize):])
	)

	switch compression {
	case datasetmd.COMPRESSION_TYPE_UNSPECIFIED, datasetmd.COMPRESSION_TYPE_NONE:
		return bitmapReader, io.NopCloser(compressedDataReader), nil

	case datasetmd.COMPRESSION_TYPE_SNAPPY:
		sr := snappyPool.Get().(*snappy.Reader)
		sr.Reset(compressedDataReader)
		return bitmapReader, &closerFunc{Reader: sr, closeFunc: func() error {
			snappyPool.Put(sr)
			return nil
		}}, nil

	case datasetmd.COMPRESSION_TYPE_ZSTD:
		return bitmapReader, &fixedZstdReader{
			page: p,
			data: p.Data[n+int(bitmapSize):],
		}, nil
	}

	panic(fmt.Sprintf("dataset.MemPage.reader: unknown compression type %q", compression.String()))
}

type closerFunc struct {
	io.Reader
	closeFunc func() error
}

func (cf *closerFunc) Close() error { return cf.closeFunc() }

var snappyPool = sync.Pool{
	New: func() interface{} {
		return snappy.NewReader(nil)
	},
}

var globalZstdDecoder = func() *zstd.Decoder {
	d, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
	if err != nil {
		panic(err)
	}
	return d
}()

type fixedZstdReader struct {
	page *MemPage
	data []byte

	uncompressedBuf *bytes.Buffer
	closed          bool
}

func (r *fixedZstdReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, io.ErrClosedPipe
	}

	if r.uncompressedBuf != nil {
		return r.uncompressedBuf.Read(p)
	}

	r.uncompressedBuf = bytesbufferPool.Get().(*bytes.Buffer)
	r.uncompressedBuf.Reset()
	r.uncompressedBuf.Grow(r.page.Info.UncompressedSize)

	buf, err := globalZstdDecoder.DecodeAll(r.data, r.uncompressedBuf.AvailableBuffer())
	if err != nil {
		return 0, fmt.Errorf("decoding zstd: %w", err)
	}
	_, _ = r.uncompressedBuf.Write(buf)

	return r.uncompressedBuf.Read(p)
}

func (r *fixedZstdReader) Close() error {
	if r.uncompressedBuf != nil {
		bytesbufferPool.Put(r.uncompressedBuf)
		r.uncompressedBuf = nil
	}
	r.closed = true
	return nil
}

var bytesbufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}
