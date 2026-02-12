package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestRecordBatchSizeBytes(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "b", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	tests := []struct {
		name     string
		rows     *arrowtest.Rows
		wantSize int
	}{
		{name: "nil returns 0", rows: nil, wantSize: 0},
		{
			name:     "one row",
			rows:     &arrowtest.Rows{map[string]any{"a": int64(1), "b": "x"}},
			wantSize: 22,
		},
		{
			name: "two rows",
			rows: &arrowtest.Rows{
				map[string]any{"a": int64(1), "b": "x"},
				map[string]any{"a": int64(2), "b": "y"},
			},
			wantSize: 35,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rec arrow.RecordBatch
			if tt.rows != nil {
				rec = tt.rows.Record(memory.DefaultAllocator, schema)
				defer rec.Release()
			}
			size := RecordBatchSizeBytes(rec)
			require.Equal(t, tt.wantSize, size)
		})
	}
}

func TestConcatenateRecordBatches(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "ts", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "msg", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	tests := []struct {
		name       string
		buildInput func(t *testing.T) []arrow.RecordBatch
		wantErr    bool
		wantRows   int64
		check      func(t *testing.T, out arrow.RecordBatch)
	}{
		{name: "nil returns error", buildInput: func(*testing.T) []arrow.RecordBatch { return nil }, wantErr: true},
		{name: "empty slice returns error", buildInput: func(*testing.T) []arrow.RecordBatch { return []arrow.RecordBatch{} }, wantErr: true},
		{
			name: "single record returns equivalent batch",
			buildInput: func(t *testing.T) []arrow.RecordBatch {
				rec := arrowtest.Rows{map[string]any{"ts": int64(1), "msg": "a"}}.Record(memory.DefaultAllocator, schema)
				t.Cleanup(rec.Release)
				return []arrow.RecordBatch{rec}
			},
			wantErr:  false,
			wantRows: 1,
			check:    func(t *testing.T, out arrow.RecordBatch) { require.True(t, schema.Equal(out.Schema())) },
		},
		{
			name: "two records concatenate to one",
			buildInput: func(t *testing.T) []arrow.RecordBatch {
				rec1 := arrowtest.Rows{map[string]any{"ts": int64(1), "msg": "a"}}.Record(memory.DefaultAllocator, schema)
				rec2 := arrowtest.Rows{map[string]any{"ts": int64(2), "msg": "b"}}.Record(memory.DefaultAllocator, schema)
				t.Cleanup(rec1.Release)
				t.Cleanup(rec2.Release)
				return []arrow.RecordBatch{rec1, rec2}
			},
			wantErr:  false,
			wantRows: 2,
			check: func(t *testing.T, out arrow.RecordBatch) {
				require.True(t, schema.Equal(out.Schema()))
				outRows, err := arrowtest.RecordRows(out)
				require.NoError(t, err)
				require.Len(t, outRows, 2)
				require.Equal(t, int64(1), outRows[0]["ts"])
				require.Equal(t, int64(2), outRows[1]["ts"])
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := tt.buildInput(t)
			out, err := ConcatenateRecordBatches(input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			defer out.Release()
			require.Equal(t, tt.wantRows, out.NumRows())
			if tt.check != nil {
				tt.check(t, out)
			}
		})
	}
}
