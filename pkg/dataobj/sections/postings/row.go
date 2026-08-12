package postings

import (
	"bytes"
	"cmp"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// CompareRows defines the physical row order of a postings section and the
// merge order across sections. It reports whether row [a] sorts before (<0),
// after (>0), or equal to (0) row [b].
func CompareRows(a, b Row) int {
	return cmp.Or(
		cmp.Compare(a.Kind, b.Kind),
		cmp.Compare(a.ColumnName, b.ColumnName),
		cmp.Compare(a.LabelValue, b.LabelValue),
		cmp.Compare(a.MinTimestamp, b.MinTimestamp),
		cmp.Compare(a.MaxTimestamp, b.MaxTimestamp),
		cmp.Compare(a.ObjectPath, b.ObjectPath),
		cmp.Compare(a.SectionIndex, b.SectionIndex),
	)
}

// Row is the decoded per-row representation of a postings section,
// covering both Label and Bloom kinds.
type Row struct {
	Kind             PostingKind // KindLabel or KindBloom
	ObjectPath       string
	SectionIndex     int64
	ColumnName       string
	LabelValue       string // empty for KindBloom rows
	StreamIDBitmap   []byte
	BloomFilter      []byte // nil for KindLabel rows
	UncompressedSize int64
	MinTimestamp     int64 // unix nanos
	MaxTimestamp     int64 // unix nanos
}

// LabelEntry converts the Row to a [LabelEntry]. The caller should only call
// this when Row.Kind == KindLabel.
func (r Row) LabelEntry() LabelEntry {
	return LabelEntry{
		ObjectPath:       r.ObjectPath,
		SectionIndex:     r.SectionIndex,
		ColumnName:       r.ColumnName,
		LabelValue:       r.LabelValue,
		StreamIDBitmap:   r.StreamIDBitmap,
		MinTimestamp:     r.MinTimestamp,
		MaxTimestamp:     r.MaxTimestamp,
		UncompressedSize: r.UncompressedSize,
	}
}

// BloomEntry converts the Row to a [BloomEntry]. The caller should only call
// this when Row.Kind == KindBloom.
func (r Row) BloomEntry() BloomEntry {
	return BloomEntry{
		ObjectPath:       r.ObjectPath,
		SectionIndex:     r.SectionIndex,
		ColumnName:       r.ColumnName,
		BloomFilter:      r.BloomFilter,
		StreamIDBitmap:   r.StreamIDBitmap,
		MinTimestamp:     r.MinTimestamp,
		MaxTimestamp:     r.MaxTimestamp,
		UncompressedSize: r.UncompressedSize,
	}
}

// Row converts the LabelEntry to a [Row] with Kind set to [KindLabel]. It is
// the inverse of [Row.LabelEntry].
func (e LabelEntry) Row() Row {
	return Row{
		Kind:             KindLabel,
		ObjectPath:       e.ObjectPath,
		SectionIndex:     e.SectionIndex,
		ColumnName:       e.ColumnName,
		LabelValue:       e.LabelValue,
		StreamIDBitmap:   e.StreamIDBitmap,
		MinTimestamp:     e.MinTimestamp,
		MaxTimestamp:     e.MaxTimestamp,
		UncompressedSize: e.UncompressedSize,
	}
}

// Row converts the BloomEntry to a [Row] with Kind set to [KindBloom]. It is
// the inverse of [Row.BloomEntry].
func (e BloomEntry) Row() Row {
	return Row{
		Kind:             KindBloom,
		ObjectPath:       e.ObjectPath,
		SectionIndex:     e.SectionIndex,
		ColumnName:       e.ColumnName,
		BloomFilter:      e.BloomFilter,
		StreamIDBitmap:   e.StreamIDBitmap,
		MinTimestamp:     e.MinTimestamp,
		MaxTimestamp:     e.MaxTimestamp,
		UncompressedSize: e.UncompressedSize,
	}
}

// ColumnIndex maps Arrow field names to column indices within a
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
// into a [Row]. Binary column values (StreamIDBitmap, BloomFilter) are copied
// so the returned Row does not retain references to the batch's memory.
//
// Columns are looked up by field name via the provided [ColumnIndex]; missing
// or null values are left at their zero value.
func DecodeRow(batch arrow.RecordBatch, columns ColumnIndex, rowIndex int) Row {
	var result Row

	getColumn := func(name string) arrow.Array {
		if idx, ok := columns[name]; ok {
			return batch.Column(idx)
		}
		return nil
	}

	if col := getColumn("kind.int64"); col != nil && !col.IsNull(rowIndex) {
		result.Kind = PostingKind(col.(*array.Int64).Value(rowIndex))
	}

	if col := getColumn("object_path.utf8"); col != nil && !col.IsNull(rowIndex) {
		result.ObjectPath = col.(*array.String).Value(rowIndex)
	}

	if col := getColumn("section_index.int64"); col != nil && !col.IsNull(rowIndex) {
		result.SectionIndex = col.(*array.Int64).Value(rowIndex)
	}

	if col := getColumn("column_name.utf8"); col != nil && !col.IsNull(rowIndex) {
		result.ColumnName = col.(*array.String).Value(rowIndex)
	}

	if col := getColumn("label_value.utf8"); col != nil && !col.IsNull(rowIndex) {
		result.LabelValue = col.(*array.String).Value(rowIndex)
	}

	if col := getColumn("stream_id_bitmap.binary"); col != nil && !col.IsNull(rowIndex) {
		result.StreamIDBitmap = bytes.Clone(col.(*array.Binary).Value(rowIndex))
	}

	if col := getColumn("bloom_filter.binary"); col != nil && !col.IsNull(rowIndex) {
		result.BloomFilter = bytes.Clone(col.(*array.Binary).Value(rowIndex))
	}

	if col := getColumn("uncompressed_size.int64"); col != nil && !col.IsNull(rowIndex) {
		result.UncompressedSize = col.(*array.Int64).Value(rowIndex)
	}

	if col := getColumn("min_timestamp.timestamp"); col != nil && !col.IsNull(rowIndex) {
		result.MinTimestamp = int64(col.(*array.Timestamp).Value(rowIndex))
	}

	if col := getColumn("max_timestamp.timestamp"); col != nil && !col.IsNull(rowIndex) {
		result.MaxTimestamp = int64(col.(*array.Timestamp).Value(rowIndex))
	}

	return result
}
