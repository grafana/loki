package log

import (
	"context"
	"strconv"
	"testing"

	"github.com/apache/arrow/go/v14/arrow"
	"github.com/apache/arrow/go/v14/arrow/array"
	"github.com/apache/arrow/go/v14/arrow/memory"
	"github.com/stretchr/testify/require"
)

func createTestBatch(pool *memory.GoAllocator) arrow.Record {
	fields := []arrow.Field{
		{Name: "timestamp", Type: &arrow.TimestampType{Unit: arrow.Nanosecond}},
		{Name: "line", Type: &arrow.StringType{}},
		{Name: "labels", Type: arrow.MapOf(&arrow.StringType{}, &arrow.StringType{})},
		{Name: "out", Type: &arrow.Float64Type{}},
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

		l := b.Field(2).(*array.MapBuilder)
		l.Append(true)
		l.KeyBuilder().(*array.StringBuilder).Append("first")
		l.ItemBuilder().(*array.StringBuilder).Append("label")

		l.KeyBuilder().(*array.StringBuilder).Append("value")
		l.ItemBuilder().(*array.StringBuilder).Append(strconv.Itoa(i))

		b.Field(3).(*array.Float64Builder).AppendNull()
	}

	return b.NewRecord()
}

func TestContainsFilterStage(t *testing.T) {
	ctx := context.Background()

	pool := memory.NewGoAllocator()
	batch := createTestBatch(pool)
	defer batch.Release()

	require.Equal(t, int64(10), batch.NumRows())

	stage := containsFilterBatchStage{
		column: 1,
		needle: []byte("foo"),
		fb:     array.NewBooleanBuilder(pool),
	}
	filtered, err := stage.Process(ctx, batch)

	require.NoError(t, err)
	require.Equal(t, int64(6), filtered.NumRows())
}

func TestUnwrapStage(t *testing.T) {
	ctx := context.Background()

	pool := memory.NewGoAllocator()
	batch := createTestBatch(pool)
	defer batch.Release()

	stage := unwrapBatchStage{
		in:    2,
		out:   3,
		label: "value",
		fb:    array.NewFloat64Builder(pool),
	}
	updated, err := stage.Process(ctx, batch)
	require.NoError(t, err)

	require.Equal(t, "[0 1 2 3 4 5 6 7 8 9]", updated.Column(3).String())
}

func TestBatchExtractor(t *testing.T) {
	ctx := context.Background()

	pool := memory.NewGoAllocator()
	batch := createTestBatch(pool)
	defer batch.Release()

	extractor := &batchSampleExtractor{
		stages: []BatchStage{
			&containsFilterBatchStage{
				column: 1,
				needle: []byte("foo"),
				fb:     array.NewBooleanBuilder(pool),
			},
		},
	}
	filtered, err := extractor.Process(ctx, batch)
	require.NoError(t, err)
	require.Equal(t, int64(6), filtered.NumRows())
}

func BenchmarkContainsFilterStage(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ctx := context.Background()

		pool := memory.NewGoAllocator()
		batch := createTestBatch(pool)
		defer batch.Release()

		require.Equal(b, int64(10), batch.NumRows())

		stage := containsFilterBatchStage{
			column: 1,
			needle: []byte("foo"),
			fb:     array.NewBooleanBuilder(pool),
		}
		filtered, err := stage.Process(ctx, batch)

		require.NoError(b, err)
		require.Equal(b, int64(6), filtered.NumRows())
	}
}

func BenchmarkUnwrapStage(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ctx := context.Background()

		pool := memory.NewGoAllocator()
		batch := createTestBatch(pool)
		defer batch.Release()

		stage := unwrapBatchStage{
			in:    2,
			out:   3,
			label: "value",
			fb:    array.NewFloat64Builder(pool),
		}
		updated, err := stage.Process(ctx, batch)
		require.NoError(b, err)

		require.Equal(b, "[0 1 2 3 4 5 6 7 8 9]", updated.Column(3).String())
	}
}

func BenchmarkBatchExtractor(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ctx := context.Background()

		pool := memory.NewGoAllocator()
		batch := createTestBatch(pool)
		defer batch.Release()

		extractor := &batchSampleExtractor{
			stages: []BatchStage{
				&containsFilterBatchStage{
					column: 1,
					needle: []byte("foo"),
					fb:     array.NewBooleanBuilder(pool),
				},
			},
		}
		filtered, err := extractor.Process(ctx, batch)
		require.NoError(b, err)
		require.Equal(b, int64(6), filtered.NumRows())
	}
}
