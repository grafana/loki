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
		bitmapData           = p.Data[n : n+int(bitmapSize)]
		compressedValuesData = p.Data[n+int(bitmapSize):]

		bitmapReader           = bytes.NewReader(bitmapData)
		compressedValuesReader = bytes.NewReader(compressedValuesData)
	)

	switch compression {
	case datasetmd.COMPRESSION_TYPE_UNSPECIFIED, datasetmd.COMPRESSION_TYPE_NONE:
		return bitmapReader, io.NopCloser(compressedValuesReader), nil

	case datasetmd.COMPRESSION_TYPE_SNAPPY:
		sr := snappyPool.Get().(*snappy.Reader)
		sr.Reset(compressedValuesReader)
		return bitmapReader, &closerFunc{Reader: sr, onClose: func() error {
			sr.Reset(nil) // Allow releasing the buffer.
			snappyPool.Put(sr)
			return nil
		}}, nil

	case datasetmd.COMPRESSION_TYPE_ZSTD:
		zr := zstdPool.Get().(*zstdWrapper)
		if err := zr.Reset(compressedValuesReader); err != nil {
			// [zstd.Decoder.Reset] can fail if the underlying reader got closed.
			// This shouldn't happen in practice (we only close the reader when the
			// wrapper has been released from the pool), but we handle this for
			// safety and fall back to manually creating a new wrapper by calling New
			// directly.
			zr = zstdPool.New().(*zstdWrapper)
		}
		return bitmapReader, &closerFunc{Reader: zr, onClose: func() error {
			_ = zr.Reset(nil) // Allow releasing the buffer.
			zstdPool.Put(zr)
			return nil
		}}, nil

	default:
		// We do *not* want to panic here, as we may be trying to read a page from
		// a newer format.
		return nil, nil, fmt.Errorf("unknown compression type %q", compression.String())
	}
}

var snappyPool = sync.Pool{
	New: func() any {
		return snappy.NewReader(nil)
	},
}

type closerFunc struct {
	io.Reader
	onClose func() error
}

func (c *closerFunc) Close() error { return c.onClose() }

// zstdWrapper wraps around a [zstd.Decoder]. [zstd.Decoder] uses persistent
// goroutines for parallelized decoding, which prevents it from being garbage
// collected.
//
// Wrapping around the decoder permits using [runtime.AddCleanup] to detect
// when the wrapper is garbage collected and automatically closing the
// underlying decoder.
type zstdWrapper struct{ *zstd.Decoder }

var zstdPool = sync.Pool{
	New: func() any {
		// Despite the name of zstd.WithDecoderLowmem implying we're using more
		// memory, in practice we've seen it use both less memory and fewer
		// allocations than the default of true. As a result, setting it to false
		// increases read speed as it is less taxing on the garbage collector.
		zr, err := zstd.NewReader(nil, zstd.WithDecoderLowmem(false))
		if err != nil {
			panic(fmt.Sprintf("creating zstd reader: %v", err))
		}

		// See doc comment on [zstdWrapper] for why we're doing this.
		zw := &zstdWrapper{zr}
		runtime.AddCleanup(zw, func(zr *zstd.Decoder) { zr.Close() }, zr)

		return zw
	},
}
