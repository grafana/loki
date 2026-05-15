package parquet

import (
	"io"
	"math"

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

// Bounds returns the min and max values in the page. NaN values are excluded
// from the result when non-NaN values exist so that query engines can rely on
// min/max for predicate pushdown and row-group/page skipping. This matches the
// behavior of Apache parquet-mr (PARQUET-1246), Apache Arrow, and the Apache
// Iceberg spec (which states lower/upper bounds apply to non-null, non-NaN
// values only). If all values are NaN, the bounds are reported as NaN so that
// readers know the page had data.
func (page *floatPage) Bounds() (min, max Value, ok bool) {
	if ok = page.values.Len() > 0; ok {
		data := page.values.Slice()
		i := 0
		for i < len(data) && math.IsNaN(float64(data[i])) {
			i++
		}
		if i >= len(data) {
			// All values are NaN.
			min = page.makeValue(data[0])
			max = page.makeValue(data[0])
			return min, max, ok
		}
		lo, hi := data[i], data[i]
		for _, v := range data[i+1:] {
			if math.IsNaN(float64(v)) {
				continue
			}
			if v < lo {
				lo = v
			}
			if v > hi {
				hi = v
			}
		}
		min = page.makeValue(lo)
		max = page.makeValue(hi)
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
