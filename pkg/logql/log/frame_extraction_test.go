package log

import (
	"context"
	"testing"

	"github.com/apache/arrow/go/v14/arrow"
	"github.com/apache/arrow/go/v14/arrow/array"

	"github.com/apache/arrow/go/v14/arrow/compute"
	"github.com/apache/arrow/go/v14/arrow/memory"
	memmem "github.com/jeschkies/go-memmem/pkg/search"
	"github.com/stretchr/testify/require"
)

func TestStringFilter(t *testing.T) {
	ctx := context.Background()
	//extractor := &frameSampleExtractor{}

	// test data
	pool := memory.NewGoAllocator()
	fields := []arrow.Field{
		{Name: "timestamp", Type: &arrow.TimestampType{Unit: arrow.Nanosecond}},
		{Name: "line", Type: &arrow.StringType{}},
	}
	schema := arrow.NewSchema(fields, &arrow.Metadata{})
	b := array.NewRecordBuilder(pool, schema)
	for i := 0; i < 10; i++ {
		b.Field(0).(*array.TimestampBuilder).Append(arrow.Timestamp(i))

		v := "foobar"
		if i%3 == 0 {
			v = "bar"
		}

		b.Field(1).(*array.StringBuilder).Append(v)
	}
	batch := b.NewRecord()

	require.Equal(t, int64(10), batch.NumRows())

	lines, ok := batch.Column(1).(*array.String)
	require.True(t, ok)
	f := Filter(lines, "won't find", pool)
	require.Equal(t, 10, f.Len())

	f = Filter(lines, "foo", pool)
	require.Equal(t, 10, f.Len())
	require.Equal(t, 4, f.NullN())

	filtered, err := compute.FilterRecordBatch(ctx, batch, f, compute.DefaultFilterOptions())
	require.NoError(t, err)
	require.Equal(t, int64(6), filtered.NumRows())
}

func Filter(data *array.String, needle string, pool *memory.GoAllocator) arrow.Array {

	fb := array.NewBooleanBuilder(pool)
	for i := 0; i < data.Len(); i++ {
		beg := data.ValueOffset64(i)
		end := data.ValueOffset64(i + 1)
		offset := memmem.Index(data.ValueBytes()[beg:], []byte(needle))

		// Nothing was found
		if offset == -1 {
			// Fill rest with nulls
			fb.AppendNulls(data.Len() - i)
			break
		}

		pos := beg + offset
		// Append nulls until offset is found
		for pos >= end {
			fb.AppendNull()
			beg = end
			i++
			end = data.ValueOffset64(i + 1)
		}

		fb.Append(true)
	}

	return fb.NewArray()
}
