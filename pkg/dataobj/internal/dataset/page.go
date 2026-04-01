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

// openedPage holds encoded (but decompressed) data for a page.
type openedPage struct {
	PresenceData []byte
	ValueData    []byte
}

// open the page for decoding. The page is validated for its CRC32 checksum.
//
// The page value data will be decompressed with the given compression type.
// After the page has been fully consumed, call the returned io.Closer to
// release resources associated with the page. The openedPage must not be
// used after closing.
func (p *MemPage) open(compression datasetmd.CompressionType) (openedPage, io.Closer, error) {
	if actual := crc32.Checksum(p.Data, checksumTable); p.Desc.CRC32 != actual {
		return openedPage{}, nil, fmt.Errorf("invalid CRC32 checksum %x, expected %x", actual, p.Desc.CRC32)
	}

	bitmapSize, n := binary.Uvarint(p.Data)
	if n <= 0 {
		return openedPage{}, nil, fmt.Errorf("reading presence bitmap size: %w", io.ErrUnexpectedEOF)
	}

	var (
		presenceData         = p.Data[n : n+int(bitmapSize)]
		compressedValuesData = p.Data[n+int(bitmapSize):]
	)

	switch compression {
	case datasetmd.COMPRESSION_TYPE_UNSPECIFIED, datasetmd.COMPRESSION_TYPE_NONE:
		// Data is already uncompressed; nothing to do.
		return openedPage{
			PresenceData: presenceData,
			ValueData:    compressedValuesData,
		}, io.NopCloser(nil), nil

	case datasetmd.COMPRESSION_TYPE_SNAPPY:
		sr := snappyPool.Get().(*snappy.Reader)
		sr.Reset(bytes.NewReader(compressedValuesData))
		defer func() {
			sr.Reset(nil) // Release references to the buffer.
			snappyPool.Put(sr)
		}()

		// TODO(rfratto): Should this be switched out with using
		// [memory.Allocator]?
		decompressed := bufpool.Get(p.PageDesc().UncompressedSize)
		if _, err := io.Copy(decompressed, sr); err != nil {
			bufpool.Put(decompressed)
			return openedPage{}, nil, err
		}

		closer := &closerFunc{
			onClose: func() error {
				bufpool.Put(decompressed)
				return nil
			},
		}

		return openedPage{
			PresenceData: presenceData,
			ValueData:    decompressed.Bytes(),
		}, closer, nil

	case datasetmd.COMPRESSION_TYPE_ZSTD:
		zr, err := getZstdDecoder()
		if err != nil {
			return openedPage{}, nil, err
		}

		decompressed := bufpool.Get(p.PageDesc().UncompressedSize)

		// We use DecodeAll which supports concurrent calls with the same
		// decoder, unlike Decode.
		buf, err := zr.DecodeAll(compressedValuesData, decompressed.Bytes())
		if err != nil {
			bufpool.Put(decompressed)
			return openedPage{}, nil, err
		}

		closer := &closerFunc{
			onClose: func() error {
				bufpool.Put(decompressed)
				return nil
			},
		}

		return openedPage{
			PresenceData: presenceData,
			ValueData:    buf,
		}, closer, nil

	default:
		// We do *not* want to panic here, as we may be trying to read a page from
		// a newer format.
		return openedPage{}, nil, fmt.Errorf("unknown compression type %q", compression.String())
	}
}

// reader returns a reader for decompressed page data. Reader returns an error
// if the CRC32 fails to validate.
func (p *MemPage) reader(compression datasetmd.CompressionType) (presence io.Reader, values io.ReadCloser, err error) {
	opened, closer, err := p.open(compression)
	if err != nil {
		return nil, nil, err
	}

	presence = bytes.NewReader(opened.PresenceData)
	values = &closerFunc{
		Reader:  bytes.NewReader(opened.ValueData),
		onClose: closer.Close,
	}
	return presence, values, nil
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
