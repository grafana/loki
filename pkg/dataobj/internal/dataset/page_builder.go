package dataset

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
)

// pageBuilder accumulates sequences of [Value] in memory until reaching a
// configurable size limit. A [MemPage] can then be created from a PageBuiler
// by calling [pageBuilder.Flush].
type pageBuilder struct {
	// Each pageBuilder writes two sets of data.
	//
	// The first set of data is a presence bitmap which tells readers which rows
	// are present. Use use 1 to indicate presence and 0 to indicate absence
	// (NULL). This bitmap always uses bitmap encoding regardless of the encoding
	// type used for values.
	//
	// The second set of data is the encoded set of non-NULL values. As an
	// optimization, the zero value is treated as NULL.
	//
	// The two sets of data are accmumulated into separate buffers, with the
	// presence bitmap being written uncompresed and the values being written
	// with the configured compression type, if any.
	//
	// To orchestrate building two sets of data, we have a few components:
	//
	// * The final buffers which hold encoded and potentially compressed data.
	// * The writer performing compression for values.
	// * The encoders that write values.

	opts BuilderOptions

	presenceBuffer *bytes.Buffer // presenceBuffer holds the encoded presence bitmap.
	valuesBuffer   *bytes.Buffer // valuesBuffer holds encoded and optionally compressed values.

	valuesWriter *compressWriter // Compresses data and writes to valuesBuffer.

	presenceEnc *bitmapEncoder
	valuesEnc   valueEncoder

	rows   int // Number of rows appended to the builder.
	values int // Number of non-NULL values appended to the builder.

	// minValue and maxValue track the minimum and maximum values appended to the
	// page. These are used to compute statistics for the page if requested.
	minValue, maxValue Value
}

// newPageBuilder creates a new pageBuilder that stores a sequence of [Value]s.
// newPageBuilder returns an error if there is no encoder available for the
// combination of opts.Value and opts.Encoding.
func newPageBuilder(opts BuilderOptions) (*pageBuilder, error) {
	var (
		presenceBuffer = bytes.NewBuffer(nil)
		valuesBuffer   = bytes.NewBuffer(make([]byte, 0, opts.PageSizeHint))

		valuesWriter = newCompressWriter(valuesBuffer, opts.Compression, opts.CompressionOptions)
	)

	presenceEnc := newBitmapEncoder(presenceBuffer)
	valuesEnc, ok := newValueEncoder(opts.Value, opts.Encoding, valuesWriter)
	if !ok {
		return nil, fmt.Errorf("no encoder available for %s/%s", opts.Value, opts.Encoding)
	}

	return &pageBuilder{
		opts: opts,

		presenceBuffer: presenceBuffer,
		valuesBuffer:   valuesBuffer,

		valuesWriter: valuesWriter,

		presenceEnc: presenceEnc,
		valuesEnc:   valuesEnc,
	}, nil
}

// Append appends value into the pageBuilder. Append returns true if the data
// was appended; false if the pageBuilder is full.
func (b *pageBuilder) Append(value Value) bool {
	if value.IsNil() || value.IsZero() {
		return b.AppendNull()
	}

	// We can't accurately know whether adding value would tip us over the page
	// size: we don't know the current state of the encoders and we don't know
	// for sure how much space value will fill.
	//
	// We use a rough estimate which will tend to overshoot the page size, making
	// sure we rarely go over.
	if sz := b.EstimatedSize(); sz > 0 && sz+valueSize(value) > b.opts.PageSizeHint {
		return false
	}

	// Update statistics. We only do this for non-NULL values,
	// otherwise NULL would always be the min for columns that contain a single
	// NULL.
	b.accumulateStatistics(value)

	// The following calls won't fail; they only return errors when the
	// underlying writers fail, which ours cannot.
	if err := b.presenceEnc.Encode(Uint64Value(1)); err != nil {
		panic(fmt.Sprintf("pageBuilder.Append: encoding presence bitmap entry: %v", err))
	}
	if err := b.valuesEnc.Encode(value); err != nil {
		panic(fmt.Sprintf("pageBuilder.Append: encoding value: %v", err))
	}

	b.rows++
	b.values++
	return true
}

// AppendNull appends a NULL value to the Builder. AppendNull returns true if
// the NULL was appended, or false if the Builder is full.
func (b *pageBuilder) AppendNull() bool {
	// See comment in Append for why we can only estimate the cost of appending a
	// value.
	//
	// Here we assume appending a NULL costs one byte, but in reality most NULLs
	// have no cost depending on the state of our bitmap encoder.
	if sz := b.EstimatedSize(); sz > 0 && sz+1 > b.opts.PageSizeHint {
		return false
	}

	// The following call won't fail; it only returns an error when the
	// underlying writer fails, which ours cannot.
	if err := b.presenceEnc.Encode(Uint64Value(0)); err != nil {
		panic(fmt.Sprintf("Builder.AppendNull: encoding presence bitmap entry: %v", err))
	}

	b.rows++
	return true
}

func (b *pageBuilder) accumulateStatistics(value Value) {
	// As a small optimization, we only update min/max values if we're intending
	// on populating them in statistics. This avoids unnecessary comparisons for very
	// large values.
	if b.opts.Statistics.StoreRangeStats {
		b.updateMinMax(value)
	}
}

func (b *pageBuilder) updateMinMax(value Value) {
	// We'll init minValue/maxValue if this is our first non-NULL value (b.values == 0).
	// This allows us to only avoid comparing against NULL values, which would lead to
	// NULL always being the min.
	if b.values == 0 || CompareValues(value, b.minValue) < 0 {
		b.minValue = value
	}
	if b.values == 0 || CompareValues(value, b.maxValue) > 0 {
		b.maxValue = value
	}
}

func valueSize(v Value) int {
	switch v.Type() {
	case datasetmd.VALUE_TYPE_INT64:
		// Assuming that int64s are written as varints.
		return streamio.VarintSize(v.Int64())

	case datasetmd.VALUE_TYPE_UINT64:
		// Assuming that uint64s are written as uvarints.
		return streamio.UvarintSize(v.Uint64())

	case datasetmd.VALUE_TYPE_STRING:
		// Assuming that strings are PLAIN encoded using their length and bytes.
		str := v.String()
		return binary.Size(len(str)) + len(str)
	case datasetmd.VALUE_TYPE_BYTE_ARRAY:
		arr := v.ByteArray()
		return binary.Size(len(arr)) + len(arr)
	}

	return 0
}

// EstimatedSize returns the estimated uncompressed size of the builder in
// bytes.
func (b *pageBuilder) EstimatedSize() int {
	// This estimate doesn't account for any values in encoders which haven't
	// been flushed yet. However, encoder buffers are usually small enough that
	// we wouldn't massively overshoot our estimate.
	return b.presenceBuffer.Len() + b.valuesWriter.BytesWritten()
}

// Rows returns the number of rows appended to the pageBuilder.
func (b *pageBuilder) Rows() int { return b.rows }

// Flush converts data in pageBuilder into a [MemPage], and returns it.
// Afterwards, pageBuilder is reset to a fresh state and can be reused. Flush
// returns an error if the pageBuilder is empty.
//
// To avoid computing useless stats, the Stats field of the returned Page is
// unset. If stats are needed for a page, callers should compute them by
// iterating over the returned Page.
func (b *pageBuilder) Flush() (*MemPage, error) {
	if b.rows == 0 {
		return nil, fmt.Errorf("no data to flush")
	}

	// Before we can build the page we need to finish flushing our encoders and
	// writers.
	//
	// We must call [compressWriter.Close] to ensure that Zstd writers write a
	// proper EOF marker, otherwise synchronous decoding can't be used.
	// compressWriters can continue to reset and reused after closing, so this is
	// safe.
	if err := b.presenceEnc.Flush(); err != nil {
		return nil, fmt.Errorf("flushing presence encoder: %w", err)
	} else if err := b.valuesEnc.Flush(); err != nil {
		return nil, fmt.Errorf("flushing values encoder: %w", err)
	} else if err := b.valuesWriter.Close(); err != nil {
		return nil, fmt.Errorf("flushing values writer: %w", err)
	}

	// The final data of our page is the combination of the presence bitmap and
	// the values. To denote when one ends and the other begins, we prepend the
	// data with the size of the presence bitmap as a uvarint. See the doc
	// comment of [PageData] for more information.
	var (
		headerSize   = streamio.UvarintSize(uint64(b.presenceBuffer.Len()))
		presenceSize = b.presenceBuffer.Len()
		valuesSize   = b.valuesBuffer.Len()

		finalData = bytes.NewBuffer(make([]byte, 0, headerSize+presenceSize+valuesSize))
	)

	if err := streamio.WriteUvarint(finalData, uint64(b.presenceBuffer.Len())); err != nil {
		return nil, fmt.Errorf("writing presence buffer size: %w", err)
	} else if _, err := b.presenceBuffer.WriteTo(finalData); err != nil {
		return nil, fmt.Errorf("writing presence buffer: %w", err)
	} else if _, err := b.valuesBuffer.WriteTo(finalData); err != nil {
		return nil, fmt.Errorf("writing values buffer: %w", err)
	}

	checksum := crc32.Checksum(finalData.Bytes(), checksumTable)

	page := MemPage{
		Info: PageInfo{
			UncompressedSize: headerSize + presenceSize + b.valuesWriter.BytesWritten(),
			CompressedSize:   finalData.Len(),
			CRC32:            checksum,
			RowCount:         b.rows,
			ValuesCount:      b.values,

			Encoding: b.opts.Encoding,
			Stats:    b.buildStats(),
		},

		Data: finalData.Bytes(),
	}

	b.Reset() // Reset state before returning.
	return &page, nil
}

func (b *pageBuilder) buildStats() *datasetmd.Statistics {
	var stats datasetmd.Statistics
	if b.opts.Statistics.StoreRangeStats {
		b.buildRangeStats(&stats)
		return &stats
	}
	return nil
}

func (b *pageBuilder) buildRangeStats(dst *datasetmd.Statistics) {

	minValueBytes, err := b.minValue.MarshalBinary()
	if err != nil {
		panic(fmt.Sprintf("pageBuilder.buildStats: failed to marshal min value: %s", err))
	}
	maxValueBytes, err := b.maxValue.MarshalBinary()
	if err != nil {
		panic(fmt.Sprintf("pageBuilder.buildStats: failed to marshal max value: %s", err))
	}

	dst.MinValue = minValueBytes
	dst.MaxValue = maxValueBytes
}

// Reset resets the pageBuilder to a fresh state, allowing it to be reused.
func (b *pageBuilder) Reset() {
	b.presenceBuffer.Reset()
	b.valuesBuffer.Reset()
	b.valuesWriter.Reset(b.valuesBuffer)
	b.presenceBuffer.Reset()
	b.valuesEnc.Reset(b.valuesWriter)
	b.rows = 0
	b.values = 0
	b.minValue = Value{}
	b.maxValue = Value{}
}
