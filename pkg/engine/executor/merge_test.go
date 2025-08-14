package executor

import (
	"context"
	"testing"
	"time"

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

	m, err := NewMergePipeline([]Pipeline{pipelineA, pipelineB})
	require.NoError(t, err)

	var got []arrowtest.Rows
	for {
		err = m.Read(context.Background())
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
