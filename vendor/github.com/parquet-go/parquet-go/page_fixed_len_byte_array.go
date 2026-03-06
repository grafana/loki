package parquet

import (
	"io"

	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/internal/memory"
)

type fixedLenByteArrayPage struct {
	typ         Type
	data        memory.SliceBuffer[byte]
	size        int
	columnIndex int16
}

func newFixedLenByteArrayPage(typ Type, columnIndex int16, numValues int32, values encoding.Values) *fixedLenByteArrayPage {
	data, size := values.FixedLenByteArray()
	return &fixedLenByteArrayPage{
		typ:         typ,
		data:        memory.SliceBufferFrom(data[:int(numValues)*size]),
		size:        size,
		columnIndex: ^columnIndex,
	}
}

func (page *fixedLenByteArrayPage) Type() Type { return page.typ }

func (page *fixedLenByteArrayPage) Column() int { return int(^page.columnIndex) }

func (page *fixedLenByteArrayPage) Dictionary() Dictionary { return nil }

func (page *fixedLenByteArrayPage) NumRows() int64 { return int64(page.data.Len() / page.size) }

func (page *fixedLenByteArrayPage) NumValues() int64 { return int64(page.data.Len() / page.size) }

func (page *fixedLenByteArrayPage) NumNulls() int64 { return 0 }

func (page *fixedLenByteArrayPage) Size() int64 { return int64(page.data.Len()) }

func (page *fixedLenByteArrayPage) RepetitionLevels() []byte { return nil }

func (page *fixedLenByteArrayPage) DefinitionLevels() []byte { return nil }

func (page *fixedLenByteArrayPage) Data() encoding.Values {
	return encoding.FixedLenByteArrayValues(page.data.Slice(), page.size)
}

func (page *fixedLenByteArrayPage) Values() ValueReader {
	return &fixedLenByteArrayPageValues{page: page}
}

func (page *fixedLenByteArrayPage) min() []byte {
	return minFixedLenByteArray(page.data.Slice(), page.size)
}

func (page *fixedLenByteArrayPage) max() []byte {
	return maxFixedLenByteArray(page.data.Slice(), page.size)
}

func (page *fixedLenByteArrayPage) bounds() (min, max []byte) {
	return boundsFixedLenByteArray(page.data.Slice(), page.size)
}

func (page *fixedLenByteArrayPage) Bounds() (min, max Value, ok bool) {
	if ok = page.data.Len() > 0; ok {
		minBytes, maxBytes := page.bounds()
		min = page.makeValueBytes(minBytes)
		max = page.makeValueBytes(maxBytes)
	}
	return min, max, ok
}

func (page *fixedLenByteArrayPage) Slice(i, j int64) Page {
	data := page.data.Slice()
	return &fixedLenByteArrayPage{
		typ:         page.typ,
		data:        memory.SliceBufferFrom(data[i*int64(page.size) : j*int64(page.size)]),
		size:        page.size,
		columnIndex: page.columnIndex,
	}
}

func (page *fixedLenByteArrayPage) makeValueBytes(v []byte) Value {
	value := makeValueBytes(FixedLenByteArray, v)
	value.columnIndex = page.columnIndex
	return value
}

func (page *fixedLenByteArrayPage) makeValueString(v string) Value {
	value := makeValueString(FixedLenByteArray, v)
	value.columnIndex = page.columnIndex
	return value
}

type fixedLenByteArrayPageValues struct {
	page   *fixedLenByteArrayPage
	offset int
}

func (r *fixedLenByteArrayPageValues) Read(b []byte) (n int, err error) {
	n, err = r.ReadFixedLenByteArrays(b)
	return n * r.page.size, err
}

func (r *fixedLenByteArrayPageValues) ReadRequired(values []byte) (int, error) {
	return r.ReadFixedLenByteArrays(values)
}

func (r *fixedLenByteArrayPageValues) ReadFixedLenByteArrays(values []byte) (n int, err error) {
	data := r.page.data.Slice()
	n = copy(values, data[r.offset:]) / r.page.size
	r.offset += n * r.page.size
	if r.offset == len(data) {
		err = io.EOF
	} else if n == 0 && len(values) > 0 {
		err = io.ErrShortBuffer
	}
	return n, err
}

func (r *fixedLenByteArrayPageValues) ReadValues(values []Value) (n int, err error) {
	data := r.page.data.Slice()
	for n < len(values) && r.offset < len(data) {
		values[n] = r.page.makeValueBytes(data[r.offset : r.offset+r.page.size])
		r.offset += r.page.size
		n++
	}
	if r.offset == len(data) {
		err = io.EOF
	}
	return n, err
}
