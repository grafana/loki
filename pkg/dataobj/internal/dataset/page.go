package dataset

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"runtime"
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
		return bitmapReader, newZstdReader(compressedDataReader), nil
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

// zstdReader implements [io.ReadCloser] for a [zstd.Decoder].
type zstdReader struct{ *zstd.Decoder }

// newZstdReader returns a new [io.ReadCloser] for a [zstd.Decoder].
func newZstdReader(r io.Reader) io.ReadCloser {
NextReader:
	dec := zstdReaderPool.Get().(*zstd.Decoder)
	if err := dec.Reset(r); err != nil {
		// Reset should only fail if the decoder was closed improperly. This
		// shouldn't happen, but if it does, we'll fall back to getting another
		// one.
		goto NextReader
	}

	return &zstdReader{Decoder: dec}
}

// Close implements [io.Closer].
func (r *zstdReader) Close() error {
	// Do *not* call [zstd.Decoder.Close] here! After Close is called, the
	// decoder can't be used anymore. Instead, we should reset the encoder to nil
	// and put it back into the pool.
	if err := r.Reset(nil); err != nil {
		return fmt.Errorf("resetting zstd reader: %w", err)
	}
	zstdReaderPool.Put(r.Decoder)
	return nil
}

var zstdReaderPool = sync.Pool{
	New: func() interface{} {
		zr, err := zstd.NewReader(nil)
		if err != nil {
			panic(fmt.Sprintf("zstd.NewReader: %v", err))
		}

		runtime.SetFinalizer(zr, func(zr *zstd.Decoder) { zr.Close() })
		return zr
	},
}
