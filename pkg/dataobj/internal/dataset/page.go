package dataset

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/zstd"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

// Helper types.
type (
	// PageData holds the raw data for a page.
	PageData []byte

	// PageInfo describes a page.
	PageInfo struct {
		UncompressedSize int    // UncompressedSize is the size of a page before compression.
		CompressedSize   int    // CompressedSize is the size of a page after compression.
		CRC32            uint32 // CRC32 checksum of the page after encoding and compression.
		RowCount         int    // RowCount is the number of rows in the page, including NULLs.

		Value       datasetmd.ValueType       // Type of values stored in the page.
		Compression datasetmd.CompressionType // Compression used for values in the page.
		Encoding    datasetmd.EncodingType    // Encoding used for values in the page.
		Stats       *datasetmd.Statistics     // Optional statistics for the page.
	}
)

// MemPage holds an encoded (and optionally compressed) sequence of [Value]
// entries of a common type. Use [ColumnBuilder] to construct sets of pages.
type MemPage struct {
	// Information about the page.
	Info PageInfo

	// Data for the page. Data is formatted as:
	//
	//   [uvarint(bitmapSize)][presenceBitmap][valueData]
	//
	// The presenceBitmap is a bitmap-encoded sequence of booleans, where values
	// describe which rows are present (1) or nil (0). presenceBitmap is always
	// stored uncompressed.
	//
	// valueData is then the encoded and optionally compressed sequence of
	// non-NULL values, whose type, compression, and encoding are specified by Value, Compression, and Encoding.
	Data PageData
}

var checksumTable = crc32.MakeTable(crc32.Castagnoli)

// reader returns a reader for decompressed page data. Reader returns an error
// if the CRc32 fails to validate.
func (p *MemPage) reader() (presence io.ReadCloser, values io.ReadCloser, err error) {
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

	switch p.Info.Compression {
	case datasetmd.COMPRESSION_TYPE_UNSPECIFIED, datasetmd.COMPRESSION_TYPE_NONE:
		return io.NopCloser(bitmapReader), io.NopCloser(compressedDataReader), nil

	case datasetmd.COMPRESSION_TYPE_SNAPPY:
		sr := snappy.NewReader(compressedDataReader)
		return io.NopCloser(bitmapReader), io.NopCloser(sr), nil

	case datasetmd.COMPRESSION_TYPE_ZSTD:
		zr, err := zstd.NewReader(compressedDataReader)
		if err != nil {
			return nil, nil, fmt.Errorf("opening zstd reader: %w", err)
		}
		return io.NopCloser(bitmapReader), newZstdReader(zr), nil
	}

	panic(fmt.Sprintf("dataset.MemPage.reader: unknown compression type %q", p.Info.Compression.String()))
}

// zstdReader implements [io.ReadCloser] for a [zstd.Decoder].
type zstdReader struct{ *zstd.Decoder }

// newZstdReader returns a new [io.ReadCloser] for a [zstd.Decoder].
func newZstdReader(dec *zstd.Decoder) io.ReadCloser {
	return &zstdReader{Decoder: dec}
}

// Close implements [io.Closer].
func (r *zstdReader) Close() error {
	r.Decoder.Close()
	return nil
}
