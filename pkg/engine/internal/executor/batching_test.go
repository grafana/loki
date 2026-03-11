package executor

import (
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// recordInput pairs a schema with rows to build one record for pipeline tests.
type recordInput struct {
	Schema *arrow.Schema
	Rows   arrowtest.Rows
}

func TestBatchingPipeline(t *testing.T) {
	schemaAB := arrow.NewSchema([]arrow.Field{
		{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "b", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	tests := []struct {
		name          string
		batchSize     int64
		inputs        []recordInput
		wantBatches   int
		wantTotalRows int64
		checkBatches  func(t *testing.T, batches []arrow.RecordBatch)
	}{
		{
			name:      "batchSize=0 passes records through unchanged",
			batchSize: 0,
			inputs: []recordInput{
				{schemaAB, arrowtest.Rows{{"a": int64(1), "b": "x"}}},
				{schemaAB, arrowtest.Rows{{"a": int64(2), "b": "x"}}},
				{schemaAB, arrowtest.Rows{{"a": int64(3), "b": "x"}}},
			},
			wantBatches:   3,
			wantTotalRows: 3,
		},
		{
			name:      "batchSize=2 fits exactly two records per batch",
			batchSize: 2,
			inputs: []recordInput{
				{schemaAB, arrowtest.Rows{{"a": int64(1), "b": "x"}}},
				{schemaAB, arrowtest.Rows{{"a": int64(2), "b": "x"}}},
				{schemaAB, arrowtest.Rows{{"a": int64(3), "b": "x"}}},
				{schemaAB, arrowtest.Rows{{"a": int64(4), "b": "x"}}},
				{schemaAB, arrowtest.Rows{{"a": int64(5), "b": "x"}}},
			},
			wantBatches:   3,
			wantTotalRows: 5,
			checkBatches: func(t *testing.T, batches []arrow.RecordBatch) {
				require.Equal(t, int64(2), batches[0].NumRows(), "first batch: 2 rows")
				require.Equal(t, int64(2), batches[1].NumRows(), "second batch: 2 rows")
				require.Equal(t, int64(1), batches[2].NumRows(), "third batch: 1 row")
			},
		},
		{
			name:      "batchSize=1 each record sent alone",
			batchSize: 1,
			inputs: []recordInput{
				{schemaAB, arrowtest.Rows{{"a": int64(1), "b": "x"}}},
				{schemaAB, arrowtest.Rows{{"a": int64(2), "b": "x"}}},
				{schemaAB, arrowtest.Rows{{"a": int64(3), "b": "x"}}},
			},
			wantBatches:   3,
			wantTotalRows: 3,
		},
		{
			name:      "records with different schemas are batched with union schema",
			batchSize: 10, // large enough to batch both records
			inputs: []recordInput{
				{
					arrow.NewSchema([]arrow.Field{
						{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
						{Name: "b", Type: arrow.BinaryTypes.String, Nullable: true},
					}, nil),
					arrowtest.Rows{{"a": int64(1), "b": "x"}},
				},
				{
					arrow.NewSchema([]arrow.Field{
						{Name: "b", Type: arrow.BinaryTypes.String, Nullable: true},
						{Name: "c", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
					}, nil),
					arrowtest.Rows{{"b": "y", "c": int64(2)}},
				},
			},
			wantBatches:   1,
			wantTotalRows: 2,
			checkBatches: func(t *testing.T, batches []arrow.RecordBatch) {
				fields := make([]string, batches[0].Schema().NumFields())
				for i := range fields {
					fields[i] = batches[0].Schema().Field(i).Name
				}
				require.Equal(t, []string{"a", "b", "c"}, fields, "union schema: fields in order first seen")
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

			p := NewBatchingPipeline(NewBufferedPipeline(records...), tt.batchSize)
			defer p.Close()

			require.NoError(t, p.Open(ctx))
			var batches []arrow.RecordBatch
			var totalRows int64
			for {
				rec, err := p.Read(ctx)
				if errors.Is(err, EOF) {
					break
				}
				require.NoError(t, err)
				batches = append(batches, rec)
				totalRows += rec.NumRows()
			}

			require.Len(t, batches, tt.wantBatches)
			require.Equal(t, tt.wantTotalRows, totalRows)
			if tt.checkBatches != nil {
				tt.checkBatches(t, batches)
			}
		})
	}
}

