package log

import (
	"fmt"
	"testing"

	"github.com/apache/arrow/go/v14/arrow"
	"github.com/apache/arrow/go/v14/arrow/array"
	//"github.com/apache/arrow/go/v14/arrow/compute"
	"github.com/apache/arrow/go/v14/arrow/memory"
	memmem "github.com/jeschkies/go-memmem/pkg/search"
	"github.com/stretchr/testify/require"
)

func TestStringFilter(t *testing.T) {
	//ctx := context.Background()
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
		if i %3 == 0 {
			v = "bar"
		}

		b.Field(1).(*array.StringBuilder).Append(v)
	}
	batch := b.NewRecord()

	require.Equal(t, batch.NumRows(), int64(10))

	lines, ok := batch.Column(1).(*array.String)
	require.True(t, ok)
	Filter(lines, "foo")

	//filtered, err := compute.FilterRecordBatch(ctx, batch, f, nil)
	//require.NoError(t, err)
}

func Filter(data *array.String, needle string, pool *memory.GoAllocator) {

	fb := array.NewInt8Builder(pool)
	for _, offset := range data.ValueOffsets() {
		pos := memmem.Index(data.ValueBytes(), needle)
	}

}
