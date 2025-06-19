package parquet

import (
	"io"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding/plain"
	"github.com/parquet-go/parquet-go/internal/unsafecast"
)

type optionalPageValues struct {
	page   *optionalPage
	values ValueReader
	offset int
}

func (r *optionalPageValues) ReadValues(values []Value) (n int, err error) {
	maxDefinitionLevel := r.page.maxDefinitionLevel
	definitionLevels := r.page.definitionLevels
	columnIndex := ^int16(r.page.Column())

	for n < len(values) && r.offset < len(definitionLevels) {
		for n < len(values) && r.offset < len(definitionLevels) && definitionLevels[r.offset] != maxDefinitionLevel {
			values[n] = Value{
				definitionLevel: definitionLevels[r.offset],
				columnIndex:     columnIndex,
			}
			r.offset++
			n++
		}

		i := n
		j := r.offset
		for i < len(values) && j < len(definitionLevels) && definitionLevels[j] == maxDefinitionLevel {
			i++
			j++
		}

		if n < i {
			for j, err = r.values.ReadValues(values[n:i]); j > 0; j-- {
				values[n].definitionLevel = maxDefinitionLevel
				r.offset++
				n++
			}
			// Do not return on an io.EOF here as we may still have null values to read.
			if err != nil && err != io.EOF {
				return n, err
			}
			err = nil
		}
	}

	if r.offset == len(definitionLevels) {
		err = io.EOF
	}
	return n, err
}

type repeatedPageValues struct {
	page   *repeatedPage
	values ValueReader
	offset int
}

func (r *repeatedPageValues) ReadValues(values []Value) (n int, err error) {
	maxDefinitionLevel := r.page.maxDefinitionLevel
	definitionLevels := r.page.definitionLevels
	repetitionLevels := r.page.repetitionLevels
	columnIndex := ^int16(r.page.Column())

	// While we haven't exceeded the output buffer and we haven't exceeded the page size.
	for n < len(values) && r.offset < len(definitionLevels) {

		// While we haven't exceeded the output buffer and we haven't exceeded the
		// page size AND the current element's definitionLevel is not the
		// maxDefinitionLevel (this is a null value), Create the zero values to be
		// returned in this run.
		for n < len(values) && r.offset < len(definitionLevels) && definitionLevels[r.offset] != maxDefinitionLevel {
			values[n] = Value{
				repetitionLevel: repetitionLevels[r.offset],
				definitionLevel: definitionLevels[r.offset],
				columnIndex:     columnIndex,
			}
			r.offset++
			n++
		}

		i := n
		j := r.offset
		// Get the length of the run of non-zero values to be copied.
		for i < len(values) && j < len(definitionLevels) && definitionLevels[j] == maxDefinitionLevel {
			i++
			j++
		}

		// Copy all the non-zero values in this run.
		if n < i {
			for j, err = r.values.ReadValues(values[n:i]); j > 0; j-- {
				values[n].repetitionLevel = repetitionLevels[r.offset]
				values[n].definitionLevel = maxDefinitionLevel
				r.offset++
				n++
			}
			if err != nil && err != io.EOF {
				return n, err
			}
			err = nil
		}
	}

	if r.offset == len(definitionLevels) {
		err = io.EOF
	}
	return n, err
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

type int32PageValues struct {
	page   *int32Page
	offset int
}

func (r *int32PageValues) Read(b []byte) (n int, err error) {
	n, err = r.ReadInt32s(unsafecast.Slice[int32](b))
	return 4 * n, err
}

func (r *int32PageValues) ReadInt32s(values []int32) (n int, err error) {
	n = copy(values, r.page.values[r.offset:])
	r.offset += n
	if r.offset == len(r.page.values) {
		err = io.EOF
	}
	return n, err
}

func (r *int32PageValues) ReadValues(values []Value) (n int, err error) {
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

type int64PageValues struct {
	page   *int64Page
	offset int
}

func (r *int64PageValues) Read(b []byte) (n int, err error) {
	n, err = r.ReadInt64s(unsafecast.Slice[int64](b))
	return 8 * n, err
}

func (r *int64PageValues) ReadInt64s(values []int64) (n int, err error) {
	n = copy(values, r.page.values[r.offset:])
	r.offset += n
	if r.offset == len(r.page.values) {
		err = io.EOF
	}
	return n, err
}

func (r *int64PageValues) ReadValues(values []Value) (n int, err error) {
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

type floatPageValues struct {
	page   *floatPage
	offset int
}

func (r *floatPageValues) Read(b []byte) (n int, err error) {
	n, err = r.ReadFloats(unsafecast.Slice[float32](b))
	return 4 * n, err
}

func (r *floatPageValues) ReadFloats(values []float32) (n int, err error) {
	n = copy(values, r.page.values[r.offset:])
	r.offset += n
	if r.offset == len(r.page.values) {
		err = io.EOF
	}
	return n, err
}

func (r *floatPageValues) ReadValues(values []Value) (n int, err error) {
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

type byteArrayPageValues struct {
	page   *byteArrayPage
	offset int
}

func (r *byteArrayPageValues) Read(b []byte) (int, error) {
	_, n, err := r.readByteArrays(b)
	return n, err
}

func (r *byteArrayPageValues) ReadRequired(values []byte) (int, error) {
	return r.ReadByteArrays(values)
}

func (r *byteArrayPageValues) ReadByteArrays(values []byte) (int, error) {
	n, _, err := r.readByteArrays(values)
	return n, err
}

func (r *byteArrayPageValues) readByteArrays(values []byte) (c, n int, err error) {
	numValues := r.page.len()
	for r.offset < numValues {
		b := r.page.index(r.offset)
		k := plain.ByteArrayLengthSize + len(b)
		if k > (len(values) - n) {
			break
		}
		plain.PutByteArrayLength(values[n:], len(b))
		n += plain.ByteArrayLengthSize
		n += copy(values[n:], b)
		r.offset++
		c++
	}
	if r.offset == numValues {
		err = io.EOF
	} else if n == 0 && len(values) > 0 {
		err = io.ErrShortBuffer
	}
	return c, n, err
}

func (r *byteArrayPageValues) ReadValues(values []Value) (n int, err error) {
	numValues := r.page.len()
	for n < len(values) && r.offset < numValues {
		values[n] = r.page.makeValueBytes(r.page.index(r.offset))
		r.offset++
		n++
	}
	if r.offset == numValues {
		err = io.EOF
	}
	return n, err
}

type fixedLenByteArrayPageValues struct {
	page   *fixedLenByteArrayPage
	offset int
}

func (r *fixedLenByteArrayPageValues) Read(b []byte) (n int, err error) {
	n, err = r.ReadFixedLenByteArrays(b)
	return n * r.page.size, err
}

func (r *fixedLenByteArrayPageValues) ReadRequired(values []byte) (int, error) {
	return r.ReadFixedLenByteArrays(values)
}

func (r *fixedLenByteArrayPageValues) ReadFixedLenByteArrays(values []byte) (n int, err error) {
	n = copy(values, r.page.data[r.offset:]) / r.page.size
	r.offset += n * r.page.size
	if r.offset == len(r.page.data) {
		err = io.EOF
	} else if n == 0 && len(values) > 0 {
		err = io.ErrShortBuffer
	}
	return n, err
}

func (r *fixedLenByteArrayPageValues) ReadValues(values []Value) (n int, err error) {
	for n < len(values) && r.offset < len(r.page.data) {
		values[n] = r.page.makeValueBytes(r.page.data[r.offset : r.offset+r.page.size])
		r.offset += r.page.size
		n++
	}
	if r.offset == len(r.page.data) {
		err = io.EOF
	}
	return n, err
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

type uint64PageValues struct {
	page   *uint64Page
	offset int
}

func (r *uint64PageValues) Read(b []byte) (n int, err error) {
	n, err = r.ReadUint64s(unsafecast.Slice[uint64](b))
	return 8 * n, err
}

func (r *uint64PageValues) ReadUint64s(values []uint64) (n int, err error) {
	n = copy(values, r.page.values[r.offset:])
	r.offset += n
	if r.offset == len(r.page.values) {
		err = io.EOF
	}
	return n, err
}

func (r *uint64PageValues) ReadValues(values []Value) (n int, err error) {
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

type nullPageValues struct {
	column int
	remain int
}

func (r *nullPageValues) ReadValues(values []Value) (n int, err error) {
	columnIndex := ^int16(r.column)
	values = values[:min(r.remain, len(values))]
	for i := range values {
		values[i] = Value{columnIndex: columnIndex}
	}
	r.remain -= len(values)
	if r.remain == 0 {
		err = io.EOF
	}
	return len(values), err
}
