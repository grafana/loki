package parquet

import (
	"io"

	"github.com/parquet-go/bitpack/unsafecast"
	"github.com/parquet-go/parquet-go/encoding"
)

type doublePage struct {
	typ         Type
	values      []float64
	columnIndex int16
}

func newDoublePage(typ Type, columnIndex int16, numValues int32, values encoding.Values) *doublePage {
	return &doublePage{
		typ:         typ,
		values:      values.Double()[:numValues],
		columnIndex: ^columnIndex,
	}
}

func (page *doublePage) Type() Type { return page.typ }

func (page *doublePage) Column() int { return int(^page.columnIndex) }

func (page *doublePage) Dictionary() Dictionary { return nil }

func (page *doublePage) NumRows() int64 { return int64(len(page.values)) }

func (page *doublePage) NumValues() int64 { return int64(len(page.values)) }

func (page *doublePage) NumNulls() int64 { return 0 }

func (page *doublePage) Size() int64 { return 8 * int64(len(page.values)) }

func (page *doublePage) RepetitionLevels() []byte { return nil }

func (page *doublePage) DefinitionLevels() []byte { return nil }

func (page *doublePage) Data() encoding.Values { return encoding.DoubleValues(page.values) }

func (page *doublePage) Values() ValueReader { return &doublePageValues{page: page} }

func (page *doublePage) min() float64 { return minFloat64(page.values) }

func (page *doublePage) max() float64 { return maxFloat64(page.values) }

func (page *doublePage) bounds() (min, max float64) { return boundsFloat64(page.values) }

func (page *doublePage) Bounds() (min, max Value, ok bool) {
	if ok = len(page.values) > 0; ok {
		minFloat64, maxFloat64 := page.bounds()
		min = page.makeValue(minFloat64)
		max = page.makeValue(maxFloat64)
	}
	return min, max, ok
}

func (page *doublePage) Slice(i, j int64) Page {
	return &doublePage{
		typ:         page.typ,
		values:      page.values[i:j],
		columnIndex: page.columnIndex,
	}
}

func (page *doublePage) makeValue(v float64) Value {
	value := makeValueDouble(v)
	value.columnIndex = page.columnIndex
	return value
}

type doublePageValues struct {
	page   *doublePage
	offset int
}

func (r *doublePageValues) Read(b []byte) (n int, err error) {
	n, err = r.ReadDoubles(unsafecast.Slice[float64](b))
	return 8 * n, err
}

func (r *doublePageValues) ReadDoubles(values []float64) (n int, err error) {
	n = copy(values, r.page.values[r.offset:])
	r.offset += n
	if r.offset == len(r.page.values) {
		err = io.EOF
	}
	return n, err
}

func (r *doublePageValues) ReadValues(values []Value) (n int, err error) {
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
