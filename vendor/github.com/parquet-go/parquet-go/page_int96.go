package parquet

import (
	"io"

	"github.com/parquet-go/bitpack/unsafecast"
	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
)

type int96Page struct {
	typ         Type
	values      []deprecated.Int96
	columnIndex int16
}

func newInt96Page(typ Type, columnIndex int16, numValues int32, values encoding.Values) *int96Page {
	return &int96Page{
		typ:         typ,
		values:      values.Int96()[:numValues],
		columnIndex: ^columnIndex,
	}
}

func (page *int96Page) Type() Type { return page.typ }

func (page *int96Page) Column() int { return int(^page.columnIndex) }

func (page *int96Page) Dictionary() Dictionary { return nil }

func (page *int96Page) NumRows() int64 { return int64(len(page.values)) }

func (page *int96Page) NumValues() int64 { return int64(len(page.values)) }

func (page *int96Page) NumNulls() int64 { return 0 }

func (page *int96Page) Size() int64 { return 12 * int64(len(page.values)) }

func (page *int96Page) RepetitionLevels() []byte { return nil }

func (page *int96Page) DefinitionLevels() []byte { return nil }

func (page *int96Page) Data() encoding.Values { return encoding.Int96Values(page.values) }

func (page *int96Page) Values() ValueReader { return &int96PageValues{page: page} }

func (page *int96Page) min() deprecated.Int96 { return deprecated.MinInt96(page.values) }

func (page *int96Page) max() deprecated.Int96 { return deprecated.MaxInt96(page.values) }

func (page *int96Page) bounds() (min, max deprecated.Int96) {
	return deprecated.MinMaxInt96(page.values)
}

func (page *int96Page) Bounds() (min, max Value, ok bool) {
	if ok = len(page.values) > 0; ok {
		minInt96, maxInt96 := page.bounds()
		min = page.makeValue(minInt96)
		max = page.makeValue(maxInt96)
	}
	return min, max, ok
}

func (page *int96Page) Slice(i, j int64) Page {
	return &int96Page{
		typ:         page.typ,
		values:      page.values[i:j],
		columnIndex: page.columnIndex,
	}
}

func (page *int96Page) makeValue(v deprecated.Int96) Value {
	value := makeValueInt96(v)
	value.columnIndex = page.columnIndex
	return value
}

type int96PageValues struct {
	page   *int96Page
	offset int
}

func (r *int96PageValues) Read(b []byte) (n int, err error) {
	n, err = r.ReadInt96s(unsafecast.Slice[deprecated.Int96](b))
	return 12 * n, err
}

func (r *int96PageValues) ReadInt96s(values []deprecated.Int96) (n int, err error) {
	n = copy(values, r.page.values[r.offset:])
	r.offset += n
	if r.offset == len(r.page.values) {
		err = io.EOF
	}
	return n, err
}

func (r *int96PageValues) ReadValues(values []Value) (n int, err error) {
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
