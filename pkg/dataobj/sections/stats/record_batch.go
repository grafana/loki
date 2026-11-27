package stats

import (
	"errors"
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
)

// A RecordBatch holds the rows that [Reader.ReadRecordBatch] read from a stats
// section, as one array for each projected column.
//
// The arrays use memory from the allocator that the caller passed to
// ReadRecordBatch. Reclaiming that allocator invalidates the arrays and the
// slices that Get returns, even while the caller holds the RecordBatch. Copy a
// value before you reclaim the allocator.
//
// Treat a RecordBatch as read-only.
//
// A field is nil when the reader did not project its column. A null element
// means that the row has no value for the column.
//
// Check IsNull before you call Get. Get returns no valid value for a null
// element. Get can return a nil slice for a non-null empty element. Do not use a
// nil slice as a null test.
type RecordBatch struct {
	// Rows is the number of rows in the batch. It is at least one, and every
	// array has Rows elements.
	Rows int

	// ObjectPath holds the [ColumnTypeObjectPath] column.
	ObjectPath *columnar.UTF8

	// SectionIndex holds the [ColumnTypeSectionIndex] column.
	SectionIndex *columnar.Number[int64]

	// SortSchema holds the [ColumnTypeSortSchema] column.
	SortSchema *columnar.UTF8

	// MinTimestamp holds the [ColumnTypeMinTimestamp] column, in nanoseconds
	// since the Unix epoch.
	MinTimestamp *columnar.Number[int64]

	// MaxTimestamp holds the [ColumnTypeMaxTimestamp] column, in nanoseconds
	// since the Unix epoch.
	MaxTimestamp *columnar.Number[int64]

	// RowCount holds the [ColumnTypeRowCount] column. Each value is the number of
	// log records of the row.
	RowCount *columnar.Number[int64]

	// UncompressedSize holds the [ColumnTypeUncompressedSize] column, in bytes.
	UncompressedSize *columnar.Number[int64]

	// ShardBucket holds the [ColumnTypeShardBucket] column. A bucket is an
	// unsigned 32 bit value in an int64 array.
	ShardBucket *columnar.Number[int64]

	// Labels holds one entry for each [ColumnTypeLabel] column that the reader
	// projected, in the order of ReaderOptions.Columns. A null element means that
	// the row has no value for the label. An empty value is not null.
	//
	// Two entries can have the same name, and a name can be empty.
	//
	// The Labels slice and the Name of each entry stay valid after the caller
	// reclaims the allocator. The Values do not.
	Labels []LabelColumn
}

// A LabelColumn holds the values that one label has in a [RecordBatch].
type LabelColumn struct {
	// Name is the label name that the column holds.
	Name string

	// Values holds the label values. It is never nil.
	Values *columnar.UTF8
}

// newRecordBatch builds a [RecordBatch] from the arrays of rb. It matches each
// array to its column by position.
//
// It returns an error when:
//   - rb has no row.
//   - The arrays do not match the columns in number, length or type.
//   - Two columns other than label columns have the same type.
//
// Label columns can repeat.
func newRecordBatch(columns []*Column, rb *columnar.RecordBatch) (*RecordBatch, error) {
	rows := int(rb.NumRows())
	if rows == 0 {
		return nil, errors.New("record batch has no row")
	}
	if int(rb.NumCols()) != len(columns) {
		return nil, fmt.Errorf("record batch has %d columns, want %d", rb.NumCols(), len(columns))
	}

	batch := &RecordBatch{Rows: rows}
	var labels int
	for _, column := range columns {
		if column.Type == ColumnTypeLabel {
			labels++
		}
	}
	if labels > 0 {
		batch.Labels = make([]LabelColumn, 0, labels)
	}

	for i, column := range columns {
		arr := rb.Column(int64(i))
		if arr.Len() != rows {
			return nil, fmt.Errorf("array of %s has %d elements, want %d", column.Type, arr.Len(), rows)
		}

		var err error
		switch column.Type {
		case ColumnTypeObjectPath:
			err = fillUTF8(&batch.ObjectPath, arr, column.Type)
		case ColumnTypeSectionIndex:
			err = fillNumber(&batch.SectionIndex, arr, column.Type)
		case ColumnTypeSortSchema:
			err = fillUTF8(&batch.SortSchema, arr, column.Type)
		case ColumnTypeMinTimestamp:
			err = fillNumber(&batch.MinTimestamp, arr, column.Type)
		case ColumnTypeMaxTimestamp:
			err = fillNumber(&batch.MaxTimestamp, arr, column.Type)
		case ColumnTypeRowCount:
			err = fillNumber(&batch.RowCount, arr, column.Type)
		case ColumnTypeUncompressedSize:
			err = fillNumber(&batch.UncompressedSize, arr, column.Type)
		case ColumnTypeShardBucket:
			err = fillNumber(&batch.ShardBucket, arr, column.Type)
		case ColumnTypeLabel:
			values, ok := arr.(*columnar.UTF8)
			if !ok {
				return nil, fmt.Errorf("invalid array type %T for %s", arr, column.Type)
			}
			batch.Labels = append(batch.Labels, LabelColumn{Name: column.Name, Values: values})
		default:
			err = fmt.Errorf("unsupported column type %s", column.Type)
		}
		if err != nil {
			return nil, err
		}
	}
	return batch, nil
}

// fillNumber fills dst with arr. It returns an error when arr is not an int64
// array. It also returns an error when dst holds an array already, because two
// columns have the same type.
func fillNumber(dst **columnar.Number[int64], arr columnar.Array, typ ColumnType) error {
	if *dst != nil {
		return fmt.Errorf("more than one %s column", typ)
	}
	values, ok := arr.(*columnar.Number[int64])
	if !ok {
		return fmt.Errorf("invalid array type %T for %s", arr, typ)
	}
	*dst = values
	return nil
}

// fillUTF8 fills dst with arr. It returns an error when arr is not a UTF8 array.
// It also returns an error when dst holds an array already, because two columns
// have the same type.
func fillUTF8(dst **columnar.UTF8, arr columnar.Array, typ ColumnType) error {
	if *dst != nil {
		return fmt.Errorf("more than one %s column", typ)
	}
	values, ok := arr.(*columnar.UTF8)
	if !ok {
		return fmt.Errorf("invalid array type %T for %s", arr, typ)
	}
	*dst = values
	return nil
}
