package postings

import (
	"errors"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// ColumnarSectionEncoder returns a [SectionEncoder] that encodes [Posting] rows
// using [dataset.ColumnBuilder]s backed by a [columnar.Encoder].
//
// pageSizeHint and pageMaxRowCount control the page splitting behaviour of the
// underlying column builders.
func ColumnarSectionEncoder(pageSizeHint int, pageMaxRowCount int) SectionEncoder {
	return func(rows []Posting, enc *columnar.Encoder) error {
		return columnarEncode(rows, enc, pageSizeHint, pageMaxRowCount)
	}
}

// columnarEncode implements the core encoding logic.
func columnarEncode(rows []Posting, enc *columnar.Encoder, pageSizeHint, pageMaxRowCount int) error {
	// Build column builders for all 10 columns.

	kindBuilder, err := dataset.NewColumnBuilder(colKind, dataset.BuilderOptions{
		PageSizeHint:    pageSizeHint,
		PageMaxRowCount: pageMaxRowCount,
		Type: dataset.ColumnType{
			Physical: datasetmd.PHYSICAL_TYPE_INT64,
			Logical:  ColumnTypeKind.String(),
		},
		Encoding:    datasetmd.ENCODING_TYPE_PLAIN,
		Compression: datasetmd.COMPRESSION_TYPE_NONE,
	})
	if err != nil {
		return fmt.Errorf("creating kind column: %w", err)
	}

	objectPathBuilder, err := binaryColumnBuilder(ColumnTypeObjectPath, pageSizeHint, pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating object_path column: %w", err)
	}

	sectionIndexBuilder, err := numberColumnBuilder(ColumnTypeSectionIndex, pageSizeHint, pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating section_index column: %w", err)
	}

	columnNameBuilder, err := binaryColumnBuilder(ColumnTypeColumnName, pageSizeHint, pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating column_name column: %w", err)
	}

	labelValueBuilder, err := binaryColumnBuilder(ColumnTypeLabelValue, pageSizeHint, pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating label_value column: %w", err)
	}

	bloomFilterBuilder, err := dataset.NewColumnBuilder(colBloomFilter, dataset.BuilderOptions{
		PageSizeHint:    pageSizeHint,
		PageMaxRowCount: pageMaxRowCount,
		Type: dataset.ColumnType{
			Physical: datasetmd.PHYSICAL_TYPE_BINARY,
			Logical:  ColumnTypeBloomFilter.String(),
		},
		Encoding:    datasetmd.ENCODING_TYPE_PLAIN,
		Compression: datasetmd.COMPRESSION_TYPE_NONE, // bloom data is pre-compressed
	})
	if err != nil {
		return fmt.Errorf("creating bloom_filter column: %w", err)
	}

	streamIDBitmapBuilder, err := binaryColumnBuilder(ColumnTypeStreamIDBitmap, pageSizeHint, pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating stream_id_bitmap column: %w", err)
	}

	uncompressedSizeBuilder, err := numberColumnBuilder(ColumnTypeUncompressedSize, pageSizeHint, pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating uncompressed_size column: %w", err)
	}

	minTimestampBuilder, err := numberColumnBuilder(ColumnTypeMinTimestamp, pageSizeHint, pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating min_timestamp column: %w", err)
	}

	maxTimestampBuilder, err := numberColumnBuilder(ColumnTypeMaxTimestamp, pageSizeHint, pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating max_timestamp column: %w", err)
	}

	// Normalize stream ID bitmaps to the same length before encoding.
	normalizedBitmaps := columnarNormalizeBitmaps(rows)

	// Populate column builders row by row.
	for i, r := range rows {
		_ = kindBuilder.Append(i, dataset.Int64Value(int64(r.Kind)))
		_ = objectPathBuilder.Append(i, dataset.BinaryValue([]byte(r.ObjectPath)))
		_ = sectionIndexBuilder.Append(i, dataset.Int64Value(r.SectionIndex))
		_ = columnNameBuilder.Append(i, dataset.BinaryValue([]byte(r.ColumnName)))

		// label_value: null for bloom postings.
		if r.Kind == KindBloom {
			_ = labelValueBuilder.Append(i, dataset.Value{}) // null
		} else {
			_ = labelValueBuilder.Append(i, dataset.BinaryValue([]byte(r.LabelValue)))
		}

		// bloom_filter: null for label postings.
		if r.BloomFilter == nil {
			_ = bloomFilterBuilder.Append(i, dataset.Value{}) // null
		} else {
			_ = bloomFilterBuilder.Append(i, dataset.BinaryValue(r.BloomFilter))
		}

		_ = streamIDBitmapBuilder.Append(i, dataset.BinaryValue(normalizedBitmaps[i]))

		_ = uncompressedSizeBuilder.Append(i, dataset.Int64Value(r.UncompressedSize))
		_ = minTimestampBuilder.Append(i, dataset.Int64Value(r.MinTimestamp))
		_ = maxTimestampBuilder.Append(i, dataset.Int64Value(r.MaxTimestamp))
	}

	// Set sort info: [kind(0), column_name(3), label_value(4), min_timestamp(8), max_timestamp(9)]
	// Column indices: kind=0, object_path=1, section_index=2, column_name=3, label_value=4,
	// bloom_filter=5, stream_id_bitmap=6, uncompressed_size=7, min_timestamp=8, max_timestamp=9.
	enc.SetSortInfo(&datasetmd.SortInfo{
		ColumnSorts: []*datasetmd.SortInfo_ColumnSort{
			{ColumnIndex: 0, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // kind
			{ColumnIndex: 3, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // column_name
			{ColumnIndex: 4, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // label_value
			{ColumnIndex: 8, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // min_timestamp
			{ColumnIndex: 9, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // max_timestamp
		},
	})

	// Encode all columns.
	errs := make([]error, 0, 10)
	errs = append(errs, encodeColumn(enc, ColumnTypeKind, kindBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypeObjectPath, objectPathBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypeSectionIndex, sectionIndexBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypeColumnName, columnNameBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypeLabelValue, labelValueBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypeBloomFilter, bloomFilterBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypeStreamIDBitmap, streamIDBitmapBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypeUncompressedSize, uncompressedSizeBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypeMinTimestamp, minTimestampBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypeMaxTimestamp, maxTimestampBuilder))

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("encoding columns: %w", err)
	}
	return nil
}

// columnarNormalizeBitmaps pads all stream ID bitmaps to the same length (the
// maximum length) with zero bytes.
func columnarNormalizeBitmaps(rows []Posting) [][]byte {
	maxLen := 0
	for _, r := range rows {
		if len(r.StreamIDBitmap) > maxLen {
			maxLen = len(r.StreamIDBitmap)
		}
	}

	result := make([][]byte, len(rows))
	for i, r := range rows {
		if len(r.StreamIDBitmap) == maxLen {
			result[i] = r.StreamIDBitmap
		} else {
			padded := make([]byte, maxLen)
			copy(padded, r.StreamIDBitmap)
			result[i] = padded
		}
	}
	return result
}

// binaryColumnBuilder creates a column builder for BINARY/PLAIN/ZSTD columns.
func binaryColumnBuilder(logicalType ColumnType, pageSize, pageRowCount int) (*dataset.ColumnBuilder, error) {
	return dataset.NewColumnBuilder(logicalType.String(), dataset.BuilderOptions{
		PageSizeHint:    pageSize,
		PageMaxRowCount: pageRowCount,
		Type: dataset.ColumnType{
			Physical: datasetmd.PHYSICAL_TYPE_BINARY,
			Logical:  logicalType.String(),
		},
		Encoding:    datasetmd.ENCODING_TYPE_PLAIN,
		Compression: datasetmd.COMPRESSION_TYPE_ZSTD,
	})
}

// numberColumnBuilder creates a column builder for INT64/DELTA/NONE columns.
func numberColumnBuilder(logicalType ColumnType, pageSize, pageRowCount int) (*dataset.ColumnBuilder, error) {
	return dataset.NewColumnBuilder(logicalType.String(), dataset.BuilderOptions{
		PageSizeHint:    pageSize,
		PageMaxRowCount: pageRowCount,
		Type: dataset.ColumnType{
			Physical: datasetmd.PHYSICAL_TYPE_INT64,
			Logical:  logicalType.String(),
		},
		Encoding:    datasetmd.ENCODING_TYPE_DELTA,
		Compression: datasetmd.COMPRESSION_TYPE_NONE,
	})
}

// encodeColumn flushes builder and writes all its pages to enc.
func encodeColumn(enc *columnar.Encoder, columnType ColumnType, builder *dataset.ColumnBuilder) error {
	column, err := builder.Flush()
	if err != nil {
		return fmt.Errorf("flushing %s column: %w", columnType, err)
	}

	columnEnc, err := enc.OpenColumn(column.ColumnDesc())
	if err != nil {
		return fmt.Errorf("opening %s column encoder: %w", columnType, err)
	}
	defer func() {
		// Discard on defer for safety. This will return an error if we
		// successfully committed.
		_ = columnEnc.Discard()
	}()
	if len(column.Pages) == 0 {
		// Column has no data; discard.
		return nil
	}

	for _, page := range column.Pages {
		err := columnEnc.AppendPage(page)
		if err != nil {
			return fmt.Errorf("appending %s page: %w", columnType, err)
		}
	}

	return columnEnc.Commit()
}
