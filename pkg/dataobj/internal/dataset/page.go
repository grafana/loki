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
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
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
	PageData interface {
		// Bytes returns the underlying data for the page. Callers must not
		// retain references to this slice after calling Close.
		Bytes() []byte

		// Close the PageData. Implementations of PageData may use Close
		// to return shared memory. After closing PageData, future calls to
		// Bytes return nil.
		//
		// If the implementation of PageData does not support closing,
		// Close does nothing.
		Close() error
	}

	// PageDesc describes a page.
	PageDesc struct {
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
	// PageDesc returns the metadata for the Page.
	PageDesc() *PageDesc

	// ReadPage returns the [PageData] for the Page.
	ReadPage(ctx context.Context) (PageData, error)
}

type byteBuffer struct {
	data []byte
}

// Resize changes the len of buf to n. If n is greater than the
// capacity of buf, Resize allocates a new slice.
func (buf *byteBuffer) Resize(n int) {
	if n > len(buf.data) {
		tmp := make([]byte, n)
		copy(buf.data, tmp)
		buf.data = tmp
	} else {
		buf.data = buf.data[:n]
	}
}

// Bytes returns the underlying data of buf.
func (buf *byteBuffer) Bytes() []byte {
	return buf.data
}

var bufferPool = sync.Pool{
	New: func() any {
		return new(byteBuffer)
	},
}

type releasableData struct {
	buf *byteBuffer
}

func (rd *releasableData) Bytes() []byte { return rd.buf.Bytes() }

func (rd *releasableData) Close() error {
	if rd.buf != nil {
		bufferPool.Put(rd.buf)
		rd.buf = nil
	}
	return nil
}

// MemPage holds an encoded (and optionally compressed) sequence of [Value]
// entries of a common type. Use [ColumnBuilder] to construct sets of pages.
type MemPage struct {
	Desc PageDesc        // Description of the page.
	data *releasableData // Data for the page.
}

var _ Page = (*MemPage)(nil)

// PageDesc implements [Page] and returns p.Desc.
func (p *MemPage) PageDesc() *PageDesc {
	return &p.Desc
}

// ReadPage implements [Page] and returns p.Data.
func (p *MemPage) ReadPage(_ context.Context) (PageData, error) {
	return p, nil
}

func (p MemPage) Bytes() []byte {
	return p.data.Bytes()
}

func (p MemPage) Close() error {
	if p.data != nil {
		return p.data.Close()
	}
	return nil
}

func InitMemPage(desc PageDesc, data []byte) MemPage {
	buf := bufferPool.Get().(*byteBuffer)
	buf.Resize(len(data))
	copy(buf.data, data)
	rd := &releasableData{buf: buf}
	return MemPage{desc, rd}
}

var checksumTable = crc32.MakeTable(crc32.Castagnoli)

// reader returns a reader for decompressed page data. Reader returns an error
// if the CRC32 fails to validate.
func (p *MemPage) reader(compression datasetmd.CompressionType) (presence io.Reader, values io.ReadCloser, err error) {
	if actual := crc32.Checksum(p.Bytes(), checksumTable); p.Desc.CRC32 != actual {
		return nil, nil, fmt.Errorf("invalid CRC32 checksum %x, expected %x", actual, p.Desc.CRC32)
	}

	bitmapSize, n := binary.Uvarint(p.Bytes())
	if n <= 0 {
		return nil, nil, fmt.Errorf("reading presence bitmap size: %w", err)
	}

	var (
		bitmapData             = p.Bytes()[n : n+int(bitmapSize)]
		compressedValuesData   = p.Bytes()[n+int(bitmapSize):]
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
		defer func() {
			_ = zr.Reset(nil) // Allow releasing the buffer.
			zstdPool.Put(zr)
		}()

		decompressed := bufpool.Get(p.PageDesc().UncompressedSize)
		defer func() {
			// Return the buffer to the pool immediately if there was an error.
			// Otherwise, the buffer will be returned to the pool when the reader is
			// closed.
			if err != nil {
				bufpool.Put(decompressed)
			}
		}()

		_, err := io.Copy(decompressed, zr)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to decompress page: %w", err)
		}

		return bitmapReader, &closerFunc{Reader: decompressed, onClose: func() error {
			bufpool.Put(decompressed)
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
		runtime.AddCleanup(zw, func(zr *zstd.Decoder) {
			zr.Close()
		}, zr)

		return zw
	},
}
