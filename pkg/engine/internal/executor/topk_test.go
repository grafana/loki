package executor

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func Test_topk(t *testing.T) {
	colTs := semconv.ColumnIdentTimestamp.FQN()
	colMsg := semconv.ColumnIdentMessage.FQN()

	var (
		fields = []arrow.Field{
			semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, true),
			semconv.FieldFromIdent(semconv.ColumnIdentMessage, true),
		}
		schema = arrow.NewSchema(fields, nil)
	)

	var (
		pipelineA = NewArrowtestPipeline(schema, arrowtest.Rows{
			{colTs: time.Unix(1, 0).UTC(), colMsg: "line A"},
			{colTs: time.Unix(6, 0).UTC(), colMsg: "line F"},
		}, arrowtest.Rows{
			{colTs: time.Unix(2, 0).UTC(), colMsg: "line B"},
			{colTs: time.Unix(7, 0).UTC(), colMsg: "line G"},
		}, arrowtest.Rows{
			{colTs: time.Unix(3, 0).UTC(), colMsg: "line C"},
			{colTs: time.Unix(8, 0).UTC(), colMsg: "line H"},
		})

		pipelineB = NewArrowtestPipeline(schema, arrowtest.Rows{
			{colTs: time.Unix(4, 0).UTC(), colMsg: "line D"},
			{colTs: time.Unix(9, 0).UTC(), colMsg: "line I"},
		}, arrowtest.Rows{
			{colTs: time.Unix(5, 0).UTC(), colMsg: "line E"},
			{colTs: time.Unix(10, 0).UTC(), colMsg: "line J"},
		})
	)

	topkPipeline, err := newTopkPipeline(topkOptions{
		Inputs: []Pipeline{pipelineA, pipelineB},
		SortBy: []physical.ColumnExpression{
			&physical.ColumnExpr{
				Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin},
			},
		},
		Ascending: true,
		K:         3,
		MaxUnused: 5,
	})
	require.NoError(t, err, "should be able to create a topk pipeline")
	defer topkPipeline.Close()

	rec, err := topkPipeline.Read(t.Context())
	require.NoError(t, err, "should be able to read the sorted batch")

	expect := arrowtest.Rows{
		{colTs: time.Unix(1, 0).UTC(), colMsg: "line A"},
		{colTs: time.Unix(2, 0).UTC(), colMsg: "line B"},
		{colTs: time.Unix(3, 0).UTC(), colMsg: "line C"},
	}

	rows, err := arrowtest.RecordRows(rec)
	require.NoError(t, err, "should be able to convert record back to rows")

	require.ElementsMatch(t, expect, rows, "should return the top 3 rows")
}

func Test_topk_emptyPipelines(t *testing.T) {
	topkPipeline, err := newTopkPipeline(topkOptions{
		Inputs: []Pipeline{emptyPipeline()},
		SortBy: []physical.ColumnExpression{
			&physical.ColumnExpr{
				Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin},
			},
		},
		Ascending: true,
		K:         3,
		MaxUnused: 5,
	})
	require.NoError(t, err, "should be able to create a topk pipeline")
	defer topkPipeline.Close()

	rec, err := topkPipeline.Read(t.Context())
	require.ErrorIs(t, err, EOF, "should return EOF if there are no results")
	require.Nil(t, rec, "should not return a record if there are no results")
}
