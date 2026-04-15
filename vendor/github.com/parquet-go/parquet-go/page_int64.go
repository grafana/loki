package parquet

import (
	"io"

	"github.com/parquet-go/bitpack/unsafecast"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/internal/memory"
)

type int64Page struct {
	typ         Type
	values      memory.SliceBuffer[int64]
	columnIndex int16
}

func newInt64Page(typ Type, columnIndex int16, numValues int32, values encoding.Values) *int64Page {
	return &int64Page{
		typ:         typ,
		values:      memory.SliceBufferFrom(values.Int64()[:numValues]),
		columnIndex: ^columnIndex,
	}
}

func (page *int64Page) Type() Type { return page.typ }

func (page *int64Page) Column() int { return int(^page.columnIndex) }

func (page *int64Page) Dictionary() Dictionary { return nil }

func (page *int64Page) NumRows() int64 { return int64(page.values.Len()) }

func (page *int64Page) NumValues() int64 { return int64(page.values.Len()) }

func (page *int64Page) NumNulls() int64 { return 0 }

func (page *int64Page) Size() int64 { return 8 * int64(page.values.Len()) }

func (page *int64Page) RepetitionLevels() []byte { return nil }

func (page *int64Page) DefinitionLevels() []byte { return nil }

func (page *int64Page) Data() encoding.Values { return encoding.Int64Values(page.values.Slice()) }

func (page *int64Page) Values() ValueReader { return &int64PageValues{page: page} }

func (page *int64Page) min() int64 { return minInt64(page.values.Slice()) }

func (page *int64Page) max() int64 { return maxInt64(page.values.Slice()) }

func (page *int64Page) bounds() (min, max int64) { return boundsInt64(page.values.Slice()) }

func (page *int64Page) Bounds() (min, max Value, ok bool) {
	if ok = page.values.Len() > 0; ok {
		minInt64, maxInt64 := page.bounds()
		min = page.makeValue(minInt64)
		max = page.makeValue(maxInt64)
	}
	return min, max, ok
}

func (page *int64Page) Slice(i, j int64) Page {
	sliced := &int64Page{
		typ:         page.typ,
		columnIndex: page.columnIndex,
	}
	sliced.values.Append(page.values.Slice()[i:j]...)
	return sliced
}

func (page *int64Page) makeValue(v int64) Value {
	value := makeValueInt64(v)
	value.columnIndex = page.columnIndex
	return value
}

type int64PageValues struct {
	page   *int64Page
	offset int
}

func (r *int64PageValues) Read(b []byte) (n int, err error) {
	n, err = r.ReadInt64s(unsafecast.Slice[int64](b))
	return 8 * n, err
}

func (r *int64PageValues) ReadInt64s(values []int64) (n int, err error) {
	pageValues := r.page.values.Slice()
	n = copy(values, pageValues[r.offset:])
	r.offset += n
	if r.offset == len(pageValues) {
		err = io.EOF
	}
	return n, err
}

func (r *int64PageValues) ReadValues(values []Value) (n int, err error) {
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
