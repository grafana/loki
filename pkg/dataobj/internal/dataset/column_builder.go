package dataset

import (
	"fmt"

	"github.com/klauspost/compress/zstd"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

// BuilderOptions configures common settings for building pages.
type BuilderOptions struct {
	// PageSizeHint is the soft limit for the size of the page. Builders try to
	// fill pages as close to this size as possible, but the actual size may be
	// slightly larger or smaller.
	PageSizeHint int

	// Value is the value type of data to write.
	Value datasetmd.ValueType

	// Encoding is the encoding algorithm to use for values.
	Encoding datasetmd.EncodingType

	// Compression is the compression algorithm to use for values.
	Compression datasetmd.CompressionType

	// CompressionOptions holds optional configuration for compression.
	CompressionOptions CompressionOptions

	// StoreRangeStats indicates whether to store value range statistics for the
	// column and pages.
	StoreRangeStats bool
}

// CompressionOptions customizes the compressor used when building pages.
type CompressionOptions struct {
	// Zstd holds encoding options for Zstd compression. Only used for
	// [datasetmd.COMPRESSION_TYPE_ZSTD].
	Zstd []zstd.EOption
}

// A ColumnBuilder builds a sequence of [Value] entries of a common type into a
// column. Values are accumulated into a buffer and then flushed into
// [MemPage]s once the size of data exceeds a configurable limit.
type ColumnBuilder struct {
	name string
	opts BuilderOptions

	rows int // Total number of rows in the column.

	pages   []*MemPage
	builder *pageBuilder
}

// NewColumnBuilder creates a new ColumnBuilder from the optional name and
// provided options. NewColumnBuilder returns an error if the options are
// invalid.
func NewColumnBuilder(name string, opts BuilderOptions) (*ColumnBuilder, error) {
	builder, err := newPageBuilder(opts)
	if err != nil {
		return nil, fmt.Errorf("creating page builder: %w", err)
	}

	return &ColumnBuilder{
		name: name,
		opts: opts,

		builder: builder,
	}, nil
}

// Append adds a new value into cb with the given zero-indexed row number. If
// the row number is higher than the current number of rows in cb, null values
// are added up to the new row.
//
// Append returns an error if the row number is out-of-order.
func (cb *ColumnBuilder) Append(row int, value Value) error {
	if row < cb.rows {
		return fmt.Errorf("row %d is older than current row %d", row, cb.rows)
	}

	// We give two attempts to append the data to the buffer; if the buffer is
	// full, we cut a page and then append to the newly reset buffer.
	//
	// The second iteration should never fail, as the buffer will always be empty
	// then.
	for range 2 {
		if cb.append(row, value) {
			cb.rows = row + 1
			return nil
		}

		cb.flushPage()
	}

	panic("ColumnBuilder.Append: failed to append value to fresh buffer")
}

// EstimatedSize returns the estimated size of all data in cb. EstimatedSize
// includes the compressed size of all cut pages in cb, followed by the size
// estimate of the in-progress page.
//
// Because compression isn't considered for the in-progress page, EstimatedSize
// tends to overestimate the actual size after flushing.
func (cb *ColumnBuilder) EstimatedSize() int {
	var size int
	for _, p := range cb.pages {
		size += p.Info.CompressedSize
	}
	size += cb.builder.EstimatedSize()
	return size
}

// Backfill adds NULLs into cb up to (but not including) the provided row
// number. If values exist up to the provided row number, Backfill does
// nothing.
func (cb *ColumnBuilder) Backfill(row int) {
	// We give two attempts to append the data to the buffer; if the buffer is
	// full, we cut a page and then append again to the newly reset buffer.
	//
	// The second iteration should never fail, as the buffer will always be
	// empty.
	for range 2 {
		if cb.backfill(row) {
			return
		}
		cb.flushPage()
	}

	panic("ColumnBuilder.Backfill: failed to backfill buffer")
}

func (cb *ColumnBuilder) backfill(row int) bool {
	for row > cb.rows {
		if !cb.builder.AppendNull() {
			return false
		}
		cb.rows++
	}

	return true
}

func (cb *ColumnBuilder) append(row int, value Value) bool {
	// Backfill up to row.
	if !cb.backfill(row) {
		return false
	}
	return cb.builder.Append(value)
}

// Flush converts data in cb into a [MemColumn]. Afterwards, cb is reset to a
// fresh state and can be reused.
func (cb *ColumnBuilder) Flush() (*MemColumn, error) {
	cb.flushPage()

	info := ColumnInfo{
		Name: cb.name,
		Type: cb.opts.Value,

		Compression: cb.opts.Compression,
		Statistics:  cb.buildStats(),
	}

	for _, page := range cb.pages {
		info.RowsCount += page.Info.RowCount
		info.ValuesCount += page.Info.ValuesCount
		info.CompressedSize += page.Info.CompressedSize
		info.UncompressedSize += page.Info.UncompressedSize
	}

	column := &MemColumn{
		Info:  info,
		Pages: cb.pages,
	}

	cb.Reset()
	return column, nil
}

func (cb *ColumnBuilder) buildStats() *datasetmd.Statistics {
	if !cb.opts.StoreRangeStats {
		return nil
	}

	var stats datasetmd.Statistics

	var minValue, maxValue Value

	for i, page := range cb.pages {
		if page.Info.Stats == nil {
			// This should never hit; if cb.opts.StoreRangeStats is true, then
			// page.Info.Stats will be populated.
			panic("ColumnBuilder.buildStats: page missing stats")
		}

		var pageMin, pageMax Value

		if err := pageMin.UnmarshalBinary(page.Info.Stats.MinValue); err != nil {
			panic(fmt.Sprintf("ColumnBuilder.buildStats: failed to unmarshal min value: %s", err))
		} else if err := pageMax.UnmarshalBinary(page.Info.Stats.MaxValue); err != nil {
			panic(fmt.Sprintf("ColumnBuilder.buildStats: failed to unmarshal max value: %s", err))
		}

		if i == 0 {
			minValue, maxValue = pageMin, pageMax
			continue
		}

		if CompareValues(pageMin, minValue) < 0 {
			minValue = pageMin
		}
		if CompareValues(pageMax, maxValue) > 0 {
			maxValue = pageMax
		}
	}

	var err error
	if stats.MinValue, err = minValue.MarshalBinary(); err != nil {
		panic(fmt.Sprintf("ColumnBuilder.buildStats: failed to marshal min value: %s", err))
	}
	if stats.MaxValue, err = maxValue.MarshalBinary(); err != nil {
		panic(fmt.Sprintf("ColumnBuilder.buildStats: failed to marshal max value: %s", err))
	}
	return &stats
}

func (cb *ColumnBuilder) flushPage() {
	if cb.builder.Rows() == 0 {
		return
	}

	page, err := cb.builder.Flush()
	if err != nil {
		// Flush should only return an error when it's empty, which we already
		// ensure it's not in the lines above.
		panic(fmt.Sprintf("failed to flush page: %s", err))
	}
	cb.pages = append(cb.pages, page)
}

// Reset clears all data in cb and resets it to a fresh state.
func (cb *ColumnBuilder) Reset() {
	cb.rows = 0
	cb.pages = nil
	cb.builder.Reset()
}
