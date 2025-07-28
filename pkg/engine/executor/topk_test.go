package executor

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func Test_topk(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	var (
		fields = []arrow.Field{
			{
				Name:     types.ColumnNameBuiltinTimestamp,
				Type:     arrow.FixedWidthTypes.Timestamp_ns,
				Nullable: true,
				Metadata: datatype.ColumnMetadata(
					types.ColumnTypeBuiltin,
					datatype.Loki.Timestamp,
				),
			},
			{
				Name:     types.ColumnNameBuiltinMessage,
				Type:     arrow.BinaryTypes.String,
				Nullable: true,
				Metadata: datatype.ColumnMetadata(
					types.ColumnTypeBuiltin,
					datatype.Loki.String,
				),
			},
		}

		schema = arrow.NewSchema(fields, nil)
	)

	var (
		pipelineA = NewArrowtestPipeline(alloc, schema, arrowtest.Rows{
			{"timestamp": time.Unix(1, 0).UTC(), "message": "line A"},
			{"timestamp": time.Unix(6, 0).UTC(), "message": "line F"},
		}, arrowtest.Rows{
			{"timestamp": time.Unix(2, 0).UTC(), "message": "line B"},
			{"timestamp": time.Unix(7, 0).UTC(), "message": "line G"},
		}, arrowtest.Rows{
			{"timestamp": time.Unix(3, 0).UTC(), "message": "line C"},
			{"timestamp": time.Unix(8, 0).UTC(), "message": "line H"},
		})

		pipelineB = NewArrowtestPipeline(alloc, schema, arrowtest.Rows{
			{"timestamp": time.Unix(4, 0).UTC(), "message": "line D"},
			{"timestamp": time.Unix(9, 0).UTC(), "message": "line I"},
		}, arrowtest.Rows{
			{"timestamp": time.Unix(5, 0).UTC(), "message": "line E"},
			{"timestamp": time.Unix(10, 0).UTC(), "message": "line J"},
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

	require.NoError(t, topkPipeline.Read(t.Context()), "should be able to read the sorted batch")

	rec, err := topkPipeline.Value()
	require.NoError(t, err)
	defer rec.Release()

	expect := arrowtest.Rows{
		{"timestamp": time.Unix(1, 0).UTC(), "message": "line A"},
		{"timestamp": time.Unix(2, 0).UTC(), "message": "line B"},
		{"timestamp": time.Unix(3, 0).UTC(), "message": "line C"},
	}

	rows, err := arrowtest.RecordRows(rec)
	require.NoError(t, err, "should be able to convert record back to rows")

	require.Equal(t, expect, rows, "should return the top 3 rows in ascending order by timestamp")
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

	require.ErrorIs(t, topkPipeline.Read(t.Context()), EOF, "should return EOF if there are no results")

	rec, err := topkPipeline.Value()
	require.Nil(t, rec, "should not return a record if there are no results")
	require.ErrorIs(t, err, EOF, "should return EOF if there are no results")
}
