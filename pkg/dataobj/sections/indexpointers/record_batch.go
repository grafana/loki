package indexpointers

import (
	"errors"
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
)

// A RecordBatch holds the rows that [Reader.ReadRecordBatch] read from an index
// pointers [Section], as one array for each projected column.
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
//
// The optional columns RunID, MinPostingKey, MaxPostingKey, ShardBuckets and
// RunLength can be missing from a section. The caller cannot project a column
// that the section lacks.
type RecordBatch struct {
	// Rows is the number of rows in the batch. It is at least one, and every
	// array has Rows elements.
	Rows int

	// Path holds the [ColumnTypePath] column.
	Path *columnar.UTF8

	// MinTimestamp holds the [ColumnTypeMinTimestamp] column, in nanoseconds
	// since the Unix epoch.
	MinTimestamp *columnar.Number[int64]

	// MaxTimestamp holds the [ColumnTypeMaxTimestamp] column, in nanoseconds
	// since the Unix epoch.
	MaxTimestamp *columnar.Number[int64]

	// RunID holds the [ColumnTypeRunID] column. A null element means that the
	// index object has no run. A value that is set is positive.
	RunID *columnar.Number[int64]

	// MinPostingKey holds the [ColumnTypeMinPostingKey] column. It holds binary
	// data in a UTF8 array, so the bytes need not be valid UTF-8.
	//
	// A null element in both posting key columns means that the bounds are
	// unknown, so the index object can hold any key. A null element in only one
	// of them is not valid. [PostingKeysFromBounds] reports an error for it.
	MinPostingKey *columnar.UTF8

	// MaxPostingKey holds the [ColumnTypeMaxPostingKey] column. It holds binary
	// data in a UTF8 array, so the bytes need not be valid UTF-8. See
	// MinPostingKey for a null element.
	MaxPostingKey *columnar.UTF8

	// ShardBuckets holds the [ColumnTypeShardBuckets] column. It holds the number
	// of buckets of the shard layout, not a bucket id. A null element means that
	// the shard layout of the index object is unknown.
	ShardBuckets *columnar.Number[int64]

	// RunLength holds the [ColumnTypeRunLength] column. A null element means
	// that the run length is unknown.
	RunLength *columnar.Number[int64]
}

// newRecordBatch builds a [RecordBatch] from the arrays of rb. It matches each
// array to its column by position.
//
// It returns an error when:
//   - rb has no row.
//   - The arrays do not match the columns in number, length or type.
//   - Two columns have the same type.
func newRecordBatch(columns []*Column, rb *columnar.RecordBatch) (*RecordBatch, error) {
	rows := int(rb.NumRows())
	if rows == 0 {
		return nil, errors.New("record batch has no row")
	}
	if int(rb.NumCols()) != len(columns) {
		return nil, fmt.Errorf("record batch has %d columns, want %d", rb.NumCols(), len(columns))
	}

	batch := &RecordBatch{Rows: rows}
	for i, column := range columns {
		arr := rb.Column(int64(i))
		if arr.Len() != rows {
			return nil, fmt.Errorf("array of %s has %d elements, want %d", column.Type, arr.Len(), rows)
		}

		var err error
		switch column.Type {
		case ColumnTypePath:
			err = fillUTF8(&batch.Path, arr, column.Type)
		case ColumnTypeMinTimestamp:
			err = fillNumber(&batch.MinTimestamp, arr, column.Type)
		case ColumnTypeMaxTimestamp:
			err = fillNumber(&batch.MaxTimestamp, arr, column.Type)
		case ColumnTypeRunID:
			err = fillNumber(&batch.RunID, arr, column.Type)
		case ColumnTypeMinPostingKey:
			err = fillUTF8(&batch.MinPostingKey, arr, column.Type)
		case ColumnTypeMaxPostingKey:
			err = fillUTF8(&batch.MaxPostingKey, arr, column.Type)
		case ColumnTypeShardBuckets:
			err = fillNumber(&batch.ShardBuckets, arr, column.Type)
		case ColumnTypeRunLength:
			err = fillNumber(&batch.RunLength, arr, column.Type)
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
