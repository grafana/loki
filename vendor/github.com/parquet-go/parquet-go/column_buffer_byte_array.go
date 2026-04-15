package parquet

import (
	"bytes"
	"io"
	"strconv"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding/plain"
	"github.com/parquet-go/parquet-go/internal/memory"
	"github.com/parquet-go/parquet-go/sparse"
)

type byteArrayColumnBuffer struct {
	byteArrayPage
	lengths memory.SliceBuffer[uint32]
	scratch memory.SliceBuffer[byte]
}

func newByteArrayColumnBuffer(typ Type, columnIndex int16, numValues int32) *byteArrayColumnBuffer {
	return &byteArrayColumnBuffer{
		byteArrayPage: byteArrayPage{
			typ:         typ,
			offsets:     memory.SliceBufferFor[uint32](int(numValues)),
			columnIndex: ^columnIndex,
		},
		lengths: memory.SliceBufferFor[uint32](int(numValues)),
	}
}

func (col *byteArrayColumnBuffer) Clone() ColumnBuffer {
	return &byteArrayColumnBuffer{
		byteArrayPage: byteArrayPage{
			typ:         col.typ,
			values:      col.cloneValues(),
			offsets:     col.cloneOffsets(),
			columnIndex: col.columnIndex,
		},
		lengths: col.cloneLengths(),
	}
}

func (col *byteArrayColumnBuffer) cloneLengths() memory.SliceBuffer[uint32] {
	return col.lengths.Clone()
}

func (col *byteArrayColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return byteArrayColumnIndex{col.page()}, nil
}

func (col *byteArrayColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return byteArrayOffsetIndex{col.page()}, nil
}

func (col *byteArrayColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *byteArrayColumnBuffer) Dictionary() Dictionary { return nil }

func (col *byteArrayColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *byteArrayColumnBuffer) page() *byteArrayPage {
	lengths := col.lengths.Slice()
	offsets := col.offsets.Slice()

	if len(lengths) > 0 && orderOfUint32(offsets) < 1 { // unordered?
		if col.scratch.Cap() < col.values.Len() {
			col.scratch.Grow(col.values.Len())
		}
		col.scratch.Resize(0)

		for i := range lengths {
			n := col.scratch.Len()
			col.scratch.Append(col.index(i)...)
			offsets[i] = uint32(n)
		}

		col.values, col.scratch = col.scratch, col.values
	}
	col.offsets.Resize(len(lengths))
	col.offsets.AppendValue(uint32(col.values.Len()))
	return &col.byteArrayPage
}

func (col *byteArrayColumnBuffer) Page() Page {
	return col.page()
}

func (col *byteArrayColumnBuffer) Reset() {
	col.values.Reset()
	col.offsets.Reset()
	col.lengths.Reset()
}

func (col *byteArrayColumnBuffer) NumRows() int64 { return int64(col.Len()) }

func (col *byteArrayColumnBuffer) NumValues() int64 { return int64(col.Len()) }

func (col *byteArrayColumnBuffer) Cap() int { return col.lengths.Cap() }

func (col *byteArrayColumnBuffer) Len() int { return col.lengths.Len() }

func (col *byteArrayColumnBuffer) Less(i, j int) bool {
	return bytes.Compare(col.index(i), col.index(j)) < 0
}

func (col *byteArrayColumnBuffer) Swap(i, j int) {
	col.offsets.Swap(i, j)
	col.lengths.Swap(i, j)
}

func (col *byteArrayColumnBuffer) Write(b []byte) (int, error) {
	_, n, err := col.writeByteArrays(b)
	return n, err
}

func (col *byteArrayColumnBuffer) WriteByteArrays(values []byte) (int, error) {
	n, _, err := col.writeByteArrays(values)
	return n, err
}

func (col *byteArrayColumnBuffer) writeByteArrays(values []byte) (count, bytes int, err error) {
	baseCount := col.lengths.Len()
	baseBytes := col.values.Len() + (plain.ByteArrayLengthSize * col.lengths.Len())

	err = plain.RangeByteArray(values, func(value []byte) error {
		col.offsets.AppendValue(uint32(col.values.Len()))
		col.lengths.AppendValue(uint32(len(value)))
		col.values.Append(value...)
		return nil
	})

	count = col.lengths.Len() - baseCount
	bytes = (col.values.Len() - baseBytes) + (plain.ByteArrayLengthSize * count)
	return count, bytes, err
}

func (col *byteArrayColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(columnLevels{}, makeArrayValue(values, offsetOfPtr))
	return len(values), nil
}

func (col *byteArrayColumnBuffer) writeValues(levels columnLevels, rows sparse.Array) {
	n := rows.Len()
	if n == 0 {
		return
	}

	stringArray := rows.StringArray()
	totalBytes := 0
	for i := range n {
		totalBytes += len(stringArray.Index(i))
	}

	offsetsStart := col.offsets.Len()
	lengthsStart := col.lengths.Len()
	valuesStart := col.values.Len()

	col.offsets.Resize(offsetsStart + n)
	col.lengths.Resize(lengthsStart + n)
	col.values.Resize(valuesStart + totalBytes)

	offsets := col.offsets.Slice()[:offsetsStart+n]
	lengths := col.lengths.Slice()[:lengthsStart+n]
	values := col.values.Slice()[:valuesStart+totalBytes]

	valueOffset := valuesStart
	for i := range n {
		s := stringArray.Index(i)
		offsets[offsetsStart+i] = uint32(valueOffset)
		lengths[lengthsStart+i] = uint32(len(s))
		copy(values[valueOffset:], s)
		valueOffset += len(s)
	}
}

func (col *byteArrayColumnBuffer) writeBoolean(levels columnLevels, value bool) {
	offset := col.values.Len()
	col.values.AppendFunc(func(b []byte) []byte {
		return strconv.AppendBool(b, value)
	})
	col.offsets.AppendValue(uint32(offset))
	col.lengths.AppendValue(uint32(col.values.Len() - offset))
}

func (col *byteArrayColumnBuffer) writeInt32(levels columnLevels, value int32) {
	offset := col.values.Len()
	col.values.AppendFunc(func(b []byte) []byte {
		return strconv.AppendInt(b, int64(value), 10)
	})
	col.offsets.AppendValue(uint32(offset))
	col.lengths.AppendValue(uint32(col.values.Len() - offset))
}

func (col *byteArrayColumnBuffer) writeInt64(levels columnLevels, value int64) {
	offset := col.values.Len()
	col.values.AppendFunc(func(b []byte) []byte {
		return strconv.AppendInt(b, value, 10)
	})
	col.offsets.AppendValue(uint32(offset))
	col.lengths.AppendValue(uint32(col.values.Len() - offset))
}

func (col *byteArrayColumnBuffer) writeInt96(levels columnLevels, value deprecated.Int96) {
	offset := col.values.Len()
	col.values.AppendFunc(func(b []byte) []byte {
		result, _ := value.Int().AppendText(b)
		return result
	})
	col.offsets.AppendValue(uint32(offset))
	col.lengths.AppendValue(uint32(col.values.Len() - offset))
}

func (col *byteArrayColumnBuffer) writeFloat(levels columnLevels, value float32) {
	offset := col.values.Len()
	col.values.AppendFunc(func(b []byte) []byte {
		return strconv.AppendFloat(b, float64(value), 'g', -1, 32)
	})
	col.offsets.AppendValue(uint32(offset))
	col.lengths.AppendValue(uint32(col.values.Len() - offset))
}

func (col *byteArrayColumnBuffer) writeDouble(levels columnLevels, value float64) {
	offset := col.values.Len()
	col.values.AppendFunc(func(b []byte) []byte {
		return strconv.AppendFloat(b, value, 'g', -1, 64)
	})
	col.offsets.AppendValue(uint32(offset))
	col.lengths.AppendValue(uint32(col.values.Len() - offset))
}

func (col *byteArrayColumnBuffer) writeByteArray(levels columnLevels, value []byte) {
	col.offsets.AppendValue(uint32(col.values.Len()))
	col.lengths.AppendValue(uint32(len(value)))
	col.values.Append(value...)
}

func (col *byteArrayColumnBuffer) writeNull(levels columnLevels) {
	col.offsets.AppendValue(uint32(col.values.Len()))
	col.lengths.AppendValue(0)
}

func (col *byteArrayColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset)
	numLengths := col.lengths.Len()
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(numLengths))
	case i >= numLengths:
		return 0, io.EOF
	default:
		for n < len(values) && i < numLengths {
			values[n] = col.makeValueBytes(col.index(i))
			n++
			i++
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}

func (col *byteArrayColumnBuffer) index(i int) []byte {
	offsets := col.offsets.Slice()
	lengths := col.lengths.Slice()
	values := col.values.Slice()
	offset := offsets[i]
	length := lengths[i]
	end := offset + length
	return values[offset:end:end]
}
