package postings

import (
	"errors"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// columnarEncode encodes bloom and label entries into the provided columnar
// encoder. Bloom entries (Kind=0) are encoded first, followed by label entries
// (Kind=1), using the same 10 column builders.
//
// pageSizeHint and pageMaxRowCount control the page splitting behaviour of the
// underlying column builders.
func columnarEncode(bloomEntries []*bloomPostingEntry, labelEntries []*labelPostingEntry, enc *columnar.Encoder, pageSizeHint, pageMaxRowCount int) error {
	// Build column builders for all 10 columns.

	// kind is a 2-value flag (0=bloom, 1=label). DELTA is the only encoding
	// available for INT64 in the dataset package. With sorted rows (blooms first,
	// then labels), deltas are almost all zeros — ZSTD compresses these runs very
	// well, unlike delta encodings on our other int64 cols
	kindBuilder, err := dataset.NewColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint:    pageSizeHint,
		PageMaxRowCount: pageMaxRowCount,
		Type: dataset.ColumnType{
			Physical: datasetmd.PHYSICAL_TYPE_INT64,
			Logical:  ColumnTypeKind.String(),
		},
		Encoding:    datasetmd.ENCODING_TYPE_DELTA,
		Compression: datasetmd.COMPRESSION_TYPE_ZSTD,
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

	bloomFilterBuilder, err := dataset.NewColumnBuilder("", dataset.BuilderOptions{
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

	// Compute the max bitmap length across both bloom and label entries for normalization.
	maxBitmapLen := 0
	for _, e := range bloomEntries {
		if b := e.BitmapBytes(); len(b) > maxBitmapLen {
			maxBitmapLen = len(b)
		}
	}
	for _, e := range labelEntries {
		if b := e.BitmapBytes(); len(b) > maxBitmapLen {
			maxBitmapLen = len(b)
		}
	}

	// normalizeBitmap pads a bitmap to maxBitmapLen.
	normalizeBitmap := func(b []byte) []byte {
		if len(b) == maxBitmapLen {
			return b
		}
		padded := make([]byte, maxBitmapLen)
		copy(padded, b)
		return padded
	}

	// Populate column builders: bloom entries first (Kind=0), then label entries (Kind=1).
	rowIdx := 0

	for _, e := range bloomEntries {
		bloomBytes, err := e.BloomBytes()
		if err != nil {
			return fmt.Errorf("marshaling bloom filter for column %q: %w", e.ColumnName, err)
		}

		_ = kindBuilder.Append(rowIdx, dataset.Int64Value(int64(KindBloom)))
		_ = objectPathBuilder.Append(rowIdx, dataset.BinaryValue([]byte(e.ObjectPath)))
		_ = sectionIndexBuilder.Append(rowIdx, dataset.Int64Value(e.SectionIndex))
		_ = columnNameBuilder.Append(rowIdx, dataset.BinaryValue([]byte(e.ColumnName)))
		_ = labelValueBuilder.Append(rowIdx, dataset.Value{}) // null for bloom
		_ = bloomFilterBuilder.Append(rowIdx, dataset.BinaryValue(bloomBytes))
		_ = streamIDBitmapBuilder.Append(rowIdx, dataset.BinaryValue(normalizeBitmap(e.BitmapBytes())))
		_ = uncompressedSizeBuilder.Append(rowIdx, dataset.Int64Value(e.UncompressedSize))
		_ = minTimestampBuilder.Append(rowIdx, dataset.Int64Value(e.MinTimestamp))
		_ = maxTimestampBuilder.Append(rowIdx, dataset.Int64Value(e.MaxTimestamp))
		rowIdx++
	}

	for _, e := range labelEntries {
		_ = kindBuilder.Append(rowIdx, dataset.Int64Value(int64(KindLabel)))
		_ = objectPathBuilder.Append(rowIdx, dataset.BinaryValue([]byte(e.ObjectPath)))
		_ = sectionIndexBuilder.Append(rowIdx, dataset.Int64Value(e.SectionIndex))
		_ = columnNameBuilder.Append(rowIdx, dataset.BinaryValue([]byte(e.ColumnName)))
		_ = labelValueBuilder.Append(rowIdx, dataset.BinaryValue([]byte(e.LabelValue)))
		_ = bloomFilterBuilder.Append(rowIdx, dataset.Value{}) // null for label
		_ = streamIDBitmapBuilder.Append(rowIdx, dataset.BinaryValue(normalizeBitmap(e.BitmapBytes())))
		_ = uncompressedSizeBuilder.Append(rowIdx, dataset.Int64Value(e.UncompressedSize))
		_ = minTimestampBuilder.Append(rowIdx, dataset.Int64Value(e.MinTimestamp))
		_ = maxTimestampBuilder.Append(rowIdx, dataset.Int64Value(e.MaxTimestamp))
		rowIdx++
	}

	// Set sort info: [kind(0), object_path(1), section_index(2), column_name(3), label_value(4)]
	// Column indices: kind=0, object_path=1, section_index=2, column_name=3, label_value=4,
	// bloom_filter=5, stream_id_bitmap=6, uncompressed_size=7, min_timestamp=8, max_timestamp=9.
	// Data is sorted by [kind, objectPath, sectionIndex, columnName, labelValue]; timestamps
	// are not part of the sort key.
	enc.SetSortInfo(&datasetmd.SortInfo{
		ColumnSorts: []*datasetmd.SortInfo_ColumnSort{
			{ColumnIndex: 0, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // kind
			{ColumnIndex: 1, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // object_path
			{ColumnIndex: 2, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // section_index
			{ColumnIndex: 3, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // column_name
			{ColumnIndex: 4, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // label_value
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

// binaryColumnBuilder creates a column builder for BINARY/PLAIN/ZSTD columns.
//
// Tag is empty: postings columns are all fixed (one column per Logical type),
// so Logical alone uniquely identifies the column. Setting Tag would duplicate
// it. Matches the convention used by streams.Reader for fixed columns.
func binaryColumnBuilder(logicalType ColumnType, pageSize, pageRowCount int) (*dataset.ColumnBuilder, error) {
	return dataset.NewColumnBuilder("", dataset.BuilderOptions{
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
//
// Tag is empty: see [binaryColumnBuilder] rationale.
func numberColumnBuilder(logicalType ColumnType, pageSize, pageRowCount int) (*dataset.ColumnBuilder, error) {
	return dataset.NewColumnBuilder("", dataset.BuilderOptions{
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
