package parquet

import (
	"io"

	"github.com/parquet-go/bitpack/unsafecast"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/internal/memory"
)

type floatPage struct {
	typ         Type
	values      memory.SliceBuffer[float32]
	columnIndex int16
}

func newFloatPage(typ Type, columnIndex int16, numValues int32, values encoding.Values) *floatPage {
	return &floatPage{
		typ:         typ,
		values:      memory.SliceBufferFrom(values.Float()[:numValues]),
		columnIndex: ^columnIndex,
	}
}

func (page *floatPage) Type() Type { return page.typ }

func (page *floatPage) Column() int { return int(^page.columnIndex) }

func (page *floatPage) Dictionary() Dictionary { return nil }

func (page *floatPage) NumRows() int64 { return int64(page.values.Len()) }

func (page *floatPage) NumValues() int64 { return int64(page.values.Len()) }

func (page *floatPage) NumNulls() int64 { return 0 }

func (page *floatPage) Size() int64 { return 4 * int64(page.values.Len()) }

func (page *floatPage) RepetitionLevels() []byte { return nil }

func (page *floatPage) DefinitionLevels() []byte { return nil }

func (page *floatPage) Data() encoding.Values { return encoding.FloatValues(page.values.Slice()) }

func (page *floatPage) Values() ValueReader { return &floatPageValues{page: page} }

func (page *floatPage) min() float32 { return minFloat32(page.values.Slice()) }

func (page *floatPage) max() float32 { return maxFloat32(page.values.Slice()) }

func (page *floatPage) bounds() (min, max float32) { return boundsFloat32(page.values.Slice()) }

func (page *floatPage) Bounds() (min, max Value, ok bool) {
	if ok = page.values.Len() > 0; ok {
		minFloat32, maxFloat32 := page.bounds()
		min = page.makeValue(minFloat32)
		max = page.makeValue(maxFloat32)
	}
	return min, max, ok
}

func (page *floatPage) Slice(i, j int64) Page {
	return &floatPage{
		typ:         page.typ,
		values:      memory.SliceBufferFrom(page.values.Slice()[i:j]),
		columnIndex: page.columnIndex,
	}
}

func (page *floatPage) makeValue(v float32) Value {
	value := makeValueFloat(v)
	value.columnIndex = page.columnIndex
	return value
}

type floatPageValues struct {
	page   *floatPage
	offset int
}

func (r *floatPageValues) Read(b []byte) (n int, err error) {
	n, err = r.ReadFloats(unsafecast.Slice[float32](b))
	return 4 * n, err
}

func (r *floatPageValues) ReadFloats(values []float32) (n int, err error) {
	pageValues := r.page.values.Slice()
	n = copy(values, pageValues[r.offset:])
	r.offset += n
	if r.offset == len(pageValues) {
		err = io.EOF
	}
	return n, err
}

func (r *floatPageValues) ReadValues(values []Value) (n int, err error) {
	pageValues := r.page.values.Slice()
	for n < len(values) && r.offset < len(pageValues) {
		values[n] = r.page.makeValue(pageValues[r.offset])
		r.offset++
		n++
	}
	if r.offset == len(pageValues) {
		err = io.EOF
	}
	return n, err
}
