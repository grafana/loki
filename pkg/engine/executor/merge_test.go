package executor

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

var (
	recordsInput1 = []arrowtest.Rows{
		{
			{"ts": int64(10), "table": "A", "line": "line A1"},
			{"ts": int64(15), "table": "A", "line": "line A2"},
			{"ts": int64(5), "table": "A", "line": "line A3"},
			{"ts": int64(20), "table": "A", "line": "line A4"},
		},
		{
			{"ts": int64(1), "table": "A", "line": "line A5"},
			{"ts": int64(50), "table": "A", "line": "line A6"},
		},
	}

	recordsInput2 = []arrowtest.Rows{
		{
			{"ts": int64(100), "table": "B", "line": "line B1"},
			{"ts": int64(75), "table": "B", "line": "line B2"},
			{"ts": int64(25), "table": "B", "line": "line B3"},
		},
		{
			{"ts": int64(13), "table": "B", "line": "line B4"},
			{"ts": int64(15), "table": "B", "line": "line B5"},
		},
		{
			{"ts": int64(23), "table": "B", "line": "line B6"},
			{"ts": int64(55), "table": "B", "line": "line B7"},
		},
	}
)

func TestMerge(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	var (
		rowsInput1 = []arrowtest.Rows{{
			{"timestamp": time.Unix(1, 0).UTC(), "message": "line A"},
			{"timestamp": time.Unix(6, 0).UTC(), "message": "line F"},
		}, {
			{"timestamp": time.Unix(2, 0).UTC(), "message": "line B"},
			{"timestamp": time.Unix(7, 0).UTC(), "message": "line G"},
		}, {
			{"timestamp": time.Unix(3, 0).UTC(), "message": "line C"},
			{"timestamp": time.Unix(8, 0).UTC(), "message": "line H"},
		}}
		rowsInput2 = []arrowtest.Rows{{
			{"timestamp": time.Unix(4, 0).UTC(), "message": "line D"},
			{"timestamp": time.Unix(9, 0).UTC(), "message": "line I"},
		}, {
			{"timestamp": time.Unix(5, 0).UTC(), "message": "line E"},
			{"timestamp": time.Unix(10, 0).UTC(), "message": "line J"},
		}}

		// pick schema from one of [arrowtest.Rows] as all of them have the same schema
		schema = rowsInput1[0].Schema()

		pipelineA = NewArrowtestPipeline(alloc, schema, rowsInput1...)
		pipelineB = NewArrowtestPipeline(alloc, schema, rowsInput2...)
	)

	m, err := newMergePipeline([]Pipeline{pipelineA, pipelineB}, 1)
	require.NoError(t, err)

	var got []arrowtest.Rows
	ctx := context.Background()
	for {
		err = m.Read(ctx)
		if err != nil {
			break
		}

		rec, _ := m.Value()
		defer rec.Release()

		rows, err := arrowtest.RecordRows(rec)
		require.NoError(t, err)

		got = append(got, rows)
	}

	require.ErrorIs(t, err, EOF)

	require.Equal(t, append(rowsInput1, rowsInput2...), got)
}

func TestMerge_concurrency(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	schema := arrow.NewSchema([]arrow.Field{
		{
			Name:     "ts",
			Type:     arrow.PrimitiveTypes.Int64,
			Nullable: true,
		},
		{
			Name:     "msg",
			Type:     arrow.BinaryTypes.String,
			Nullable: true,
		},
	}, nil)

	pipelineInputs := make([][]arrowtest.Rows, 10)
	var expectedRows []arrowtest.Rows

	// create records for each input pipeline
	for i := range len(pipelineInputs) {
		// Each pipeline has 2-4 records with unique identifiers
		numRecords := 2 + (i % 3) // 2, 3, or 4 records per pipeline
		rows := make([]arrowtest.Rows, numRecords)

		for j := range numRecords {
			recordID := fmt.Sprintf("pipeline_%d_record_%d", i, j)
			// Each record has 3-5 rows
			numRows := 3 + (j % 3) // 3, 4, or 5 rows per record
			recordRows := make(arrowtest.Rows, numRows)

			for k := range numRows {
				recordRows[k] = map[string]any{
					"ts":  int64(i*100 + j*10 + k),
					"msg": fmt.Sprintf("%s_row_%d", recordID, k),
				}
			}
			rows[j] = recordRows
		}

		// Accumulate expected rows
		expectedRows = append(expectedRows, rows...)
		pipelineInputs[i] = rows
	}

	// Test different concurrency settings
	for _, maxConcurrency := range []int{1, 3, 5, 10} {
		t.Run(fmt.Sprintf("concurrency=%d", maxConcurrency), func(t *testing.T) {
			// Create fresh pipelines using the pre-generated data
			pipelines := make([]Pipeline, 10)
			for i := range len(pipelines) {
				pipelines[i] = NewArrowtestPipeline(alloc, schema, pipelineInputs[i]...)
			}

			m, err := newMergePipeline(pipelines, maxConcurrency)
			require.NoError(t, err)
			defer m.Close()

			ctx := context.Background()
			var actualRows []arrowtest.Rows

			// Read all records from the merge pipeline
			for {
				err := m.Read(ctx)
				if errors.Is(err, EOF) {
					break
				}
				require.NoError(t, err, "Unexpected error during read")

				rec, _ := m.Value()
				defer rec.Release()

				rows, err := arrowtest.RecordRows(rec)
				require.NoError(t, err)

				actualRows = append(actualRows, rows)
			}

			// Compare actual vs expected rows
			require.ElementsMatch(t, expectedRows, actualRows)
		})
	}
}
