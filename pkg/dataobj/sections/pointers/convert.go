package pointers

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

type PopulateColumnFilter func(arrow.Field, ColumnType) bool

func PopulateSectionKey(_ arrow.Field, columnType ColumnType) bool {
	return columnType == ColumnTypePath || columnType == ColumnTypeSection
}

var sectionPointerColumns = map[ColumnType]struct{}{
	ColumnTypePath:             {},
	ColumnTypeSection:          {},
	ColumnTypeStreamID:         {},
	ColumnTypeStreamIDRef:      {},
	ColumnTypeMinTimestamp:     {},
	ColumnTypeMaxTimestamp:     {},
	ColumnTypeRowCount:         {},
	ColumnTypeUncompressedSize: {},
}

func PopulateSection(_ arrow.Field, columnType ColumnType) bool {
	_, ok := sectionPointerColumns[columnType]
	return ok
}

func FromRecordBatch(
	rec arrow.RecordBatch,
	dest []SectionPointer,
	populate func(arrow.Field, ColumnType) bool,
) (int, error) {
	schema := rec.Schema()
	numRows := int(rec.NumRows())
	if len(dest) < numRows {
		numRows = len(dest)
	}

	for fIdx := range schema.Fields() {
		field := schema.Field(fIdx)
		col := rec.Column(fIdx)
		ct := ColumnTypeFromField(field)

		if !populate(field, ct) {
			continue
		}

		switch ct {
		case ColumnTypePath:
			values := col.(*array.String)
			for rIdx := range numRows {
				if col.IsNull(rIdx) {
					continue
				}
				dest[rIdx].Path = values.Value(rIdx)
			}
		case ColumnTypeSection:
			values := col.(*array.Int64)
			for rIdx := range numRows {
				if col.IsNull(rIdx) {
					continue
				}
				dest[rIdx].Section = values.Value(rIdx)
			}
		case ColumnTypeStreamID:
			values := col.(*array.Int64)
			for rIdx := range numRows {
				if col.IsNull(rIdx) {
					continue
				}
				dest[rIdx].StreamID = values.Value(rIdx)
			}
		case ColumnTypeStreamIDRef:
			values := col.(*array.Int64)
			for rIdx := range numRows {
				if col.IsNull(rIdx) {
					continue
				}
				dest[rIdx].StreamIDRef = values.Value(rIdx)
			}
		case ColumnTypeMinTimestamp:
			values := col.(*array.Timestamp)
			for rIdx := range numRows {
				if col.IsNull(rIdx) {
					continue
				}
				dest[rIdx].StartTs = time.Unix(0, int64(values.Value(rIdx)))
			}
		case ColumnTypeMaxTimestamp:
			values := col.(*array.Timestamp)
			for rIdx := range numRows {
				if col.IsNull(rIdx) {
					continue
				}
				dest[rIdx].EndTs = time.Unix(0, int64(values.Value(rIdx)))
			}
		case ColumnTypeRowCount:
			values := col.(*array.Int64)
			for rIdx := range numRows {
				if col.IsNull(rIdx) {
					continue
				}
				dest[rIdx].LineCount = values.Value(rIdx)
			}
		case ColumnTypeUncompressedSize:
			values := col.(*array.Int64)
			for rIdx := range numRows {
				if col.IsNull(rIdx) {
					continue
				}
				dest[rIdx].UncompressedSize = values.Value(rIdx)
			}
		default:
			continue
		}
	}

	return numRows, nil
}
