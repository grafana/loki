package log

import (
	"context"
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
