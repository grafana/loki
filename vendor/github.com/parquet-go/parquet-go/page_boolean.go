package parquet

import (
	"io"

	"github.com/parquet-go/bitpack"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/internal/memory"
)

type booleanPage struct {
	typ         Type
	bits        memory.SliceBuffer[byte]
	offset      int32
	numValues   int32
	columnIndex int16
}

func newBooleanPage(typ Type, columnIndex int16, numValues int32, values encoding.Values) *booleanPage {
	return &booleanPage{
		typ:         typ,
		bits:        memory.SliceBufferFrom(values.Boolean()[:bitpack.ByteCount(uint(numValues))]),
		numValues:   numValues,
		columnIndex: ^columnIndex,
	}
}

func (page *booleanPage) Type() Type { return page.typ }

func (page *booleanPage) Column() int { return int(^page.columnIndex) }

func (page *booleanPage) Dictionary() Dictionary { return nil }

func (page *booleanPage) NumRows() int64 { return int64(page.numValues) }

func (page *booleanPage) NumValues() int64 { return int64(page.numValues) }

func (page *booleanPage) NumNulls() int64 { return 0 }

func (page *booleanPage) Size() int64 { return int64(page.bits.Len()) }

func (page *booleanPage) RepetitionLevels() []byte { return nil }

func (page *booleanPage) DefinitionLevels() []byte { return nil }

func (page *booleanPage) Data() encoding.Values { return encoding.BooleanValues(page.bits.Slice()) }

func (page *booleanPage) Values() ValueReader { return &booleanPageValues{page: page} }

func (page *booleanPage) valueAt(i int) bool {
	bits := page.bits.Slice()
	j := uint32(int(page.offset)+i) / 8
	k := uint32(int(page.offset)+i) % 8
	return ((bits[j] >> k) & 1) != 0
}

func (page *booleanPage) min() bool {
	for i := range int(page.numValues) {
		if !page.valueAt(i) {
			return false
		}
	}
	return page.numValues > 0
}

func (page *booleanPage) max() bool {
	for i := range int(page.numValues) {
		if page.valueAt(i) {
			return true
		}
	}
	return false
}

func (page *booleanPage) bounds() (min, max bool) {
	hasFalse, hasTrue := false, false

	for i := range int(page.numValues) {
		v := page.valueAt(i)
		if v {
			hasTrue = true
		} else {
			hasFalse = true
		}
		if hasTrue && hasFalse {
			break
		}
	}

	min = !hasFalse
	max = hasTrue
	return min, max
}

func (page *booleanPage) Bounds() (min, max Value, ok bool) {
	if ok = page.numValues > 0; ok {
		minBool, maxBool := page.bounds()
		min = page.makeValue(minBool)
		max = page.makeValue(maxBool)
	}
	return min, max, ok
}

func (page *booleanPage) Slice(i, j int64) Page {
	lowWithOffset := i + int64(page.offset)
	highWithOffset := j + int64(page.offset)

	off := lowWithOffset / 8
	end := highWithOffset / 8

	if (highWithOffset % 8) != 0 {
		end++
	}

	return &booleanPage{
		typ:         page.typ,
		bits:        memory.SliceBufferFrom(page.bits.Slice()[off:end]),
		offset:      int32(lowWithOffset % 8),
		numValues:   int32(j - i),
		columnIndex: page.columnIndex,
	}
}

func (page *booleanPage) makeValue(v bool) Value {
	value := makeValueBoolean(v)
	value.columnIndex = page.columnIndex
	return value
}

type booleanPageValues struct {
	page   *booleanPage
	offset int
}

func (r *booleanPageValues) ReadBooleans(values []bool) (n int, err error) {
	for n < len(values) && r.offset < int(r.page.numValues) {
		values[n] = r.page.valueAt(r.offset)
		r.offset++
		n++
	}
	if r.offset == int(r.page.numValues) {
		err = io.EOF
	}
	return n, err
}

func (r *booleanPageValues) ReadValues(values []Value) (n int, err error) {
	for n < len(values) && r.offset < int(r.page.numValues) {
		values[n] = r.page.makeValue(r.page.valueAt(r.offset))
		r.offset++
		n++
	}
	if r.offset == int(r.page.numValues) {
		err = io.EOF
	}
	return n, err
}
