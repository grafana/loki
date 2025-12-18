package parquet

import (
	"io"

	"github.com/parquet-go/bitpack/unsafecast"
	"github.com/parquet-go/parquet-go/encoding"
)

type uint32Page struct {
	typ         Type
	values      []uint32
	columnIndex int16
}

func newUint32Page(typ Type, columnIndex int16, numValues int32, values encoding.Values) *uint32Page {
	return &uint32Page{
		typ:         typ,
		values:      values.Uint32()[:numValues],
		columnIndex: ^columnIndex,
	}
}

func (page *uint32Page) Type() Type { return page.typ }

func (page *uint32Page) Column() int { return int(^page.columnIndex) }

func (page *uint32Page) Dictionary() Dictionary { return nil }

func (page *uint32Page) NumRows() int64 { return int64(len(page.values)) }

func (page *uint32Page) NumValues() int64 { return int64(len(page.values)) }

func (page *uint32Page) NumNulls() int64 { return 0 }

func (page *uint32Page) Size() int64 { return 4 * int64(len(page.values)) }

func (page *uint32Page) RepetitionLevels() []byte { return nil }

func (page *uint32Page) DefinitionLevels() []byte { return nil }

func (page *uint32Page) Data() encoding.Values { return encoding.Uint32Values(page.values) }

func (page *uint32Page) Values() ValueReader { return &uint32PageValues{page: page} }

func (page *uint32Page) min() uint32 { return minUint32(page.values) }

func (page *uint32Page) max() uint32 { return maxUint32(page.values) }

func (page *uint32Page) bounds() (min, max uint32) { return boundsUint32(page.values) }

func (page *uint32Page) Bounds() (min, max Value, ok bool) {
	if ok = len(page.values) > 0; ok {
		minUint32, maxUint32 := page.bounds()
		min = page.makeValue(minUint32)
		max = page.makeValue(maxUint32)
	}
	return min, max, ok
}

func (page *uint32Page) Slice(i, j int64) Page {
	return &uint32Page{
		typ:         page.typ,
		values:      page.values[i:j],
		columnIndex: page.columnIndex,
	}
}

func (page *uint32Page) makeValue(v uint32) Value {
	value := makeValueUint32(v)
	value.columnIndex = page.columnIndex
	return value
}

type uint32PageValues struct {
	page   *uint32Page
	offset int
}

func (r *uint32PageValues) Read(b []byte) (n int, err error) {
	n, err = r.ReadUint32s(unsafecast.Slice[uint32](b))
	return 4 * n, err
}

func (r *uint32PageValues) ReadUint32s(values []uint32) (n int, err error) {
	n = copy(values, r.page.values[r.offset:])
	r.offset += n
	if r.offset == len(r.page.values) {
		err = io.EOF
	}
	return n, err
}

func (r *uint32PageValues) ReadValues(values []Value) (n int, err error) {
	for n < len(values) && r.offset < len(r.page.values) {
		values[n] = r.page.makeValue(r.page.values[r.offset])
		r.offset++
		n++
	}
	if r.offset == len(r.page.values) {
		err = io.EOF
	}
	return n, err
}
