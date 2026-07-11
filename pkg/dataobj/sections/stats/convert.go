package stats

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func FromRecordBatch(rec arrow.RecordBatch, dest []Stat) (int, error) {
	schema := rec.Schema()
	numRows := min(len(dest), int(rec.NumRows()))

	for i := range numRows {
		dest[i] = Stat{Labels: make(map[string]string)}
	}

	for fIdx := range schema.Fields() {
		field := schema.Field(fIdx)
		col := rec.Column(fIdx)

		switch field.Name {
		case "section_index.int64":
			int64Col := col.(*array.Int64)
			for rIdx := range numRows {
				if !col.IsNull(rIdx) {
					dest[rIdx].SectionIndex = int64Col.Value(rIdx)
				}
			}
		case "sort_schema.utf8":
			strCol := col.(*array.String)
			for rIdx := range numRows {
				if !col.IsNull(rIdx) {
					dest[rIdx].SortSchema = strCol.Value(rIdx)
				}
			}
		case "min_timestamp.timestamp":
			tsCol := col.(*array.Timestamp)
			for rIdx := range numRows {
				if !col.IsNull(rIdx) {
					dest[rIdx].MinTimestamp = int64(tsCol.Value(rIdx))
				}
			}
		case "max_timestamp.timestamp":
			tsCol := col.(*array.Timestamp)
			for rIdx := range numRows {
				if !col.IsNull(rIdx) {
					dest[rIdx].MaxTimestamp = int64(tsCol.Value(rIdx))
				}
			}
		case "row_count.int64":
			int64Col := col.(*array.Int64)
			for rIdx := range numRows {
				if !col.IsNull(rIdx) {
					dest[rIdx].RowCount = int64Col.Value(rIdx)
				}
			}
		case "uncompressed_size.int64":
			int64Col := col.(*array.Int64)
			for rIdx := range numRows {
				if !col.IsNull(rIdx) {
					dest[rIdx].UncompressedSize = int64Col.Value(rIdx)
				}
			}
		case "object_path.utf8":
			strCol := col.(*array.String)
			for rIdx := range numRows {
				if !col.IsNull(rIdx) {
					dest[rIdx].ObjectPath = strCol.Value(rIdx)
				}
			}
		default:
			if before, ok := strings.CutSuffix(field.Name, ".label.utf8"); ok {
				labelName := before
				strCol := col.(*array.String)
				for rIdx := range numRows {
					if !col.IsNull(rIdx) {
						dest[rIdx].Labels[labelName] = strCol.Value(rIdx)
					}
				}
			}
		}
	}

	return numRows, nil
}
