package worker

import (
	"context"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// mockRecordSink records each Send call and total rows for testing.
type mockRecordSink struct {
	mu        sync.Mutex
	SendCount int
	TotalRows int64
}

func (m *mockRecordSink) Send(_ context.Context, rec arrow.RecordBatch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SendCount++
	m.TotalRows += rec.NumRows()
	return nil
}

func TestThread_drainPipeline(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "b", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	makeRecord := func(rows arrowtest.Rows) arrow.RecordBatch {
		return rows.Record(memory.DefaultAllocator, schema)
	}

	tests := []struct {
		name                 string
		recordBatchSizeBytes int
		numRecords           int
		wantTotalRows        int
		checkSends           func(t *testing.T, sendCount int, totalRows int64)
	}{
		{
			name:                 "recordBatchSizeBytes=0 sink receives one send per record",
			recordBatchSizeBytes: 0,
			numRecords:           3,
			wantTotalRows:        3,
			checkSends: func(t *testing.T, sendCount int, totalRows int64) {
				require.Equal(t, 3, sendCount, "sink should receive one send per record when batching is disabled")
				require.Equal(t, int64(3), totalRows)
			},
		},
		{
			name:                 "recordBatchSizeBytes fits exactly two 1-row records per batch",
			recordBatchSizeBytes: 44, // 2 * size of one 1-row record (22 bytes) with this schema
			numRecords:           5,
			wantTotalRows:        5,
			checkSends: func(t *testing.T, sendCount int, totalRows int64) {
				require.Equal(t, 3, sendCount, "5 records with 2 per batch => 2+2+1 sends")
				require.Equal(t, int64(5), totalRows, "all rows must be received regardless of batching")
			},
		},
		{
			name:                 "record larger than recordBatchSizeBytes is written alone",
			recordBatchSizeBytes: 10, // smaller than one 1-row record (22 bytes)
			numRecords:           3,
			wantTotalRows:        3,
			checkSends: func(t *testing.T, sendCount int, totalRows int64) {
				require.Equal(t, 3, sendCount, "each record exceeds batch limit so each is sent alone")
				require.Equal(t, int64(3), totalRows)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			records := make([]arrow.RecordBatch, tt.numRecords)
			for i := 0; i < tt.numRecords; i++ {
				records[i] = makeRecord(arrowtest.Rows{map[string]any{"a": int64(i + 1), "b": "x"}})
			}
			pipeline := executor.NewBufferedPipeline(records...)
			defer pipeline.Close()

			sink := &mockRecordSink{}
			th := &thread{Logger: log.NewNopLogger()}
			totalRows, err := th.drainPipeline(ctx, pipeline, []RecordSink{sink}, tt.recordBatchSizeBytes, log.NewNopLogger())
			require.NoError(t, err)
			require.Equal(t, tt.wantTotalRows, totalRows)

			sink.mu.Lock()
			sendCount := sink.SendCount
			totalRowsReceived := sink.TotalRows
			sink.mu.Unlock()
			tt.checkSends(t, sendCount, totalRowsReceived)
		})
	}
}
