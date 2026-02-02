package executor

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// testContext returns a context for merge tests that drain the merge until EOF.
// We use Background() so draining is not interrupted by test context cancellation.
func testContext(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

func TestMerge(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "timestamp", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "message", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	n := 3 // number of inputs
	inputRows := make([][]arrowtest.Rows, n)
	var expectedRows []arrowtest.Rows

	// create records for each input pipeline
	for i := range len(inputRows) {
		// Each pipeline has 2-4 records with unique identifiers
		numRecords := 2 + (i % n) // 2, 3, or 4 records per pipeline
		rows := make([]arrowtest.Rows, numRecords)

		for j := range numRecords {
			// Each record has 3-5 rows
			numRows := 3 + (j % n) // 3, 4, or 5 rows per record
			recordRows := make(arrowtest.Rows, numRows)

			for k := range numRows {
				recordRows[k] = map[string]any{
					"timestamp": int64(i*100 + j*10 + k),
					"message":   fmt.Sprintf("input=%d record=%d row=%d", i, j, k),
				}
			}
			rows[j] = recordRows
		}

		// Accumulate expected rows
		expectedRows = append(expectedRows, rows...)
		inputRows[i] = rows
	}

	t.Run("context=full", func(t *testing.T) {
		// Create fresh inputs using the pre-generated data
		inputs := make([]Pipeline, n)
		for i := range len(inputs) {
			inputs[i] = NewArrowtestPipeline(schema, inputRows[i]...)
		}

		m, err := newMergePipeline(inputs, 0, nil)
		require.NoError(t, err)
		defer m.Close()

		ctx := t.Context()
		var actualRows []arrowtest.Rows

		// Read all records from the merge pipeline
		for {
			rec, err := m.Read(ctx)
			if errors.Is(err, EOF) {
				t.Log("stop reading from pipeline:", err)
				break
			}
			require.NoError(t, err, "Unexpected error during read")

			rows, err := arrowtest.RecordRows(rec)
			require.NoError(t, err)

			actualRows = append(actualRows, rows)
		}

		// Compare actual vs expected rows
		// Order of processed inputs must stay the same
		require.Equal(t, expectedRows, actualRows)
	})

	t.Run("context=canceled", func(t *testing.T) {
		// Create fresh inputs using the pre-generated data
		inputs := make([]Pipeline, n)
		for i := range len(inputs) {
			inputs[i] = NewArrowtestPipeline(schema, inputRows[i]...)
		}

		m, err := newMergePipeline(inputs, 0, nil)
		require.NoError(t, err)
		defer m.Close()

		ctx := t.Context()
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		var gotRows int64
		for {
			// Cancel the context once half of the expected/generated rows was consumed
			if gotRows > int64(len(expectedRows)/2) {
				cancel()
			}

			rec, err := m.Read(ctx)
			if errors.Is(err, EOF) || errors.Is(err, context.Canceled) {
				t.Log("stop reading from pipeline:", err)
				break
			}
			require.NoError(t, err, "Unexpected error during read")

			gotRows += rec.NumRows()
		}
	})
}

// TestMerge_Buffering verifies that with bufferBatchCount > 0 the merge returns
// equally sized batches (except the last), regardless of the number of inputs.
func TestMerge_Buffering(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "timestamp", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "message", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	tests := []struct {
		name               string
		batchesPerInput    []int // each input has this many 1-row batches
		bufferBatchCount   int
		expectedBatchSizes []int64 // expected NumRows() per Read() until EOF
	}{
		{
			name:               "single input buffer 3",
			batchesPerInput:    []int{5},
			bufferBatchCount:   3,
			expectedBatchSizes: []int64{3, 2},
		},
		{
			name:               "three inputs buffer 2",
			batchesPerInput:    []int{5, 3, 4},
			bufferBatchCount:   2,
			expectedBatchSizes: []int64{2, 2, 2, 2, 2, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs := make([]Pipeline, len(tt.batchesPerInput))
			for i, numBatches := range tt.batchesPerInput {
				rows := make([]arrowtest.Rows, numBatches)
				for j := range rows {
					rows[j] = arrowtest.Rows{
						map[string]any{"timestamp": int64(i*100 + j), "message": fmt.Sprintf("input%d-batch%d", i, j)},
					}
				}
				inputs[i] = NewArrowtestPipeline(schema, rows...)
			}

			m, err := newMergePipeline(inputs, tt.bufferBatchCount, nil)
			require.NoError(t, err)
			defer m.Close()

			ctx := testContext(t)
			var actualBatchSizes []int64
			for {
				rec, err := m.Read(ctx)
				if errors.Is(err, EOF) || errors.Is(err, context.Canceled) {
					break
				}
				require.NoError(t, err)
				require.NotNil(t, rec)
				actualBatchSizes = append(actualBatchSizes, rec.NumRows())
				rec.Release()
			}

			require.Equal(t, tt.expectedBatchSizes, actualBatchSizes, "batch sizes should match regardless of number of inputs")

			// All but the last batch should have the same size (bufferBatchCount when each input batch has 1 row)
			if tt.bufferBatchCount > 0 && len(actualBatchSizes) > 1 {
				expectedFullSize := int64(tt.bufferBatchCount)
				for i := 0; i < len(actualBatchSizes)-1; i++ {
					require.Equal(t, expectedFullSize, actualBatchSizes[i], "batch %d should be full size", i)
				}
			}
		})
	}
}
