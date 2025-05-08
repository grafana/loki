package executor

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

func TestExecuteLimit(t *testing.T) {
	// Simple test to verify the Context.executeLimit method
	ctx := context.Background()
	c := &Context{}

	t.Run("with no inputs", func(t *testing.T) {
		pipeline := c.executeLimit(ctx, &physical.Limit{}, nil)
		err := pipeline.Read()
		require.Equal(t, EOF, err)
	})

	t.Run("with multiple inputs", func(t *testing.T) {
		pipeline := c.executeLimit(ctx, &physical.Limit{}, []Pipeline{emptyPipeline(), emptyPipeline()})
		err := pipeline.Read()
		require.Error(t, err)
		require.Contains(t, err.Error(), "limit expects exactly one input, got 2")
	})

	t.Run("with valid input", func(t *testing.T) {
		// Create test data
		fields := []arrow.Field{
			{Name: "name", Type: datatype.Arrow.String},
		}
		csvData := "Alice\nBob\nCharlie"
		record, err := CSVToArrow(fields, csvData)
		require.NoError(t, err)
		defer record.Release()

		record.Retain()
		source := NewBufferedPipeline(record)

		// Execute limit with skip=1 and fetch=1
		limit := &physical.Limit{Skip: 1, Fetch: 1}
		pipeline := c.executeLimit(ctx, limit, []Pipeline{source})
		defer pipeline.Close()

		// Read from the pipeline
		err = pipeline.Read()
		require.NoError(t, err)

		batch, err := pipeline.Value()
		require.NoError(t, err)
		require.NotNil(t, batch)

		// Should have returned only the second row
		require.Equal(t, int64(1), batch.NumRows())
		require.Equal(t, int64(1), batch.NumCols())
		require.Equal(t, "name", batch.ColumnName(0))
		require.Equal(t, "Bob", batch.Column(0).ValueStr(0))

		// Next read should return EOF
		err = pipeline.Read()
		require.Equal(t, EOF, err)
	})

	// Test with desired rows split across 2 batches
	t.Run("with rows split across batches", func(t *testing.T) {
		// Create schema with a single string column
		fields := []arrow.Field{
			{Name: "letter", Type: datatype.Arrow.String},
		}

		// Create first batch with letters A-C
		batch1Data := "A\nB\nC"
		batch1, err := CSVToArrow(fields, batch1Data)
		require.NoError(t, err)
		defer batch1.Release()

		// Create second batch with letters D-F
		batch2Data := "D\nE\nF"
		batch2, err := CSVToArrow(fields, batch2Data)
		require.NoError(t, err)
		defer batch2.Release()

		// Create source pipeline with both batches
		batch1.Retain()
		batch2.Retain()
		source := NewBufferedPipeline(batch1, batch2)

		// Create limit pipeline that skips 2 and fetches 3
		// Should return C from batch1, and D,E from batch2
		limit := &physical.Limit{Skip: 2, Fetch: 3}
		pipeline := c.executeLimit(ctx, limit, []Pipeline{source})
		defer pipeline.Close()

		// First read should give us C from batch1
		expectedFields := []arrow.Field{
			{Name: "letter", Type: datatype.Arrow.String},
		}

		expectedData := "C\nD\nE"
		expectedRecord, err := CSVToArrow(expectedFields, expectedData)
		require.NoError(t, err)
		defer expectedRecord.Release()

		expectedPipeline := NewBufferedPipeline(expectedRecord)
		defer expectedPipeline.Close()

		AssertPipelinesEqual(t, pipeline, expectedPipeline)
	})
}

func TestLimitPipeline_Skip_Fetch(t *testing.T) {
	// Create test pipeline with known data
	fields := []arrow.Field{
		{Name: "id", Type: datatype.Arrow.Integer},
	}

	// Create a pipeline with numbers 1-10
	var data string
	for i := 1; i <= 10; i++ {
		data += string(rune('0'+i)) + "\n"
	}

	record, err := CSVToArrow(fields, data)
	require.NoError(t, err)
	defer record.Release()

	record.Retain()
	source := NewBufferedPipeline(record)

	// Test with skip=3, fetch=4 (should return 4,5,6,7)
	limit := NewLimitPipeline(source, 3, 4)
	defer limit.Close()

	// Test first read
	err = limit.Read()
	require.NoError(t, err)

	batch, err := limit.Value()
	require.NoError(t, err)
	require.NotNil(t, batch)
	require.Equal(t, int64(4), batch.NumRows())

	// Check values to ensure we got the right rows
	for i := 0; i < 4; i++ {
		expectedVal := string(rune('0' + 4 + i))
		actualVal := batch.Column(0).ValueStr(i)
		require.Equal(t, expectedVal, actualVal)
	}

	// Next read should be EOF
	err = limit.Read()
	require.Equal(t, EOF, err)
}

func TestLimitPipeline_MultipleBatches(t *testing.T) {
	// Create test pipeline with multiple batches
	fields := []arrow.Field{
		{Name: "id", Type: datatype.Arrow.Integer},
	}

	// First batch: 1-5
	data1 := "1\n2\n3\n4\n5\n"
	record1, err := CSVToArrow(fields, data1)
	require.NoError(t, err)
	defer record1.Release()

	// Second batch: 6-10
	data2 := "6\n7\n8\n9\n10\n"
	record2, err := CSVToArrow(fields, data2)
	require.NoError(t, err)
	defer record2.Release()

	record1.Retain()
	record2.Retain()
	source := NewBufferedPipeline(record1, record2)

	// Test with skip=3, fetch=5 (should return 4,5,6,7,8)
	limit := NewLimitPipeline(source, 3, 5)
	defer limit.Close()

	expectedFields := []arrow.Field{
		{Name: "id", Type: datatype.Arrow.Integer},
	}

	expectedData := "4\n5\n6\n7\n8\n"
	expectedRecord, err := CSVToArrow(expectedFields, expectedData)
	require.NoError(t, err)
	defer expectedRecord.Release()

	expectedPipeline := NewBufferedPipeline(expectedRecord)
	defer expectedPipeline.Close()

	AssertPipelinesEqual(t, limit, expectedPipeline)
}

func TestLimit(t *testing.T) {
	for _, tt := range []struct {
		name            string
		offset          uint32
		limit           uint32
		batchSize       int64
		expectedBatches int64
		expectedRows    int64
	}{
		{
			name:            "without offset",
			offset:          0,
			limit:           5,
			batchSize:       3,
			expectedBatches: 2,
			expectedRows:    5,
		},
		{
			name:            "with offset",
			offset:          3,
			limit:           5,
			batchSize:       4,
			expectedBatches: 2,
			expectedRows:    5,
		},
		{
			name:            "with offset greater than batch size",
			offset:          5,
			limit:           6,
			batchSize:       2,
			expectedBatches: 4,
			expectedRows:    6,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := &Context{
				batchSize: tt.batchSize,
			}
			limit := &physical.Limit{
				Skip:  tt.offset,
				Fetch: tt.limit,
			}
			inputs := []Pipeline{
				incrementingIntPipeline.Pipeline(tt.batchSize, 1000),
			}

			pipeline := c.executeLimit(context.Background(), limit, inputs)
			batches, rows := collect(t, pipeline)

			require.Equal(t, tt.expectedBatches, batches)
			require.Equal(t, tt.expectedRows, rows)
		})
	}
}
