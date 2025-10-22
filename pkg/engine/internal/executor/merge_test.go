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

	// Test different concurrency settings
	//  0 ... no prefetching of next input / only current input is prefetched
	//  1 ... next input is prefetched
	//  3 ... number of prefetched inputs is equal to the number of inputs
	//  5 ... number of prefetched inputs is greater than the number of inputs
	// -1 ... unlimited/all inputs are prefetched
	for _, maxPrefetch := range []int{0, 1, n, 5, -1} {

		t.Run(fmt.Sprintf("context=full/maxPrefetch=%d", maxPrefetch), func(t *testing.T) {
			// Create fresh inputs using the pre-generated data
			inputs := make([]Pipeline, n)
			for i := range len(inputs) {
				inputs[i] = NewArrowtestPipeline(schema, inputRows[i]...)
			}

			m, err := newMergePipeline(inputs, maxPrefetch)
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

		t.Run(fmt.Sprintf("context=canceled/maxPrefetch=%d", maxPrefetch), func(t *testing.T) {
			// Create fresh inputs using the pre-generated data
			inputs := make([]Pipeline, n)
			for i := range len(inputs) {
				inputs[i] = NewArrowtestPipeline(schema, inputRows[i]...)
			}

			m, err := newMergePipeline(inputs, maxPrefetch)
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
}
