package parquet

import (
	"io"

	"github.com/parquet-go/parquet-go/encoding"
)

type be128Page struct {
	typ         Type
	values      [][16]byte
	columnIndex int16
}

func newBE128Page(typ Type, columnIndex int16, numValues int32, values encoding.Values) *be128Page {
	return &be128Page{
		typ:         typ,
		values:      values.Uint128()[:numValues],
		columnIndex: ^columnIndex,
	}
}

func (page *be128Page) Type() Type { return page.typ }

func (page *be128Page) Column() int { return int(^page.columnIndex) }

func (page *be128Page) Dictionary() Dictionary { return nil }

func (page *be128Page) NumRows() int64 { return int64(len(page.values)) }

func (page *be128Page) NumValues() int64 { return int64(len(page.values)) }

func (page *be128Page) NumNulls() int64 { return 0 }

func (page *be128Page) Size() int64 { return 16 * int64(len(page.values)) }

func (page *be128Page) RepetitionLevels() []byte { return nil }

func (page *be128Page) DefinitionLevels() []byte { return nil }

func (page *be128Page) Data() encoding.Values { return encoding.Uint128Values(page.values) }

func (page *be128Page) Values() ValueReader { return &be128PageValues{page: page} }

func (page *be128Page) min() []byte { return minBE128(page.values) }

func (page *be128Page) max() []byte { return maxBE128(page.values) }

func (page *be128Page) bounds() (min, max []byte) { return boundsBE128(page.values) }

func (page *be128Page) Bounds() (min, max Value, ok bool) {
	if ok = len(page.values) > 0; ok {
		minBytes, maxBytes := page.bounds()
		min = page.makeValueBytes(minBytes)
		max = page.makeValueBytes(maxBytes)
	}
	return min, max, ok
}

func (page *be128Page) Slice(i, j int64) Page {
	return &be128Page{
		typ:         page.typ,
		values:      page.values[i:j],
		columnIndex: page.columnIndex,
	}
}

func (page *be128Page) makeValue(v *[16]byte) Value {
	return page.makeValueBytes(v[:])
}

func (page *be128Page) makeValueBytes(v []byte) Value {
	value := makeValueBytes(FixedLenByteArray, v)
	value.columnIndex = page.columnIndex
	return value
}

func (page *be128Page) makeValueString(v string) Value {
	value := makeValueString(FixedLenByteArray, v)
	value.columnIndex = page.columnIndex
	return value
}

type be128PageValues struct {
	page   *be128Page
	offset int
}

func (r *be128PageValues) ReadValues(values []Value) (n int, err error) {
	for n < len(values) && r.offset < len(r.page.values) {
		values[n] = r.page.makeValue(&r.page.values[r.offset])
		r.offset++
		n++
	}
	if r.offset == len(r.page.values) {
		err = io.EOF
	}
	return n, err
}
