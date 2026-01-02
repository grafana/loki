package parquet

import (
	"io"
	"slices"

	"github.com/parquet-go/bitpack"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/sparse"
)

type booleanColumnBuffer struct{ booleanPage }

func newBooleanColumnBuffer(typ Type, columnIndex int16, numValues int32) *booleanColumnBuffer {
	// Boolean values are bit-packed, we can fit up to 8 values per byte.
	bufferSize := (numValues + 7) / 8
	return &booleanColumnBuffer{
		booleanPage: booleanPage{
			typ:         typ,
			bits:        make([]byte, 0, bufferSize),
			columnIndex: ^columnIndex,
		},
	}
}

func (col *booleanColumnBuffer) Clone() ColumnBuffer {
	return &booleanColumnBuffer{
		booleanPage: booleanPage{
			typ:         col.typ,
			bits:        slices.Clone(col.bits),
			offset:      col.offset,
			numValues:   col.numValues,
			columnIndex: col.columnIndex,
		},
	}
}

func (col *booleanColumnBuffer) ColumnIndex() (ColumnIndex, error) {
	return booleanColumnIndex{&col.booleanPage}, nil
}

func (col *booleanColumnBuffer) OffsetIndex() (OffsetIndex, error) {
	return booleanOffsetIndex{&col.booleanPage}, nil
}

func (col *booleanColumnBuffer) BloomFilter() BloomFilter { return nil }

func (col *booleanColumnBuffer) Dictionary() Dictionary { return nil }

func (col *booleanColumnBuffer) Pages() Pages { return onePage(col.Page()) }

func (col *booleanColumnBuffer) Page() Page { return &col.booleanPage }

func (col *booleanColumnBuffer) Reset() {
	col.bits = col.bits[:0]
	col.offset = 0
	col.numValues = 0
}

func (col *booleanColumnBuffer) Cap() int { return 8 * cap(col.bits) }

func (col *booleanColumnBuffer) Len() int { return int(col.numValues) }

func (col *booleanColumnBuffer) Less(i, j int) bool {
	a := col.valueAt(i)
	b := col.valueAt(j)
	return a != b && !a
}

func (col *booleanColumnBuffer) valueAt(i int) bool {
	j := uint32(i) / 8
	k := uint32(i) % 8
	return ((col.bits[j] >> k) & 1) != 0
}

func (col *booleanColumnBuffer) setValueAt(i int, v bool) {
	// `offset` is always zero in the page of a column buffer
	j := uint32(i) / 8
	k := uint32(i) % 8
	x := byte(0)
	if v {
		x = 1
	}
	col.bits[j] = (col.bits[j] & ^(1 << k)) | (x << k)
}

func (col *booleanColumnBuffer) Swap(i, j int) {
	a := col.valueAt(i)
	b := col.valueAt(j)
	col.setValueAt(i, b)
	col.setValueAt(j, a)
}

func (col *booleanColumnBuffer) WriteBooleans(values []bool) (int, error) {
	col.writeValues(columnLevels{}, sparse.MakeBoolArray(values).UnsafeArray())
	return len(values), nil
}

func (col *booleanColumnBuffer) WriteValues(values []Value) (int, error) {
	col.writeValues(columnLevels{}, makeArrayValue(values, offsetOfBool))
	return len(values), nil
}

func (col *booleanColumnBuffer) writeValues(_ columnLevels, rows sparse.Array) {
	numBytes := bitpack.ByteCount(uint(col.numValues) + uint(rows.Len()))
	if cap(col.bits) < numBytes {
		col.bits = append(make([]byte, 0, max(numBytes, 2*cap(col.bits))), col.bits...)
	}
	col.bits = col.bits[:numBytes]
	i := 0
	r := 8 - (int(col.numValues) % 8)
	bytes := rows.Uint8Array()

	if r <= bytes.Len() {
		// First we attempt to write enough bits to align the number of values
		// in the column buffer on 8 bytes. After this step the next bit should
		// be written at the zero'th index of a byte of the buffer.
		if r < 8 {
			var b byte
			for i < r {
				v := bytes.Index(i)
				b |= (v & 1) << uint(i)
				i++
			}
			x := uint(col.numValues) / 8
			y := uint(col.numValues) % 8
			col.bits[x] = (b << y) | (col.bits[x] & ^(0xFF << y))
			col.numValues += int32(i)
		}

		if n := ((bytes.Len() - i) / 8) * 8; n > 0 {
			// At this stage, we know that that we have at least 8 bits to write
			// and the bits will be aligned on the address of a byte in the
			// output buffer. We can work on 8 values per loop iteration,
			// packing them into a single byte and writing it to the output
			// buffer. This effectively reduces by 87.5% the number of memory
			// stores that the program needs to perform to generate the values.
			i += sparse.GatherBits(col.bits[col.numValues/8:], bytes.Slice(i, i+n))
			col.numValues += int32(n)
		}
	}

	for i < bytes.Len() {
		x := uint(col.numValues) / 8
		y := uint(col.numValues) % 8
		b := bytes.Index(i)
		col.bits[x] = ((b & 1) << y) | (col.bits[x] & ^(1 << y))
		col.numValues++
		i++
	}

	col.bits = col.bits[:bitpack.ByteCount(uint(col.numValues))]
}

func (col *booleanColumnBuffer) writeBoolean(levels columnLevels, value bool) {
	numBytes := bitpack.ByteCount(uint(col.numValues) + 1)
	if cap(col.bits) < numBytes {
		col.bits = append(make([]byte, 0, max(numBytes, 2*cap(col.bits))), col.bits...)
	}
	col.bits = col.bits[:numBytes]
	x := uint(col.numValues) / 8
	y := uint(col.numValues) % 8
	bit := byte(0)
	if value {
		bit = 1
	}
	col.bits[x] = (bit << y) | (col.bits[x] & ^(1 << y))
	col.numValues++
}

func (col *booleanColumnBuffer) writeInt32(levels columnLevels, value int32) {
	col.writeBoolean(levels, value != 0)
}

func (col *booleanColumnBuffer) writeInt64(levels columnLevels, value int64) {
	col.writeBoolean(levels, value != 0)
}

func (col *booleanColumnBuffer) writeInt96(levels columnLevels, value deprecated.Int96) {
	col.writeBoolean(levels, !value.IsZero())
}

func (col *booleanColumnBuffer) writeFloat(levels columnLevels, value float32) {
	col.writeBoolean(levels, value != 0)
}

func (col *booleanColumnBuffer) writeDouble(levels columnLevels, value float64) {
	col.writeBoolean(levels, value != 0)
}

func (col *booleanColumnBuffer) writeByteArray(levels columnLevels, value []byte) {
	col.writeBoolean(levels, len(value) != 0)
}

func (col *booleanColumnBuffer) writeNull(levels columnLevels) {
	col.writeBoolean(levels, false)
}

func (col *booleanColumnBuffer) ReadValuesAt(values []Value, offset int64) (n int, err error) {
	i := int(offset)
	switch {
	case i < 0:
		return 0, errRowIndexOutOfBounds(offset, int64(col.numValues))
	case i >= int(col.numValues):
		return 0, io.EOF
	default:
		for n < len(values) && i < int(col.numValues) {
			values[n] = col.makeValue(col.valueAt(i))
			n++
			i++
		}
		if n < len(values) {
			err = io.EOF
		}
		return n, err
	}
}
