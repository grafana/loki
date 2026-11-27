package postings

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func int64Values(alloc *memory.Allocator, values ...*int64) columnar.Array {
	b := columnar.NewNumberBuilder[int64](alloc)
	for _, v := range values {
		if v == nil {
			b.AppendNull()
		} else {
			b.AppendValue(*v)
		}
	}
	return b.Build()
}

func binaryValues(alloc *memory.Allocator, values ...*string) columnar.Array {
	b := columnar.NewUTF8Builder(alloc)
	for _, v := range values {
		if v == nil {
			b.AppendNull()
		} else {
			b.AppendValue([]byte(*v))
		}
	}
	return b.Build()
}

func kindValue(v PostingKind) *int64 { n := int64(v); return &n }

func str(v string) *string { return &v }

var (
	kindColumn   = &Column{Type: ColumnTypeKind}
	pathColumn   = &Column{Type: ColumnTypeObjectPath}
	bitmapColumn = &Column{Type: ColumnTypeStreamIDBitmap}
	bloomColumn  = &Column{Type: ColumnTypeBloomFilter}
)

func TestCheckRecordBatch(t *testing.T) {
	t.Run("accepts arrays after the columns", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			binaryValues(alloc, str("/a")),
			int64Values(alloc, kindValue(KindLabel)),
		})

		require.NoError(t, checkRecordBatch([]*Column{pathColumn}, rb))
	})

	t.Run("accepts any array for a column of an unknown type", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			binaryValues(alloc, str("x")),
		})

		require.NoError(t, checkRecordBatch([]*Column{{Type: ColumnTypeInvalid}}, rb))
	})

	t.Run("returns an error when the batch has fewer arrays than columns", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			int64Values(alloc, kindValue(KindLabel)),
		})

		err := checkRecordBatch([]*Column{kindColumn, pathColumn}, rb)
		require.ErrorContains(t, err, "want at least 2")
	})

	t.Run("returns an error when an int64 column holds a binary array", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			binaryValues(alloc, str("1")),
		})

		err := checkRecordBatch([]*Column{kindColumn}, rb)
		require.ErrorContains(t, err, "invalid array type")
	})

	t.Run("returns an error when a binary column holds an int64 array", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			int64Values(alloc, kindValue(KindLabel)),
		})

		err := checkRecordBatch([]*Column{pathColumn}, rb)
		require.ErrorContains(t, err, "invalid array type")
	})

	t.Run("returns an error when an array length differs from the row count", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 2, []columnar.Array{
			int64Values(alloc, kindValue(KindLabel)),
		})

		err := checkRecordBatch([]*Column{kindColumn}, rb)
		require.ErrorContains(t, err, "has 1 elements, want 2")
	})
}

func TestDecodeRow(t *testing.T) {
	decodeAll := func(t *testing.T, columns []*Column, rb *columnar.RecordBatch) []Row {
		t.Helper()
		require.NoError(t, checkRecordBatch(columns, rb))
		var values rowValues
		values.reset(columns, rb)

		rows := make([]Row, rb.NumRows())
		for i := range rows {
			rows[i] = decodeRow(columns, rb, i, &values)
		}
		return rows
	}

	t.Run("decodes the columns and leaves the other fields zero", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 2, []columnar.Array{
			int64Values(alloc, kindValue(KindLabel), kindValue(KindBloom)),
			binaryValues(alloc, str("/a"), str("/b")),
			binaryValues(alloc, str("ab"), str("cd")),
			binaryValues(alloc, str("bloom-a"), str("bloom-b")),
		})

		got := decodeAll(t, []*Column{kindColumn, pathColumn, bitmapColumn, bloomColumn}, rb)
		require.Equal(t, []Row{
			{Kind: KindLabel, ObjectPath: "/a", StreamIDBitmap: []byte("ab"), BloomFilter: []byte("bloom-a")},
			{Kind: KindBloom, ObjectPath: "/b", StreamIDBitmap: []byte("cd"), BloomFilter: []byte("bloom-b")},
		}, got)
	})

	t.Run("decodes null values as zero values", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			int64Values(alloc, nil),
			binaryValues(alloc, nil),
			binaryValues(alloc, nil),
			binaryValues(alloc, nil),
		})

		got := decodeAll(t, []*Column{kindColumn, pathColumn, bitmapColumn, bloomColumn}, rb)
		require.Equal(t, []Row{{}}, got)
	})

	t.Run("ignores the arrays after the columns", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			binaryValues(alloc, str("/a")),
			int64Values(alloc, kindValue(KindLabel)),
		})

		got := decodeAll(t, []*Column{pathColumn}, rb)
		require.Equal(t, []Row{{ObjectPath: "/a"}}, got)
	})

	t.Run("returns rows that stay valid after the batch memory is reused", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			binaryValues(alloc, str("/a")),
			binaryValues(alloc, str("bitmap")),
			binaryValues(alloc, str("bloom")),
		})
		got := decodeAll(t, []*Column{pathColumn, bitmapColumn, bloomColumn}, rb)

		alloc.Reclaim()
		_ = binaryValues(alloc, str("/z"), str("XXXXXX"), str("XXXXX"))

		require.Equal(t, []Row{{ObjectPath: "/a", StreamIDBitmap: []byte("bitmap"), BloomFilter: []byte("bloom")}}, got)
	})
}

func TestRowValues(t *testing.T) {
	newValues := func(t *testing.T, columns []*Column, arrays ...columnar.Array) *rowValues {
		t.Helper()
		var values rowValues
		values.reset(columns, columnar.NewRecordBatch(nil, int64(arrays[0].Len()), arrays))
		return &values
	}

	t.Run("copies each column into a buffer of its own", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		values := newValues(t, []*Column{pathColumn, bloomColumn},
			binaryValues(alloc, str("/a")),
			binaryValues(alloc, str("bloom")),
		)

		path := values.string(0, []byte("/a"))
		bloom := values.bytes(1, []byte("bloom"))

		require.Same(t, &values.bufs[0][0], unsafe.StringData(path))
		require.Same(t, &values.bufs[1][0], unsafe.SliceData(bloom))
	})

	t.Run("allocates no buffer for an int64 column or an empty binary column", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		values := newValues(t, []*Column{kindColumn, pathColumn},
			int64Values(alloc, kindValue(KindLabel)),
			binaryValues(alloc, nil),
		)

		require.Equal(t, [][]byte{nil, nil}, values.bufs)
	})

	t.Run("returns values that stay valid when the buffer grows or is reset", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		values := newValues(t, []*Column{pathColumn, bitmapColumn},
			binaryValues(alloc, str("x")),
			binaryValues(alloc, str("y")),
		)

		first := values.string(0, []byte("first"))
		second := values.bytes(1, []byte("second"))
		values.reset([]*Column{pathColumn}, columnar.NewRecordBatch(nil, 1, []columnar.Array{binaryValues(alloc, str("third"))}))
		_ = values.string(0, []byte("third"))

		require.Equal(t, "first", first)
		require.Equal(t, []byte("second"), second)
	})

	t.Run("returns bytes that an append cannot use to overwrite the next value", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		values := newValues(t, []*Column{bitmapColumn}, binaryValues(alloc, str("ab"), str("cd")))

		first := values.bytes(0, []byte("ab"))
		second := values.bytes(0, []byte("cd"))
		_ = append(first, 'X')

		require.Equal(t, []byte("cd"), second)
	})

	t.Run("returns the empty string for an empty value", func(t *testing.T) {
		var values rowValues
		require.Empty(t, values.string(0, nil))
	})
}
