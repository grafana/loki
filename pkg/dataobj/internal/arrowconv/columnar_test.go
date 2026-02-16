package arrowconv_test

import (
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/arrowconv"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestToRecordBatch_int64(t *testing.T) {
	alloc := memory.NewAllocator(nil)
	srcInt64 := []int64{1, 2, 3, 4, 0, 0, 0, 100, 500}
	validity := memory.NewBitmap(alloc, len(srcInt64))
	validity.Resize(len(srcInt64))
	validity.SetRange(0, len(srcInt64), true)
	validity.Set(4, false)
	validity.Set(5, false)
	int64Arr := columnar.NewNumber[int64](srcInt64, validity)
	src := columnar.NewRecordBatch(nil, int64(len(srcInt64)), []columnar.Array{int64Arr})

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "myint64", Type: arrow.PrimitiveTypes.Int64},
	}, nil)

	res, err := arrowconv.ToRecordBatch(src, schema)

	require.NoError(t, err)
	col := res.Column(0).(*array.Int64)
	require.Equal(t, srcInt64, col.Values())
	require.True(t, col.IsNull(4))
	require.True(t, col.IsNull(5))
	require.False(t, col.IsNull(0))
}

func TestToRecordBatch_uint64(t *testing.T) {
	alloc := memory.NewAllocator(nil)
	srcUint64 := []uint64{1, 2, 3, 4, 0, 0, 0, 100, 500}
	validity := memory.NewBitmap(alloc, len(srcUint64))
	validity.Resize(len(srcUint64))
	validity.SetRange(0, len(srcUint64), true)
	validity.Set(4, false)
	validity.Set(5, false)
	uint64Arr := columnar.NewNumber[uint64](srcUint64, validity)
	src := columnar.NewRecordBatch(nil, int64(len(srcUint64)), []columnar.Array{uint64Arr})

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "myuint64", Type: arrow.PrimitiveTypes.Uint64},
	}, nil)

	res, err := arrowconv.ToRecordBatch(src, schema)

	require.NoError(t, err)
	col := res.Column(0).(*array.Uint64)
	require.Equal(t, srcUint64, col.Values())
	require.True(t, col.IsNull(4))
	require.True(t, col.IsNull(5))
	require.False(t, col.IsNull(0))
}

func TestToRecordBatch_string(t *testing.T) {
	alloc := memory.NewAllocator(nil)
	srcStrings := []string{"a", "b", "", "cd"}

	validity := memory.NewBitmap(alloc, len(srcStrings))
	validity.Resize(len(srcStrings))
	validity.SetRange(0, len(srcStrings), true)
	validity.Set(2, false)

	utf8Arr := columnar.NewUTF8([]byte(strings.Join(srcStrings, "")), []int32{0, 1, 2, 2, 4}, validity)
	src := columnar.NewRecordBatch(nil, int64(len(srcStrings)), []columnar.Array{utf8Arr})

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "mystring", Type: arrow.BinaryTypes.String},
	}, nil)
	res, err := arrowconv.ToRecordBatch(src, schema)

	require.NoError(t, err)
	col := res.Column(0).(*array.String)
	for i, expectedStr := range srcStrings {
		if !validity.Get(i) {
			require.True(t, col.IsNull(i))
			continue
		}
		require.False(t, col.IsNull(i))
		actualStr := col.Value(i)
		require.Equal(t, expectedStr, actualStr)
	}
}

func TestToRecordBatch_binary(t *testing.T) {
	alloc := memory.NewAllocator(nil)
	srcValues := [][]byte{
		{0x00, 0x01},
		nil,
		{0xff},
	}

	validity := memory.NewBitmap(alloc, len(srcValues))
	validity.Resize(len(srcValues))
	validity.SetRange(0, len(srcValues), true)
	validity.Set(1, false)

	data := []byte{0x00, 0x01, 0xff}
	offsets := []int32{0, 2, 2, 3}
	utf8Arr := columnar.NewUTF8(data, offsets, validity)
	src := columnar.NewRecordBatch(nil, int64(len(srcValues)), []columnar.Array{utf8Arr})

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "mybinary", Type: arrow.BinaryTypes.Binary},
	}, nil)
	res, err := arrowconv.ToRecordBatch(src, schema)

	require.NoError(t, err)
	col := res.Column(0).(*array.Binary)
	require.False(t, col.IsNull(0))
	require.True(t, col.IsNull(1))
	require.False(t, col.IsNull(2))
	require.Equal(t, srcValues[0], col.Value(0))
	require.Equal(t, srcValues[2], col.Value(2))
}

func TestToRecordBatch_timestamp(t *testing.T) {
	alloc := memory.NewAllocator(nil)
	now := time.Now().UTC()
	srcTimestamps := []time.Time{
		now,
		now.Add(time.Hour),
		{},
		now.Add(time.Hour * 2),
	}
	srcNanos := make([]int64, 0, len(srcTimestamps))
	for _, t := range srcTimestamps {
		srcNanos = append(srcNanos, t.UnixNano())
	}

	validity := memory.NewBitmap(alloc, len(srcTimestamps))
	validity.Resize(len(srcTimestamps))
	validity.SetRange(0, len(srcTimestamps), true)
	validity.Set(2, false)
	int64Arr := columnar.NewNumber[int64](srcNanos, validity)
	src := columnar.NewRecordBatch(nil, int64(len(srcTimestamps)), []columnar.Array{int64Arr})

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "mytimestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
	}, nil)
	res, err := arrowconv.ToRecordBatch(src, schema)

	require.NoError(t, err)
	require.Equal(t, len(srcTimestamps), int(res.NumRows()))
	col := res.Column(0).(*array.Timestamp)
	for i, expectedTime := range srcTimestamps {
		if !validity.Get(i) {
			require.True(t, col.IsNull(i))
			continue
		}
		actualTime := col.Value(i).ToTime(arrow.Nanosecond)
		require.Equal(t, expectedTime, actualTime, "item %d", i)
	}
}

func BenchmarkToRecordBatch_int64(b *testing.B) {
	src, schema := makeInt64BenchmarkBatch(b, benchmarkRows)
	benchmarkToRecordBatch(b, src, schema)
}

func BenchmarkToRecordBatch_uint64(b *testing.B) {
	src, schema := makeUint64BenchmarkBatch(b, benchmarkRows)
	benchmarkToRecordBatch(b, src, schema)
}

func BenchmarkToRecordBatch_string(b *testing.B) {
	src, schema := makeStringBenchmarkBatch(b, benchmarkRows)
	benchmarkToRecordBatch(b, src, schema)
}

func BenchmarkToRecordBatch_timestamp(b *testing.B) {
	src, schema := makeTimestampBenchmarkBatch(b, benchmarkRows)
	benchmarkToRecordBatch(b, src, schema)
}

func BenchmarkToRecordBatch_binary(b *testing.B) {
	src, schema := makeBinaryBenchmarkBatch(b, benchmarkRows)
	benchmarkToRecordBatch(b, src, schema)
}

const benchmarkRows = 4096

func benchmarkToRecordBatch(b *testing.B, src *columnar.RecordBatch, schema *arrow.Schema) {
	b.Helper()
	b.ReportAllocs()

	for b.Loop() {
		res, err := arrowconv.ToRecordBatch(src, schema)
		if err != nil {
			b.Fatal(err)
		}
		res.Release()
	}
}

func makeInt64BenchmarkBatch(b *testing.B, n int) (*columnar.RecordBatch, *arrow.Schema) {
	b.Helper()
	alloc := memory.NewAllocator(nil)
	values := make([]int64, n)
	for i := range values {
		values[i] = int64(i * 3)
	}
	validity := makeValidity(alloc, n, 10)
	int64Arr := columnar.NewNumber[int64](values, validity)
	src := columnar.NewRecordBatch(nil, int64(n), []columnar.Array{int64Arr})
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "myint64", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	return src, schema
}

func makeUint64BenchmarkBatch(b *testing.B, n int) (*columnar.RecordBatch, *arrow.Schema) {
	b.Helper()
	alloc := memory.NewAllocator(nil)
	values := make([]uint64, n)
	for i := range values {
		values[i] = uint64(i * 7)
	}
	validity := makeValidity(alloc, n, 10)
	uint64Arr := columnar.NewNumber[uint64](values, validity)
	src := columnar.NewRecordBatch(nil, int64(n), []columnar.Array{uint64Arr})
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "myuint64", Type: arrow.PrimitiveTypes.Uint64},
	}, nil)
	return src, schema
}

func makeStringBenchmarkBatch(b *testing.B, n int) (*columnar.RecordBatch, *arrow.Schema) {
	b.Helper()
	alloc := memory.NewAllocator(nil)
	validity := makeValidity(alloc, n, 10)

	data := make([]byte, 0, n*8)
	offsets := make([]int32, 0, n+1)
	offsets = append(offsets, 0)
	for i := 0; i < n; i++ {
		if !validity.Get(i) {
			offsets = append(offsets, int32(len(data)))
			continue
		}
		valLen := (i % 32) + 1
		for j := 0; j < valLen; j++ {
			data = append(data, byte('a'+(i%26)))
		}
		offsets = append(offsets, int32(len(data)))
	}

	utf8Arr := columnar.NewUTF8(data, offsets, validity)
	src := columnar.NewRecordBatch(nil, int64(n), []columnar.Array{utf8Arr})
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "mystring", Type: arrow.BinaryTypes.String},
	}, nil)
	return src, schema
}

func makeBinaryBenchmarkBatch(b *testing.B, n int) (*columnar.RecordBatch, *arrow.Schema) {
	b.Helper()

	batch, _ := makeStringBenchmarkBatch(b, n)
	return batch, arrow.NewSchema([]arrow.Field{
		{Name: "mybinary", Type: arrow.BinaryTypes.Binary},
	}, nil)
}

func makeTimestampBenchmarkBatch(b *testing.B, n int) (*columnar.RecordBatch, *arrow.Schema) {
	b.Helper()
	alloc := memory.NewAllocator(nil)
	values := make([]int64, n)
	start := time.Now().UTC()
	for i := range values {
		values[i] = start.Add(time.Duration(i) * time.Millisecond).UnixNano()
	}
	validity := makeValidity(alloc, n, 10)
	int64Arr := columnar.NewNumber[int64](values, validity)
	src := columnar.NewRecordBatch(nil, int64(n), []columnar.Array{int64Arr})
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "mytimestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
	}, nil)
	return src, schema
}

func makeValidity(alloc *memory.Allocator, n int, nullEvery int) memory.Bitmap {
	validity := memory.NewBitmap(alloc, n)
	validity.Resize(n)
	validity.SetRange(0, n, true)
	if nullEvery > 0 {
		for i := nullEvery - 1; i < n; i += nullEvery {
			validity.Set(i, false)
		}
	}
	return validity
}
