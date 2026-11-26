package postings

import (
	"bytes"
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
)

// rowBatch decodes the rows of one record batch that [Reader.readRecordBatch]
// returned. It holds one field for each column of [Row]. A field is zero when
// the reader did not project its column.
//
// The arrays point into the memory of the Reader, so a rowBatch is valid only
// until the next read: the next call to Read, readRecordBatch, or Reset of the
// Reader. The rows that it decodes do not use that memory.
type rowBatch struct {
	rows int

	kind             *columnar.Number[int64]
	objectPath       stringColumn
	shardBuckets     *columnar.Number[int64]
	sectionIndex     *columnar.Number[int64]
	columnName       stringColumn
	labelValue       stringColumn
	streamIDBitmap   bytesColumn
	bloomFilter      *columnar.UTF8
	uncompressedSize *columnar.Number[int64]
	minTimestamp     *columnar.Number[int64]
	maxTimestamp     *columnar.Number[int64]
	minShardBucket   *columnar.Number[int64]
	maxShardBucket   *columnar.Number[int64]
}

// newRowBatch matches the first len(columns) arrays of rb to columns by
// position. It returns an error if rb has fewer arrays than columns, if an
// array has a length other than rb.NumRows, if an array has the wrong type for
// its column, or if a binary array has invalid offsets. It ignores a column of
// a type that it does not know, because [Row] has no field for it.
func newRowBatch(columns []*Column, rb *columnar.RecordBatch) (rowBatch, error) {
	if int(rb.NumCols()) < len(columns) {
		return rowBatch{}, fmt.Errorf("record batch has %d columns, want at least %d", rb.NumCols(), len(columns))
	}

	batch := rowBatch{rows: int(rb.NumRows())}
	for i, col := range columns {
		arr := rb.Column(int64(i))
		if arr.Len() != batch.rows {
			return rowBatch{}, fmt.Errorf("array of %s has %d elements, want %d", col.Type, arr.Len(), batch.rows)
		}

		var err error
		switch col.Type {
		case ColumnTypeKind:
			batch.kind, err = int64Array(arr, col.Type)
		case ColumnTypeObjectPath:
			batch.objectPath, err = newStringColumn(arr, col.Type)
		case ColumnTypeShardBuckets:
			batch.shardBuckets, err = int64Array(arr, col.Type)
		case ColumnTypeSectionIndex:
			batch.sectionIndex, err = int64Array(arr, col.Type)
		case ColumnTypeColumnName:
			batch.columnName, err = newStringColumn(arr, col.Type)
		case ColumnTypeLabelValue:
			batch.labelValue, err = newStringColumn(arr, col.Type)
		case ColumnTypeStreamIDBitmap:
			batch.streamIDBitmap, err = newBytesColumn(arr, col.Type)
		case ColumnTypeBloomFilter:
			batch.bloomFilter, err = binaryArray(arr, col.Type)
		case ColumnTypeUncompressedSize:
			batch.uncompressedSize, err = int64Array(arr, col.Type)
		case ColumnTypeMinTimestamp:
			batch.minTimestamp, err = int64Array(arr, col.Type)
		case ColumnTypeMaxTimestamp:
			batch.maxTimestamp, err = int64Array(arr, col.Type)
		case ColumnTypeMinShardBucket:
			batch.minShardBucket, err = int64Array(arr, col.Type)
		case ColumnTypeMaxShardBucket:
			batch.maxShardBucket, err = int64Array(arr, col.Type)
		}
		if err != nil {
			return rowBatch{}, err
		}
	}
	return batch, nil
}

func int64Array(arr columnar.Array, typ ColumnType) (*columnar.Number[int64], error) {
	numbers, ok := arr.(*columnar.Number[int64])
	if !ok {
		return nil, fmt.Errorf("invalid array type %T for %s", arr, typ)
	}
	return numbers, nil
}

// binaryArray returns arr as a binary array. It returns an error if arr is not
// binary, or if its offsets have the wrong count, decrease, or fall outside its
// data. A check of every offset lets the callers slice the values without a
// panic on corrupt data.
func binaryArray(arr columnar.Array, typ ColumnType) (*columnar.UTF8, error) {
	values, ok := arr.(*columnar.UTF8)
	if !ok {
		return nil, fmt.Errorf("invalid array type %T for %s", arr, typ)
	}

	offsets := values.Offsets()
	if len(offsets) != values.Len()+1 {
		return nil, fmt.Errorf("array of %s has %d offsets, want %d", typ, len(offsets), values.Len()+1)
	}
	first, last := offsets[0], offsets[len(offsets)-1]
	if first < 0 || int(last) > len(values.Data()) {
		return nil, fmt.Errorf("array of %s has offsets [%d, %d] outside its %d data bytes", typ, first, last, len(values.Data()))
	}
	for i := 1; i < len(offsets); i++ {
		if offsets[i] < offsets[i-1] {
			return nil, fmt.Errorf("array of %s has offset %d at index %d, below offset %d before it", typ, offsets[i], i, offsets[i-1])
		}
	}
	return values, nil
}

// row decodes row i of the batch. The index i must be in the range [0, rows).
//
// A null value or a column that the reader did not project decodes as the
// zero value. The Row does not use the memory of the Reader, so it stays valid
// after the next read.
func (b *rowBatch) row(i int) Row {
	return Row{
		Kind:             PostingKind(int64At(b.kind, i)),
		ObjectPath:       b.objectPath.at(i),
		ShardBuckets:     int64At(b.shardBuckets, i),
		SectionIndex:     int64At(b.sectionIndex, i),
		ColumnName:       b.columnName.at(i),
		LabelValue:       b.labelValue.at(i),
		StreamIDBitmap:   b.streamIDBitmap.at(i),
		BloomFilter:      cloneBytesAt(b.bloomFilter, i),
		UncompressedSize: int64At(b.uncompressedSize, i),
		MinTimestamp:     int64At(b.minTimestamp, i),
		MaxTimestamp:     int64At(b.maxTimestamp, i),
		MinShardBucket:   uint32(int64At(b.minShardBucket, i)),
		MaxShardBucket:   uint32(int64At(b.maxShardBucket, i)),
	}
}

func int64At(arr *columnar.Number[int64], i int) int64 {
	if arr == nil || arr.IsNull(i) {
		return 0
	}
	return arr.Get(i)
}

// cloneBytesAt returns a copy of value i of arr, or nil if arr is nil or the
// value is null.
//
// It copies each value on its own because bloom filters can be tens of
// kilobytes each. A shared copy, as in [bytesColumn], keeps every bloom filter
// of the batch alive while a caller retains one.
func cloneBytesAt(arr *columnar.UTF8, i int) []byte {
	if arr == nil || arr.IsNull(i) {
		return nil
	}
	return bytes.Clone(arr.Get(i))
}

// stringColumn decodes the values of a binary array as strings. It copies the
// values of the batch into one string, so decoding a row takes a substring
// and does not allocate. A retained value keeps the string of its whole batch
// alive.
//
// The zero value decodes every row as the empty string.
type stringColumn struct {
	values valueOffsets
	data   string
}

func newStringColumn(arr columnar.Array, typ ColumnType) (stringColumn, error) {
	values, data, err := newValueOffsets(arr, typ)
	if err != nil {
		return stringColumn{}, err
	}
	return stringColumn{values: values, data: string(data)}, nil
}

// at returns value i, or the empty string if the value is null.
func (c stringColumn) at(i int) string {
	start, end, ok := c.values.bounds(i)
	if !ok {
		return ""
	}
	return c.data[start:end]
}

// bytesColumn decodes the values of a binary array as byte slices. It copies
// the values of the batch into one slice, so decoding a row takes a subslice
// and does not allocate. A retained value keeps the slice of its whole batch
// alive.
//
// The zero value decodes every row as nil.
type bytesColumn struct {
	values valueOffsets
	data   []byte
}

func newBytesColumn(arr columnar.Array, typ ColumnType) (bytesColumn, error) {
	values, data, err := newValueOffsets(arr, typ)
	if err != nil {
		return bytesColumn{}, err
	}
	return bytesColumn{values: values, data: bytes.Clone(data)}, nil
}

// at returns value i, or nil if the value is null. The capacity of the slice
// ends at its length, so an append by the caller cannot overwrite the next
// value.
func (c bytesColumn) at(i int) []byte {
	start, end, ok := c.values.bounds(i)
	if !ok {
		return nil
	}
	return c.data[start:end:end]
}

// valueOffsets locates the values of a binary array in a copy of its data.
//
// It reads the offsets and the validity of arr, which use the memory of the
// Reader. So it is valid only until the next read. The copy of the data that
// the caller holds stays valid after it.
type valueOffsets struct {
	arr  *columnar.UTF8
	base int32 // Offset of the copy in the data of arr.
}

// newValueOffsets returns the offsets of arr and the bytes that its values
// cover. The bytes use the memory of the Reader, so the caller must copy them
// before the next read. It returns the errors of [binaryArray].
func newValueOffsets(arr columnar.Array, typ ColumnType) (valueOffsets, []byte, error) {
	values, err := binaryArray(arr, typ)
	if err != nil {
		return valueOffsets{}, nil, err
	}
	offsets := values.Offsets()
	first, last := offsets[0], offsets[len(offsets)-1]
	return valueOffsets{arr: values, base: first}, values.Data()[first:last], nil
}

// bounds returns the range of value i in the copy of the data. It reports
// false if the value is null or v is the zero value.
func (v valueOffsets) bounds(i int) (start, end int32, ok bool) {
	if v.arr == nil || v.arr.IsNull(i) {
		return 0, 0, false
	}
	offsets := v.arr.Offsets()
	return offsets[i] - v.base, offsets[i+1] - v.base, true
}
