package postings

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestNewRowBatch(t *testing.T) {
	int64Values := func(alloc *memory.Allocator, values ...*int64) columnar.Array {
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
	binaryValues := func(alloc *memory.Allocator, values ...*string) columnar.Array {
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
	kindValue := func(v PostingKind) *int64 { n := int64(v); return &n }
	str := func(v string) *string { return &v }

	kindColumn := &Column{Type: ColumnTypeKind}
	pathColumn := &Column{Type: ColumnTypeObjectPath}
	bitmapColumn := &Column{Type: ColumnTypeStreamIDBitmap}

	t.Run("decodes the projected columns and leaves the other fields zero", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 2, []columnar.Array{
			int64Values(alloc, kindValue(KindLabel), kindValue(KindBloom)),
			binaryValues(alloc, str("/a"), str("/b")),
		})

		batch, err := newRowBatch([]*Column{kindColumn, pathColumn}, rb)
		require.NoError(t, err)
		require.Equal(t, 2, batch.rows)
		require.Equal(t, Row{Kind: KindLabel, ObjectPath: "/a"}, batch.row(0))
		require.Equal(t, Row{Kind: KindBloom, ObjectPath: "/b"}, batch.row(1))
	})

	t.Run("decodes null values as zero values", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			int64Values(alloc, nil),
			binaryValues(alloc, nil),
			binaryValues(alloc, nil),
		})

		batch, err := newRowBatch([]*Column{kindColumn, pathColumn, bitmapColumn}, rb)
		require.NoError(t, err)
		require.Equal(t, Row{}, batch.row(0))
	})

	t.Run("returns rows that stay valid after the batch memory is reused", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			binaryValues(alloc, str("/a")),
			binaryValues(alloc, str("bitmap")),
		})
		batch, err := newRowBatch([]*Column{pathColumn, bitmapColumn}, rb)
		require.NoError(t, err)
		row := batch.row(0)

		alloc.Reclaim()
		_ = binaryValues(alloc, str("/z"), str("XXXXXX"), str("XXXXXX"))

		require.Equal(t, Row{ObjectPath: "/a", StreamIDBitmap: []byte("bitmap")}, row)
	})

	t.Run("returns bitmaps that an append cannot use to overwrite the next row", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 2, []columnar.Array{
			binaryValues(alloc, str("ab"), str("cd")),
		})
		batch, err := newRowBatch([]*Column{bitmapColumn}, rb)
		require.NoError(t, err)

		first := batch.row(0)
		_ = append(first.StreamIDBitmap, 'X')

		require.Equal(t, []byte("cd"), batch.row(1).StreamIDBitmap)
	})

	t.Run("decodes a sliced array whose offsets do not start at zero", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		paths := binaryValues(alloc, str("/skip"), str("/a"), nil, str("/b")).Slice(1, 4)
		bitmaps := binaryValues(alloc, str("skip"), str("x"), nil, str("yz")).Slice(1, 4)
		rb := columnar.NewRecordBatch(nil, 3, []columnar.Array{paths, bitmaps})

		batch, err := newRowBatch([]*Column{pathColumn, bitmapColumn}, rb)
		require.NoError(t, err)
		require.Equal(t, Row{ObjectPath: "/a", StreamIDBitmap: []byte("x")}, batch.row(0))
		require.Equal(t, Row{}, batch.row(1))
		require.Equal(t, Row{ObjectPath: "/b", StreamIDBitmap: []byte("yz")}, batch.row(2))
	})

	t.Run("ignores the arrays after the projected columns", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			binaryValues(alloc, str("/a")),
			int64Values(alloc, kindValue(KindLabel)),
		})

		batch, err := newRowBatch([]*Column{pathColumn}, rb)
		require.NoError(t, err)
		require.Equal(t, Row{ObjectPath: "/a"}, batch.row(0))
	})

	t.Run("returns an error when the batch has fewer arrays than columns", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			int64Values(alloc, kindValue(KindLabel)),
		})

		_, err := newRowBatch([]*Column{kindColumn, pathColumn}, rb)
		require.ErrorContains(t, err, "want at least 2")
	})

	t.Run("returns an error when an int64 column holds a binary array", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			binaryValues(alloc, str("1")),
		})

		_, err := newRowBatch([]*Column{kindColumn}, rb)
		require.ErrorContains(t, err, "invalid array type")
	})

	t.Run("returns an error when a binary column holds an int64 array", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			int64Values(alloc, kindValue(KindLabel)),
		})

		_, err := newRowBatch([]*Column{pathColumn}, rb)
		require.ErrorContains(t, err, "invalid array type")
	})

	t.Run("returns an error when the offsets fall outside the data", func(t *testing.T) {
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			columnar.NewUTF8([]byte("ab"), []int32{0, 5}, memory.Bitmap{}),
		})

		_, err := newRowBatch([]*Column{pathColumn}, rb)
		require.ErrorContains(t, err, "outside its 2 data bytes")
	})

	t.Run("returns an error when the bloom filter offsets fall outside the data", func(t *testing.T) {
		rb := columnar.NewRecordBatch(nil, 1, []columnar.Array{
			columnar.NewUTF8([]byte("ab"), []int32{0, 5}, memory.Bitmap{}),
		})

		_, err := newRowBatch([]*Column{{Type: ColumnTypeBloomFilter}}, rb)
		require.ErrorContains(t, err, "outside its 2 data bytes")
	})

	t.Run("returns an error when the offsets decrease", func(t *testing.T) {
		rb := columnar.NewRecordBatch(nil, 2, []columnar.Array{
			columnar.NewUTF8([]byte("abcd"), []int32{0, 3, 1}, memory.Bitmap{}),
		})

		_, err := newRowBatch([]*Column{bitmapColumn}, rb)
		require.ErrorContains(t, err, "below offset 3")
	})

	t.Run("returns an error when an array length differs from the row count", func(t *testing.T) {
		alloc := memory.NewAllocator(nil)
		rb := columnar.NewRecordBatch(nil, 2, []columnar.Array{
			int64Values(alloc, kindValue(KindLabel)),
		})

		_, err := newRowBatch([]*Column{kindColumn}, rb)
		require.ErrorContains(t, err, "has 1 elements, want 2")
	})
}
