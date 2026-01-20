package parquet

import (
	"io"

	"github.com/parquet-go/bitpack/unsafecast"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/internal/memory"
)

type uint64Page struct {
	typ         Type
	values      memory.SliceBuffer[uint64]
	columnIndex int16
}

func newUint64Page(typ Type, columnIndex int16, numValues int32, values encoding.Values) *uint64Page {
	return &uint64Page{
		typ:         typ,
		values:      memory.SliceBufferFrom(values.Uint64()[:numValues]),
		columnIndex: ^columnIndex,
	}
}

func (page *uint64Page) Type() Type { return page.typ }

func (page *uint64Page) Column() int { return int(^page.columnIndex) }

func (page *uint64Page) Dictionary() Dictionary { return nil }

func (page *uint64Page) NumRows() int64 { return int64(page.values.Len()) }

func (page *uint64Page) NumValues() int64 { return int64(page.values.Len()) }

func (page *uint64Page) NumNulls() int64 { return 0 }

func (page *uint64Page) Size() int64 { return 8 * int64(page.values.Len()) }

func (page *uint64Page) RepetitionLevels() []byte { return nil }

func (page *uint64Page) DefinitionLevels() []byte { return nil }

func (page *uint64Page) Data() encoding.Values { return encoding.Uint64Values(page.values.Slice()) }

func (page *uint64Page) Values() ValueReader { return &uint64PageValues{page: page} }

func (page *uint64Page) min() uint64 { return minUint64(page.values.Slice()) }

func (page *uint64Page) max() uint64 { return maxUint64(page.values.Slice()) }

func (page *uint64Page) bounds() (min, max uint64) { return boundsUint64(page.values.Slice()) }

func (page *uint64Page) Bounds() (min, max Value, ok bool) {
	if ok = page.values.Len() > 0; ok {
		minUint64, maxUint64 := page.bounds()
		min = page.makeValue(minUint64)
		max = page.makeValue(maxUint64)
	}
	return min, max, ok
}

func (page *uint64Page) Slice(i, j int64) Page {
	sliced := &uint64Page{
		typ:         page.typ,
		columnIndex: page.columnIndex,
	}
	sliced.values.Append(page.values.Slice()[i:j]...)
	return sliced
}

func (page *uint64Page) makeValue(v uint64) Value {
	value := makeValueUint64(v)
	value.columnIndex = page.columnIndex
	return value
}

type uint64PageValues struct {
	page   *uint64Page
	offset int
}

func (r *uint64PageValues) Read(b []byte) (n int, err error) {
	n, err = r.ReadUint64s(unsafecast.Slice[uint64](b))
	return 8 * n, err
}

func (r *uint64PageValues) ReadUint64s(values []uint64) (n int, err error) {
	pageValues := r.page.values.Slice()
	n = copy(values, pageValues[r.offset:])
	r.offset += n
	if r.offset == len(pageValues) {
		err = io.EOF
	}
	return n, err
}

func (r *uint64PageValues) ReadValues(values []Value) (n int, err error) {
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
