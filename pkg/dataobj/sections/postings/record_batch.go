package postings

import (
	"errors"
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
)

// A RecordBatch holds the rows that [Reader.ReadRecordBatch] read from a
// postings section, as one array for each projected column.
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

	// Kind holds the [ColumnTypeKind] column. It holds raw [PostingKind] values,
	// and ReadRecordBatch does not validate them.
	Kind *columnar.Number[int64]

	// ObjectPath holds the [ColumnTypeObjectPath] column.
	ObjectPath *columnar.UTF8

	// ShardBuckets holds the [ColumnTypeShardBuckets] column. It is the number of
	// buckets, not a bucket id.
	ShardBuckets *columnar.Number[int64]

	// SectionIndex holds the [ColumnTypeSectionIndex] column.
	SectionIndex *columnar.Number[int64]

	// ColumnName holds the [ColumnTypeColumnName] column.
	ColumnName *columnar.UTF8

	// LabelValue holds the [ColumnTypeLabelValue] column. It is null on bloom
	// rows.
	LabelValue *columnar.UTF8

	// BloomFilter holds the [ColumnTypeBloomFilter] column. It is null on label
	// rows. It holds binary data in UTF8 arrays, so the bytes need not be valid
	// UTF-8.
	BloomFilter *columnar.UTF8

	// StreamIDBitmap holds the [ColumnTypeStreamIDBitmap] column. It holds binary
	// data in UTF8 arrays, so the bytes need not be valid UTF-8.
	StreamIDBitmap *columnar.UTF8

	// UncompressedSize holds the [ColumnTypeUncompressedSize] column, in bytes.
	UncompressedSize *columnar.Number[int64]

	// MinTimestamp holds the [ColumnTypeMinTimestamp] column, in nanoseconds
	// since the Unix epoch.
	MinTimestamp *columnar.Number[int64]

	// MaxTimestamp holds the [ColumnTypeMaxTimestamp] column, in nanoseconds
	// since the Unix epoch.
	MaxTimestamp *columnar.Number[int64]

	// MinShardBucket holds the [ColumnTypeMinShardBucket] column. It is null on
	// bloom rows. A bucket is an unsigned 32 bit value in an int64 array.
	MinShardBucket *columnar.Number[int64]

	// MaxShardBucket holds the [ColumnTypeMaxShardBucket] column. It is null on
	// bloom rows. A bucket is an unsigned 32 bit value in an int64 array.
	MaxShardBucket *columnar.Number[int64]
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
		case ColumnTypeKind:
			err = fillNumber(&batch.Kind, arr, column.Type)
		case ColumnTypeObjectPath:
			err = fillUTF8(&batch.ObjectPath, arr, column.Type)
		case ColumnTypeShardBuckets:
			err = fillNumber(&batch.ShardBuckets, arr, column.Type)
		case ColumnTypeSectionIndex:
			err = fillNumber(&batch.SectionIndex, arr, column.Type)
		case ColumnTypeColumnName:
			err = fillUTF8(&batch.ColumnName, arr, column.Type)
		case ColumnTypeLabelValue:
			err = fillUTF8(&batch.LabelValue, arr, column.Type)
		case ColumnTypeBloomFilter:
			err = fillUTF8(&batch.BloomFilter, arr, column.Type)
		case ColumnTypeStreamIDBitmap:
			err = fillUTF8(&batch.StreamIDBitmap, arr, column.Type)
		case ColumnTypeUncompressedSize:
			err = fillNumber(&batch.UncompressedSize, arr, column.Type)
		case ColumnTypeMinTimestamp:
			err = fillNumber(&batch.MinTimestamp, arr, column.Type)
		case ColumnTypeMaxTimestamp:
			err = fillNumber(&batch.MaxTimestamp, arr, column.Type)
		case ColumnTypeMinShardBucket:
			err = fillNumber(&batch.MinShardBucket, arr, column.Type)
		case ColumnTypeMaxShardBucket:
			err = fillNumber(&batch.MaxShardBucket, arr, column.Type)
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
