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
	alloc := memory.MakeAllocator(nil)
	srcInt64 := []int64{1, 2, 3, 4, 0, 0, 0, 100, 500}
	validity := memory.MakeBitmap(alloc, len(srcInt64))
	validity.Resize(len(srcInt64))
	validity.SetRange(0, len(srcInt64), true)
	validity.Set(4, false)
	validity.Set(5, false)
	int64Arr := columnar.MakeInt64(srcInt64, validity)
	src := columnar.NewRecordBatch(int64(len(srcInt64)), []columnar.Array{int64Arr})

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
	alloc := memory.MakeAllocator(nil)
	srcUint64 := []uint64{1, 2, 3, 4, 0, 0, 0, 100, 500}
	validity := memory.MakeBitmap(alloc, len(srcUint64))
	validity.Resize(len(srcUint64))
	validity.SetRange(0, len(srcUint64), true)
	validity.Set(4, false)
	validity.Set(5, false)
	uint64Arr := columnar.MakeUint64(srcUint64, validity)
	src := columnar.NewRecordBatch(int64(len(srcUint64)), []columnar.Array{uint64Arr})

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
	alloc := memory.MakeAllocator(nil)
	srcStrings := []string{"a", "b", "", "cd"}

	validity := memory.MakeBitmap(alloc, len(srcStrings))
	validity.Resize(len(srcStrings))
	validity.SetRange(0, len(srcStrings), true)
	validity.Set(2, false)

	utf8Arr := columnar.MakeUTF8([]byte(strings.Join(srcStrings, "")), []int32{0, 1, 2, 2, 4}, validity)
	src := columnar.NewRecordBatch(int64(len(srcStrings)), []columnar.Array{utf8Arr})

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

func TestToRecordBatch_timestamp(t *testing.T) {
	alloc := memory.MakeAllocator(nil)
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

	validity := memory.MakeBitmap(alloc, len(srcTimestamps))
	validity.Resize(len(srcTimestamps))
	validity.SetRange(0, len(srcTimestamps), true)
	validity.Set(2, false)
	int64Arr := columnar.MakeInt64(srcNanos, validity)
	src := columnar.NewRecordBatch(int64(len(srcTimestamps)), []columnar.Array{int64Arr})

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
