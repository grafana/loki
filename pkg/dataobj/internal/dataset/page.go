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
	PageData []byte

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

// MemPage holds an encoded (and optionally compressed) sequence of [Value]
// entries of a common type. Use [ColumnBuilder] to construct sets of pages.
type MemPage struct {
	Desc PageDesc // Description of the page.
	Data PageData // Data for the page.
}

var _ Page = (*MemPage)(nil)

// PageDesc implements [Page] and returns p.Desc.
func (p *MemPage) PageDesc() *PageDesc {
	return &p.Desc
}

// ReadPage implements [Page] and returns p.Data.
func (p *MemPage) ReadPage(_ context.Context) (PageData, error) {
	return p.Data, nil
}

var checksumTable = crc32.MakeTable(crc32.Castagnoli)

// reader returns a reader for decompressed page data. Reader returns an error
// if the CRC32 fails to validate.
func (p *MemPage) reader(compression datasetmd.CompressionType) (presence io.Reader, values io.ReadCloser, err error) {
	if actual := crc32.Checksum(p.Data, checksumTable); p.Desc.CRC32 != actual {
		return nil, nil, fmt.Errorf("invalid CRC32 checksum %x, expected %x", actual, p.Desc.CRC32)
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
		zr, err := getZstdDecoder()
		if err != nil {
			return nil, nil, err
		}

		decompressed := bufpool.Get(p.PageDesc().UncompressedSize)
		defer func() {
			// Return the buffer to the pool immediately if there was an error.
			// Otherwise, the buffer will be returned to the pool when the reader is
			// closed.
			if err != nil {
				bufpool.Put(decompressed)
			}
		}()

		// We use DecodeAll which supports concurrent calls with the same
		// decoder, unlike Decode.
		buf, err := zr.DecodeAll(compressedValuesData, decompressed.Bytes())
		if err != nil {
			return nil, nil, err
		}

		return bitmapReader, &closerFunc{Reader: bytes.NewReader(buf), onClose: func() error {
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

// getZstdDecoder lazily initializes a global Zstd decoder. It is only safe to
// use DecodeAll concurrently.
var getZstdDecoder = sync.OnceValues(func() (*zstd.Decoder, error) {
	// NOTE(rfratto): We used to use pooled decoders here with streaming decodes
	// (Decode rather than DecodeAll), but using pooled decoders made it
	// difficult to control total allocations.

	// Using a concurrency of 0 will use GOMAXPROCS workers.
	return zstd.NewReader(nil, zstd.WithDecoderConcurrency(0))
})
