package postings

import (
	"errors"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// postingsEncoder encodes sorted posting entries into size-bounded sections,
// splitting at run boundaries. Stateless and reusable across flushes.
type postingsEncoder struct {
	pageSizeHint           int
	pageMaxRowCount        int
	targetSectionSizeBytes int
}

func newPostingsEncoder(pageSizeHint, pageMaxRowCount, targetSectionSizeBytes int) *postingsEncoder {
	return &postingsEncoder{
		pageSizeHint:           pageSizeHint,
		pageMaxRowCount:        pageMaxRowCount,
		targetSectionSizeBytes: targetSectionSizeBytes,
	}
}

// encodeBloomEntries splits sorted bloom entries into size-bounded sections,
// writing each to w, and returns the total bytes written.
func (p *postingsEncoder) encodeBloomEntries(w dataobj.SectionWriter, tenant string, entries []BloomEntry) (int64, error) {
	var total int64
	err := encodeInSections(entries, p.targetSectionSizeBytes, bloomEntrySize, func(section []BloomEntry) error {
		n, err := p.writeSection(w, tenant, section, nil)
		if err != nil {
			return err
		}
		total += n
		return nil
	})
	return total, err
}

// encodeLabelEntries splits sorted label entries into size-bounded sections,
// writing each to w, and returns the total bytes written.
func (p *postingsEncoder) encodeLabelEntries(w dataobj.SectionWriter, tenant string, entries []LabelEntry) (int64, error) {
	var total int64
	err := encodeInSections(entries, p.targetSectionSizeBytes, labelEntrySize, func(section []LabelEntry) error {
		n, err := p.writeSection(w, tenant, nil, section)
		if err != nil {
			return err
		}
		total += n
		return nil
	})
	return total, err
}

// encodeInSections splits sorted entries into size-bounded sections, cutting at
// any row boundary once accumulated estimated size reaches target. A single
// entry larger than target becomes its own section.
func encodeInSections[E any](entries []E, target int, sizeOf func(E) int, emit func([]E) error) error {
	if len(entries) == 0 {
		return nil
	}
	start := 0
	size := 0
	for i := range entries {
		if i > start && size >= target {
			if err := emit(entries[start:i]); err != nil {
				return err
			}
			start = i
			size = 0
		}
		size += sizeOf(entries[i])
	}
	return emit(entries[start:])
}

// bloomEntrySize estimates the encoded size of one bloom entry.
func bloomEntrySize(e BloomEntry) int {
	return 5*8 + len(e.ObjectPath) + len(e.ColumnName) + len(e.BloomFilter) + len(e.StreamIDBitmap)
}

// labelEntrySize estimates the encoded size of one label entry.
func labelEntrySize(e LabelEntry) int {
	return 5*8 + len(e.ObjectPath) + len(e.ColumnName) + len(e.LabelValue) + len(e.StreamIDBitmap)
}

// writeSection encodes a single-kind section (blooms or labels) into a columnar
// encoder, flushes to w, and returns bytes written. Encoder is reset on return.
func (p *postingsEncoder) writeSection(w dataobj.SectionWriter, tenant string, blooms []BloomEntry, labels []LabelEntry) (int64, error) {
	var enc columnar.Encoder
	defer enc.Reset()
	if err := p.encodeColumns(blooms, labels, &enc); err != nil {
		return 0, err
	}
	enc.SetTenant(tenant)
	return enc.Flush(w)
}

// encodeColumns encodes bloom and label entries into the provided columnar
// encoder. Bloom entries (Kind=0) first, label entries (Kind=1) second.
func (p *postingsEncoder) encodeColumns(bloomEntries []BloomEntry, labelEntries []LabelEntry, enc *columnar.Encoder) error {
	// Build column builders for all 10 columns.

	// kind is a 2-value flag (0=bloom, 1=label). With sorted rows (blooms first,
	// then labels), deltas are almost all zeros — ZSTD compresses these runs very
	// well, unlike delta encodings on our other int64 cols.
	kindBuilder, err := dataset.NewColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint:    p.pageSizeHint,
		PageMaxRowCount: p.pageMaxRowCount,
		Type: dataset.ColumnType{
			Physical: datasetmd.PHYSICAL_TYPE_INT64,
			Logical:  ColumnTypeKind.String(),
		},
		Encoding:    datasetmd.ENCODING_TYPE_DELTA,
		Compression: datasetmd.COMPRESSION_TYPE_ZSTD,
		Statistics: dataset.StatisticsOptions{
			StoreRangeStats: true,
		},
	})
	if err != nil {
		return fmt.Errorf("creating kind column: %w", err)
	}

	objectPathBuilder, err := binaryColumnBuilder(ColumnTypeObjectPath, p.pageSizeHint, p.pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating object_path column: %w", err)
	}

	sectionIndexBuilder, err := numberColumnBuilder(ColumnTypeSectionIndex, p.pageSizeHint, p.pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating section_index column: %w", err)
	}

	columnNameBuilder, err := binaryColumnBuilder(ColumnTypeColumnName, p.pageSizeHint, p.pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating column_name column: %w", err)
	}

	labelValueBuilder, err := binaryColumnBuilder(ColumnTypeLabelValue, p.pageSizeHint, p.pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating label_value column: %w", err)
	}

	bloomFilterBuilder, err := dataset.NewColumnBuilder("", dataset.BuilderOptions{
		PageSizeHint:    p.pageSizeHint,
		PageMaxRowCount: p.pageMaxRowCount,
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

	streamIDBitmapBuilder, err := binaryColumnBuilder(ColumnTypeStreamIDBitmap, p.pageSizeHint, p.pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating stream_id_bitmap column: %w", err)
	}

	uncompressedSizeBuilder, err := numberColumnBuilder(ColumnTypeUncompressedSize, p.pageSizeHint, p.pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating uncompressed_size column: %w", err)
	}

	minTimestampBuilder, err := numberColumnBuilder(ColumnTypeMinTimestamp, p.pageSizeHint, p.pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating min_timestamp column: %w", err)
	}

	maxTimestampBuilder, err := numberColumnBuilder(ColumnTypeMaxTimestamp, p.pageSizeHint, p.pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating max_timestamp column: %w", err)
	}

	// Compute the max bitmap length across both bloom and label entries for normalization.
	maxBitmapLen := 0
	for _, e := range bloomEntries {
		if len(e.StreamIDBitmap) > maxBitmapLen {
			maxBitmapLen = len(e.StreamIDBitmap)
		}
	}
	for _, e := range labelEntries {
		if len(e.StreamIDBitmap) > maxBitmapLen {
			maxBitmapLen = len(e.StreamIDBitmap)
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
		_ = kindBuilder.Append(rowIdx, dataset.Int64Value(int64(KindBloom)))
		_ = objectPathBuilder.Append(rowIdx, dataset.BinaryValue([]byte(e.ObjectPath)))
		_ = sectionIndexBuilder.Append(rowIdx, dataset.Int64Value(e.SectionIndex))
		_ = columnNameBuilder.Append(rowIdx, dataset.BinaryValue([]byte(e.ColumnName)))
		_ = labelValueBuilder.Append(rowIdx, dataset.Value{}) // null for bloom
		_ = bloomFilterBuilder.Append(rowIdx, dataset.BinaryValue(e.BloomFilter))
		_ = streamIDBitmapBuilder.Append(rowIdx, dataset.BinaryValue(normalizeBitmap(e.StreamIDBitmap)))
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
		_ = streamIDBitmapBuilder.Append(rowIdx, dataset.BinaryValue(normalizeBitmap(e.StreamIDBitmap)))
		_ = uncompressedSizeBuilder.Append(rowIdx, dataset.Int64Value(e.UncompressedSize))
		_ = minTimestampBuilder.Append(rowIdx, dataset.Int64Value(e.MinTimestamp))
		_ = maxTimestampBuilder.Append(rowIdx, dataset.Int64Value(e.MaxTimestamp))
		rowIdx++
	}

	enc.SetSortInfo(&datasetmd.SortInfo{
		ColumnSorts: []*datasetmd.SortInfo_ColumnSort{
			{ColumnIndex: 0, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // kind
			{ColumnIndex: 3, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // column_name
			{ColumnIndex: 4, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // label_value
			{ColumnIndex: 8, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // min_timestamp
			{ColumnIndex: 9, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // max_timestamp
			{ColumnIndex: 1, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // object_path
			{ColumnIndex: 2, Direction: datasetmd.SORT_DIRECTION_ASCENDING}, // section_index
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
