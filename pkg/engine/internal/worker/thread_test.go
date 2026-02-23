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
// RecordedFieldNames is the list of field names (in order) for each received record.
type mockRecordSink struct {
	mu                 sync.Mutex
	SendCount          int
	TotalRows          int64
	RecordedFieldNames [][]string
}

func (m *mockRecordSink) Send(_ context.Context, rec arrow.RecordBatch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SendCount++
	m.TotalRows += rec.NumRows()
	if rec != nil && rec.Schema() != nil {
		n := rec.Schema().NumFields()
		names := make([]string, n)
		for i := 0; i < n; i++ {
			names[i] = rec.Schema().Field(i).Name
		}
		m.RecordedFieldNames = append(m.RecordedFieldNames, names)
	}
	return nil
}

// recordInput pairs a schema with rows to build one record for pipeline tests.
type recordInput struct {
	Schema *arrow.Schema
	Rows   arrowtest.Rows
}

func TestThread_drainPipeline(t *testing.T) {
	schemaAB := arrow.NewSchema([]arrow.Field{
		{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "b", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	tests := []struct {
		name             string
		batchSizeRecords int64
		inputs           []recordInput
		wantTotalRows    int
		checkSends       func(t *testing.T, sendCount int, totalRows int64)
		checkFieldNames  func(t *testing.T, fieldNames [][]string)
	}{
		{
			name:             "batchSizeRecords=0 sink receives one send per record",
			batchSizeRecords: 0,
			inputs: []recordInput{
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(1), "b": "x"}}},
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(2), "b": "x"}}},
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(3), "b": "x"}}},
			},
			wantTotalRows: 3,
			checkSends: func(t *testing.T, sendCount int, totalRows int64) {
				require.Equal(t, 3, sendCount, "sink should receive one send per record when batching is disabled")
				require.Equal(t, int64(3), totalRows)
			},
		},
		{
			name:             "batchSizeRecords=2 fits exactly two records per batch",
			batchSizeRecords: 2,
			inputs: []recordInput{
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(1), "b": "x"}}},
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(2), "b": "x"}}},
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(3), "b": "x"}}},
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(4), "b": "x"}}},
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(5), "b": "x"}}},
			},
			wantTotalRows: 5,
			checkSends: func(t *testing.T, sendCount int, totalRows int64) {
				require.Equal(t, 3, sendCount, "5 records with 2 per batch => 2+2+1 sends")
				require.Equal(t, int64(5), totalRows, "all rows must be received regardless of batching")
			},
		},
		{
			name:             "batchSizeRecords=1 each record sent alone",
			batchSizeRecords: 1,
			inputs: []recordInput{
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(1), "b": "x"}}},
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(2), "b": "x"}}},
				{schemaAB, arrowtest.Rows{map[string]any{"a": int64(3), "b": "x"}}},
			},
			wantTotalRows: 3,
			checkSends: func(t *testing.T, sendCount int, totalRows int64) {
				require.Equal(t, 3, sendCount, "each record in its own batch so each is sent alone")
				require.Equal(t, int64(3), totalRows)
			},
		},
		{
			name:             "records with different schemas are batched with union schema",
			batchSizeRecords: 10, // large enough to batch both records
			inputs: []recordInput{
				{
					arrow.NewSchema([]arrow.Field{
						{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
						{Name: "b", Type: arrow.BinaryTypes.String, Nullable: true},
					}, nil),
					arrowtest.Rows{map[string]any{"a": int64(1), "b": "x"}},
				},
				{
					arrow.NewSchema([]arrow.Field{
						{Name: "b", Type: arrow.BinaryTypes.String, Nullable: true},
						{Name: "c", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
					}, nil),
					arrowtest.Rows{map[string]any{"b": "y", "c": int64(2)}},
				},
			},
			wantTotalRows: 2,
			checkSends: func(t *testing.T, sendCount int, totalRows int64) {
				require.Equal(t, 1, sendCount, "one batched send with schema reconciliation")
				require.Equal(t, int64(2), totalRows)
			},
			checkFieldNames: func(t *testing.T, fieldNames [][]string) {
				require.Len(t, fieldNames, 1, "one record received")
				require.Equal(t, []string{"a", "b", "c"}, fieldNames[0], "union schema order first seen: a,b from first record then c from second")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			records := make([]arrow.RecordBatch, len(tt.inputs))
			for i, in := range tt.inputs {
				records[i] = in.Rows.Record(memory.DefaultAllocator, in.Schema)
			}
			pipeline := executor.NewBufferedPipeline(records...)
			defer pipeline.Close()

			sink := &mockRecordSink{}
			th := &thread{Logger: log.NewNopLogger()}
			totalRows, err := th.drainPipeline(ctx, pipeline, []recordSink{sink}, tt.batchSizeRecords, log.NewNopLogger())
			require.NoError(t, err)
			require.Equal(t, tt.wantTotalRows, totalRows)

			sink.mu.Lock()
			sendCount := sink.SendCount
			totalRowsReceived := sink.TotalRows
			fieldNames := append([][]string(nil), sink.RecordedFieldNames...)
			sink.mu.Unlock()

			tt.checkSends(t, sendCount, totalRowsReceived)
			if tt.checkFieldNames != nil {
				tt.checkFieldNames(t, fieldNames)
			}
		})
	}
}
