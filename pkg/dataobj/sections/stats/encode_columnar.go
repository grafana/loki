package stats

import (
	"errors"
	"fmt"
	"strings"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// ColumnarSectionEncoder returns a [SectionEncoder] that encodes [Stat] rows
// using [dataset.ColumnBuilder]s backed by a [columnar.Encoder].
//
// pageSizeHint and pageMaxRowCount control the page splitting behaviour of the
// underlying column builders.
func ColumnarSectionEncoder(pageSizeHint int, pageMaxRowCount int) SectionEncoder {
	return func(rows []Stat, enc *columnar.Encoder) error {
		return columnarEncode(rows, enc, pageSizeHint, pageMaxRowCount)
	}
}

// columnarEncode implements the core encoding logic.
func columnarEncode(rows []Stat, enc *columnar.Encoder, pageSizeHint, pageMaxRowCount int) error {
	if len(rows) == 0 {
		return nil
	}

	// Parse label keys from SortSchema. All rows within a section share the
	// same SortSchema (guaranteed by the builder being per-tenant and the
	// calculation pipeline producing rows with a config-driven schema).
	sortSchema := rows[0].SortSchema
	var labelKeys []string
	if sortSchema != "" {
		parts := strings.SplitSeq(sortSchema, ",")
		for k := range parts {
			if k != "" {
				labelKeys = append(labelKeys, k)
			}
		}
	}

	// Build fixed column builders (indices 0-6).
	objectPathBuilder, err := binaryColumnBuilder(ColumnTypeObjectPath, pageSizeHint, pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating object_path column: %w", err)
	}

	sectionIndexBuilder, err := numberColumnBuilder(ColumnTypeSectionIndex, pageSizeHint, pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating section_index column: %w", err)
	}

	sortSchemaBuilder, err := binaryColumnBuilder(ColumnTypeSortSchema, pageSizeHint, pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating sort_schema column: %w", err)
	}

	minTimestampBuilder, err := numberColumnBuilder(ColumnTypeMinTimestamp, pageSizeHint, pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating min_timestamp column: %w", err)
	}

	maxTimestampBuilder, err := numberColumnBuilder(ColumnTypeMaxTimestamp, pageSizeHint, pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating max_timestamp column: %w", err)
	}

	rowCountBuilder, err := numberColumnBuilder(ColumnTypeRowCount, pageSizeHint, pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating row_count column: %w", err)
	}

	uncompressedSizeBuilder, err := numberColumnBuilder(ColumnTypeUncompressedSize, pageSizeHint, pageMaxRowCount)
	if err != nil {
		return fmt.Errorf("creating uncompressed_size column: %w", err)
	}

	// Build dynamic label column builders (indices 7+), one per label key in sort-schema order.
	labelBuilders := make([]*dataset.ColumnBuilder, len(labelKeys))
	for i, key := range labelKeys {
		lb, err := labelColumnBuilder(key, pageSizeHint, pageMaxRowCount)
		if err != nil {
			return fmt.Errorf("creating label column %q: %w", key, err)
		}
		labelBuilders[i] = lb
	}

	// Populate column builders row by row.
	for i, r := range rows {
		_ = objectPathBuilder.Append(i, dataset.BinaryValue([]byte(r.ObjectPath)))
		_ = sectionIndexBuilder.Append(i, dataset.Int64Value(r.SectionIndex))
		_ = sortSchemaBuilder.Append(i, dataset.BinaryValue([]byte(r.SortSchema)))
		_ = minTimestampBuilder.Append(i, dataset.Int64Value(r.MinTimestamp))
		_ = maxTimestampBuilder.Append(i, dataset.Int64Value(r.MaxTimestamp))
		_ = rowCountBuilder.Append(i, dataset.Int64Value(r.RowCount))
		_ = uncompressedSizeBuilder.Append(i, dataset.Int64Value(r.UncompressedSize))

		// Dynamic label columns: append value, or omit (null) when absent.
		for j, key := range labelKeys {
			if val, ok := r.Labels[key]; ok {
				_ = labelBuilders[j].Append(i, dataset.BinaryValue([]byte(val)))
			}
		}
	}

	// Set sort info: sort_schema (index 2), then dynamic label columns (indices 7+),
	// then min_timestamp (index 3), then max_timestamp (index 4).
	// Fixed column indices: object_path=0, section_index=1, sort_schema=2,
	// min_timestamp=3, max_timestamp=4, row_count=5, uncompressed_size=6.
	// Label columns: 7, 8, ...
	columnSorts := make([]*datasetmd.SortInfo_ColumnSort, 0, 1+len(labelKeys)+2)
	columnSorts = append(columnSorts, &datasetmd.SortInfo_ColumnSort{
		ColumnIndex: 2, // sort_schema
		Direction:   datasetmd.SORT_DIRECTION_ASCENDING,
	})
	for i := range labelKeys {
		columnSorts = append(columnSorts, &datasetmd.SortInfo_ColumnSort{
			ColumnIndex: uint32(7 + i),
			Direction:   datasetmd.SORT_DIRECTION_ASCENDING,
		})
	}
	columnSorts = append(columnSorts, &datasetmd.SortInfo_ColumnSort{
		ColumnIndex: 3, // min_timestamp
		Direction:   datasetmd.SORT_DIRECTION_ASCENDING,
	})
	columnSorts = append(columnSorts, &datasetmd.SortInfo_ColumnSort{
		ColumnIndex: 4, // max_timestamp
		Direction:   datasetmd.SORT_DIRECTION_ASCENDING,
	})
	enc.SetSortInfo(&datasetmd.SortInfo{ColumnSorts: columnSorts})

	// Encode fixed columns.
	errs := make([]error, 0, 7+len(labelKeys))
	errs = append(errs, encodeColumn(enc, ColumnTypeObjectPath, objectPathBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypeSectionIndex, sectionIndexBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypeSortSchema, sortSchemaBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypeMinTimestamp, minTimestampBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypeMaxTimestamp, maxTimestampBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypeRowCount, rowCountBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypeUncompressedSize, uncompressedSizeBuilder))

	// Encode dynamic label columns.
	for i, lb := range labelBuilders {
		errs = append(errs, encodeLabelColumn(enc, labelKeys[i], lb))
	}

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("encoding columns: %w", err)
	}
	return nil
}

// binaryColumnBuilder creates a column builder for a fixed BINARY/PLAIN/ZSTD column.
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

// numberColumnBuilder creates a column builder for a fixed INT64/DELTA/NONE column.
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

// labelColumnBuilder creates a column builder for a dynamic label column (BINARY/PLAIN/ZSTD).
// The tag is the label name; the logical type is ColumnTypeLabel.
func labelColumnBuilder(labelName string, pageSize, pageRowCount int) (*dataset.ColumnBuilder, error) {
	return dataset.NewColumnBuilder(labelName, dataset.BuilderOptions{
		PageSizeHint:    pageSize,
		PageMaxRowCount: pageRowCount,
		Type: dataset.ColumnType{
			Physical: datasetmd.PHYSICAL_TYPE_BINARY,
			Logical:  ColumnTypeLabel.String(),
		},
		Encoding:    datasetmd.ENCODING_TYPE_PLAIN,
		Compression: datasetmd.COMPRESSION_TYPE_ZSTD,
	})
}

// encodeColumn flushes a builder and writes all its pages to enc.
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
		_ = columnEnc.Discard()
	}()
	if len(column.Pages) == 0 {
		return nil
	}

	for _, page := range column.Pages {
		if err := columnEnc.AppendPage(page); err != nil {
			return fmt.Errorf("appending %s page: %w", columnType, err)
		}
	}

	return columnEnc.Commit()
}

// encodeLabelColumn flushes a dynamic label column builder and writes all its pages to enc.
func encodeLabelColumn(enc *columnar.Encoder, labelName string, builder *dataset.ColumnBuilder) error {
	column, err := builder.Flush()
	if err != nil {
		return fmt.Errorf("flushing label %q column: %w", labelName, err)
	}

	columnEnc, err := enc.OpenColumn(column.ColumnDesc())
	if err != nil {
		return fmt.Errorf("opening label %q column encoder: %w", labelName, err)
	}
	defer func() {
		_ = columnEnc.Discard()
	}()
	if len(column.Pages) == 0 {
		return nil
	}

	for _, page := range column.Pages {
		if err := columnEnc.AppendPage(page); err != nil {
			return fmt.Errorf("appending label %q page: %w", labelName, err)
		}
	}

	return columnEnc.Commit()
}
