package stats

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// ColumnIndex maps Arrow field names to column indices within an
// [arrow.RecordBatch]. Build one with [BuildColumnIndex] and reuse it across
// rows decoded from the same schema.
type ColumnIndex map[string]int

// BuildColumnIndex constructs a [ColumnIndex] from an Arrow schema. The
// returned index can be passed to [DecodeRow] for efficient per-row decoding.
func BuildColumnIndex(schema *arrow.Schema) ColumnIndex {
	idx := make(ColumnIndex, len(schema.Fields()))
	for i, field := range schema.Fields() {
		idx[field.Name] = i
	}
	return idx
}

// DecodeRow decodes a single row at rowIndex from an Arrow [arrow.RecordBatch]
// into a [Stat]. Dynamic label columns (fields with a ".label.utf8" suffix)
// are decoded into the Labels map.
//
// Columns are looked up by field name via the provided [ColumnIndex]; missing
// or null values are left at their zero value.
func DecodeRow(batch arrow.RecordBatch, columns ColumnIndex, rowIndex int) Stat {
	result := Stat{
		Labels: make(map[string]string),
	}

	getColumn := func(name string) arrow.Array {
		if idx, ok := columns[name]; ok {
			return batch.Column(idx)
		}
		return nil
	}

	if col := getColumn("object_path.utf8"); col != nil && !col.IsNull(rowIndex) {
		result.ObjectPath = col.(*array.String).Value(rowIndex)
	}

	if col := getColumn("section_index.int64"); col != nil && !col.IsNull(rowIndex) {
		result.SectionIndex = col.(*array.Int64).Value(rowIndex)
	}

	if col := getColumn("sort_schema.utf8"); col != nil && !col.IsNull(rowIndex) {
		result.SortSchema = col.(*array.String).Value(rowIndex)
	}

	if col := getColumn("min_timestamp.timestamp"); col != nil && !col.IsNull(rowIndex) {
		result.MinTimestamp = int64(col.(*array.Timestamp).Value(rowIndex))
	}

	if col := getColumn("max_timestamp.timestamp"); col != nil && !col.IsNull(rowIndex) {
		result.MaxTimestamp = int64(col.(*array.Timestamp).Value(rowIndex))
	}

	if col := getColumn("row_count.int64"); col != nil && !col.IsNull(rowIndex) {
		result.RowCount = col.(*array.Int64).Value(rowIndex)
	}

	if col := getColumn("uncompressed_size.int64"); col != nil && !col.IsNull(rowIndex) {
		result.UncompressedSize = col.(*array.Int64).Value(rowIndex)
	}

	// Decode all label columns (format: "<label>.label.utf8").
	// Use suffix trimming rather than Split to handle label names that contain dots.
	for fieldName, colIdx := range columns {
		if !strings.HasSuffix(fieldName, ".label.utf8") {
			continue
		}
		labelName := strings.TrimSuffix(fieldName, ".label.utf8")
		col := batch.Column(colIdx)
		if !col.IsNull(rowIndex) {
			result.Labels[labelName] = col.(*array.String).Value(rowIndex)
		}
	}

	return result
}
