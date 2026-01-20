package parquet

import (
	"bytes"
	"io"

	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/encoding/plain"
	"github.com/parquet-go/parquet-go/internal/memory"
)

type byteArrayPage struct {
	typ         Type
	values      memory.SliceBuffer[byte]
	offsets     memory.SliceBuffer[uint32]
	columnIndex int16
}

func newByteArrayPage(typ Type, columnIndex int16, numValues int32, values encoding.Values) *byteArrayPage {
	data, offsets := values.ByteArray()
	return &byteArrayPage{
		typ:         typ,
		values:      memory.SliceBufferFrom(data),
		offsets:     memory.SliceBufferFrom(offsets[:numValues+1]),
		columnIndex: ^columnIndex,
	}
}

func (page *byteArrayPage) Type() Type { return page.typ }

func (page *byteArrayPage) Column() int { return int(^page.columnIndex) }

func (page *byteArrayPage) Dictionary() Dictionary { return nil }

func (page *byteArrayPage) NumRows() int64 { return int64(page.len()) }

func (page *byteArrayPage) NumValues() int64 { return int64(page.len()) }

func (page *byteArrayPage) NumNulls() int64 { return 0 }

func (page *byteArrayPage) Size() int64 {
	return int64(page.values.Len()) + 4*int64(page.offsets.Len())
}

func (page *byteArrayPage) RepetitionLevels() []byte { return nil }

func (page *byteArrayPage) DefinitionLevels() []byte { return nil }

func (page *byteArrayPage) Data() encoding.Values {
	return encoding.ByteArrayValues(page.values.Slice(), page.offsets.Slice())
}

func (page *byteArrayPage) Values() ValueReader { return &byteArrayPageValues{page: page} }

func (page *byteArrayPage) len() int { return page.offsets.Len() - 1 }

func (page *byteArrayPage) index(i int) []byte {
	offsets := page.offsets.Slice()
	values := page.values.Slice()
	j := offsets[i+0]
	k := offsets[i+1]
	return values[j:k:k]
}

func (page *byteArrayPage) min() (min []byte) {
	if n := page.len(); n > 0 {
		min = page.index(0)

		for i := 1; i < n; i++ {
			v := page.index(i)

			if bytes.Compare(v, min) < 0 {
				min = v
			}
		}
	}
	return min
}

func (page *byteArrayPage) max() (max []byte) {
	if n := page.len(); n > 0 {
		max = page.index(0)

		for i := 1; i < n; i++ {
			v := page.index(i)

			if bytes.Compare(v, max) > 0 {
				max = v
			}
		}
	}
	return max
}

func (page *byteArrayPage) bounds() (min, max []byte) {
	if n := page.len(); n > 0 {
		min = page.index(0)
		max = min

		for i := 1; i < n; i++ {
			v := page.index(i)

			switch {
			case bytes.Compare(v, min) < 0:
				min = v
			case bytes.Compare(v, max) > 0:
				max = v
			}
		}
	}
	return min, max
}

func (page *byteArrayPage) Bounds() (min, max Value, ok bool) {
	if ok = page.offsets.Len() > 1; ok {
		minBytes, maxBytes := page.bounds()
		min = page.makeValueBytes(minBytes)
		max = page.makeValueBytes(maxBytes)
	}
	return min, max, ok
}

func (page *byteArrayPage) cloneValues() memory.SliceBuffer[byte] {
	return page.values.Clone()
}

func (page *byteArrayPage) cloneOffsets() memory.SliceBuffer[uint32] {
	return page.offsets.Clone()
}

func (page *byteArrayPage) Slice(i, j int64) Page {
	return &byteArrayPage{
		typ:         page.typ,
		values:      page.values,
		offsets:     memory.SliceBufferFrom(page.offsets.Slice()[i : j+1]),
		columnIndex: page.columnIndex,
	}
}

func (page *byteArrayPage) makeValueBytes(v []byte) Value {
	value := makeValueBytes(ByteArray, v)
	value.columnIndex = page.columnIndex
	return value
}

func (page *byteArrayPage) makeValueString(v string) Value {
	value := makeValueString(ByteArray, v)
	value.columnIndex = page.columnIndex
	return value
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
