package parquet

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"slices"
	"unsafe"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/sparse"
)

type fixedLenByteArrayColumnBuffer struct {
	fixedLenByteArrayPage
	tmp []byte
}

func newFixedLenByteArrayColumnBuffer(typ Type, columnIndex int16, numValues int32) *fixedLenByteArrayColumnBuffer {
	size := typ.Length()
	return &fixedLenByteArrayColumnBuffer{
		fixedLenByteArrayPage: fixedLenByteArrayPage{
			typ:         typ,
			size:        size,
			data:        make([]byte, 0, typ.EstimateSize(int(numValues))),
			columnIndex: ^columnIndex,
		},
		tmp: make([]byte, size),
	}
}

func (col *fixedLenByteArrayColumnBuffer) Clone() ColumnBuffer {
	return &fixedLenByteArrayColumnBuffer{
		fixedLenByteArrayPage: fixedLenByteArrayPage{
			typ:         col.typ,
			size:        col.size,
			data:        slices.Clone(col.data),
			columnIndex: col.columnIndex,
		},
		tmp: make([]byte, col.size),
	}
}

func (col *fixedLenByteArrayColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return fixedLenByteArrayColumnIndex{&col.fixedLenByteArrayPage}, nil
}

func (col *fixedLenByteArrayColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return fixedLenByteArrayOffsetIndex{&col.fixedLenByteArrayPage}, nil
}

func (col *fixedLenByteArrayColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *fixedLenByteArrayColumnBuffer) Dictionary() Dictionary { return nil }

func (col *fixedLenByteArrayColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *fixedLenByteArrayColumnBuffer) Page() Page { return &col.fixedLenByteArrayPage }

func (col *fixedLenByteArrayColumnBuffer) Reset() { col.data = col.data[:0] }

func (col *fixedLenByteArrayColumnBuffer) Cap() int { return cap(col.data) / col.size }

func (col *fixedLenByteArrayColumnBuffer) Len() int { return len(col.data) / col.size }

func (col *fixedLenByteArrayColumnBuffer) Less(i, j int) bool {
	return bytes.Compare(col.index(i), col.index(j)) < 0
}

func (col *fixedLenByteArrayColumnBuffer) Swap(i, j int) {
	t, u, v := col.tmp[:col.size], col.index(i), col.index(j)
	copy(t, u)
	copy(u, v)
	copy(v, t)
}

func (col *fixedLenByteArrayColumnBuffer) index(i int) []byte {
	j := (i + 0) * col.size
	k := (i + 1) * col.size
	return col.data[j:k:k]
}

func (col *fixedLenByteArrayColumnBuffer) Write(b []byte) (int, error) {
	n, err := col.WriteFixedLenByteArrays(b)
	return n * col.size, err
}

func (col *fixedLenByteArrayColumnBuffer) WriteFixedLenByteArrays(values []byte) (int, error) {
	if len(values) == 0 {
		return 0, nil
	}
	d, m := len(values)/col.size, len(values)%col.size
	if d == 0 || m != 0 {
		return 0, fmt.Errorf("cannot write FIXED_LEN_BYTE_ARRAY values of size %d from input of size %d", col.size, len(values))
	}
	col.data = append(col.data, values...)
	return d, nil
}

func (col *fixedLenByteArrayColumnBuffer) WriteValues(values []Value) (int, error) {
	for i, v := range values {
		if n := len(v.byteArray()); n != col.size {
			return i, fmt.Errorf("cannot write FIXED_LEN_BYTE_ARRAY values of size %d from input of size %d", col.size, n)
		}
		col.data = append(col.data, v.byteArray()...)
	}
	return len(values), nil
}

func (col *fixedLenByteArrayColumnBuffer) writeValues(_ columnLevels, rows sparse.Array) {
	n := col.size * rows.Len()
	i := len(col.data)
	j := len(col.data) + n

	if cap(col.data) < j {
		col.data = append(make([]byte, 0, max(i+n, 2*cap(col.data))), col.data...)
	}

	col.data = col.data[:j]
	newData := col.data[i:]

	for i := range rows.Len() {
		p := rows.Index(i)
		copy(newData[i*col.size:], unsafe.Slice((*byte)(p), col.size))
	}
}

func (col *fixedLenByteArrayColumnBuffer) writeBoolean(levels columnLevels, value bool) {
	var fixedLenByteArrayValue [1]byte
	if value {
		fixedLenByteArrayValue[0] = 1
	}
	col.writeBigEndian(fixedLenByteArrayValue[:])
}

func (col *fixedLenByteArrayColumnBuffer) writeInt32(levels columnLevels, value int32) {
	var fixedLenByteArrayValue [4]byte
	binary.BigEndian.PutUint32(fixedLenByteArrayValue[:], uint32(value))
	col.writeBigEndian(fixedLenByteArrayValue[:])
}

func (col *fixedLenByteArrayColumnBuffer) writeInt64(levels columnLevels, value int64) {
	var fixedLenByteArrayValue [8]byte
	binary.BigEndian.PutUint64(fixedLenByteArrayValue[:], uint64(value))
	col.writeBigEndian(fixedLenByteArrayValue[:])
}

func (col *fixedLenByteArrayColumnBuffer) writeInt96(levels columnLevels, value deprecated.Int96) {
	var fixedLenByteArrayValue [12]byte
	binary.BigEndian.PutUint32(fixedLenByteArrayValue[0:4], value[2])
	binary.BigEndian.PutUint32(fixedLenByteArrayValue[4:8], value[1])
	binary.BigEndian.PutUint32(fixedLenByteArrayValue[8:12], value[0])
	col.writeBigEndian(fixedLenByteArrayValue[:])
}

func (col *fixedLenByteArrayColumnBuffer) writeFloat(levels columnLevels, value float32) {
	var fixedLenByteArrayValue [4]byte
	binary.BigEndian.PutUint32(fixedLenByteArrayValue[:], math.Float32bits(value))
	col.writeBigEndian(fixedLenByteArrayValue[:])
}

func (col *fixedLenByteArrayColumnBuffer) writeDouble(levels columnLevels, value float64) {
	var fixedLenByteArrayValue [8]byte
	binary.BigEndian.PutUint64(fixedLenByteArrayValue[:], math.Float64bits(value))
	col.writeBigEndian(fixedLenByteArrayValue[:])
}

func (col *fixedLenByteArrayColumnBuffer) writeByteArray(levels columnLevels, value []byte) {
	if col.size != len(value) {
		panic(fmt.Sprintf("cannot write byte array of length %d to fixed length byte array column of size %d", len(value), col.size))
	}
	col.data = append(col.data, value...)
}

func (col *fixedLenByteArrayColumnBuffer) writeNull(levels columnLevels) {
	col.data = append(col.data, make([]byte, col.size)...)
}

func (col *fixedLenByteArrayColumnBuffer) writeBigEndian(value []byte) {
	if col.size < len(value) {
		panic(fmt.Sprintf("cannot write byte array of length %d to fixed length byte array column of size %d", len(value), col.size))
	}
	col.data = append(col.data, make([]byte, col.size-len(value))...)
	col.data = append(col.data, value...)
}

func (col *fixedLenByteArrayColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset) * col.size
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(len(col.data)/col.size))
	case i >= len(col.data):
		return 0, io.EOF
	default:
		for n < len(values) && i < len(col.data) {
			values[n] = col.makeValueBytes(col.data[i : i+col.size])
			n++
			i += col.size
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}
