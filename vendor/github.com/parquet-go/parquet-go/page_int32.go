package parquet

import (
	"io"

	"github.com/parquet-go/bitpack/unsafecast"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/internal/memory"
)

type int32Page struct {
	typ         Type
	values      memory.SliceBuffer[int32]
	columnIndex int16
}

func newInt32Page(typ Type, columnIndex int16, numValues int32, values encoding.Values) *int32Page {
	return &int32Page{
		typ:         typ,
		values:      memory.SliceBufferFrom(values.Int32()[:numValues]),
		columnIndex: ^columnIndex,
	}
}

func (page *int32Page) Type() Type { return page.typ }

func (page *int32Page) Column() int { return int(^page.columnIndex) }

func (page *int32Page) Dictionary() Dictionary { return nil }

func (page *int32Page) NumRows() int64 { return int64(page.values.Len()) }

func (page *int32Page) NumValues() int64 { return int64(page.values.Len()) }

func (page *int32Page) NumNulls() int64 { return 0 }

func (page *int32Page) Size() int64 { return 4 * int64(page.values.Len()) }

func (page *int32Page) RepetitionLevels() []byte { return nil }

func (page *int32Page) DefinitionLevels() []byte { return nil }

func (page *int32Page) Data() encoding.Values { return encoding.Int32Values(page.values.Slice()) }

func (page *int32Page) Values() ValueReader { return &int32PageValues{page: page} }

func (page *int32Page) min() int32 { return minInt32(page.values.Slice()) }

func (page *int32Page) max() int32 { return maxInt32(page.values.Slice()) }

func (page *int32Page) bounds() (min, max int32) { return boundsInt32(page.values.Slice()) }

func (page *int32Page) Bounds() (min, max Value, ok bool) {
	if ok = page.values.Len() > 0; ok {
		minInt32, maxInt32 := page.bounds()
		min = page.makeValue(minInt32)
		max = page.makeValue(maxInt32)
	}
	return min, max, ok
}

func (page *int32Page) Slice(i, j int64) Page {
	sliced := &int32Page{
		typ:         page.typ,
		columnIndex: page.columnIndex,
	}
	sliced.values.Append(page.values.Slice()[i:j]...)
	return sliced
}

func (page *int32Page) makeValue(v int32) Value {
	value := makeValueInt32(v)
	value.columnIndex = page.columnIndex
	return value
}

type int32PageValues struct {
	page   *int32Page
	offset int
}

func (r *int32PageValues) Read(b []byte) (n int, err error) {
	n, err = r.ReadInt32s(unsafecast.Slice[int32](b))
	return 4 * n, err
}

func (r *int32PageValues) ReadInt32s(values []int32) (n int, err error) {
	pageValues := r.page.values.Slice()
	n = copy(values, pageValues[r.offset:])
	r.offset += n
	if r.offset == len(pageValues) {
		err = io.EOF
	}
	return n, err
}

func (r *int32PageValues) ReadValues(values []Value) (n int, err error) {
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
