package executor

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/arrowtest"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestNewMathExpressionPipeline(t *testing.T) {
	t.Run("calculates input_1 / 10", func(t *testing.T) {
		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0)

		schema := arrow.NewSchema([]arrow.Field{
			{Name: types.ColumnNameBuiltinTimestamp, Type: datatype.Arrow.Timestamp, Metadata: datatype.ColumnMetadataBuiltinTimestamp},
			{Name: types.ColumnNameGeneratedValue, Type: datatype.Arrow.Float, Metadata: datatype.ColumnMetadata(types.ColumnTypeGenerated, datatype.Loki.Float)},
		}, nil)

		rowsPipeline1 := []arrowtest.Rows{
			{
				{"timestamp": time.Unix(20, 0).UTC(), "value": float64(230)},
				{"timestamp": time.Unix(15, 0).UTC(), "value": float64(120)},
				{"timestamp": time.Unix(10, 0).UTC(), "value": float64(260)},
				{"timestamp": time.Unix(12, 0).UTC(), "value": float64(250)},
			},
		}
		input1 := NewArrowtestPipeline(alloc, schema, rowsPipeline1...)

		mathExpr := &physical.MathExpression{
			Expression: &physical.BinaryExpr{
				Left: &physical.ColumnExpr{
					Ref: types.ColumnRef{
						Column: "input_0",
						Type:   types.ColumnTypeGenerated,
					},
				},
				Right: physical.NewLiteral(float64(10)),
				Op:    types.BinaryOpDiv,
			},
		}

		pipeline := NewMathExpressionPipeline(mathExpr, []Pipeline{input1}, expressionEvaluator{})
		defer pipeline.Close()

		// Read the pipeline output
		record, err := pipeline.Read(t.Context())
		require.NoError(t, err)
		defer record.Release()

		expect := arrowtest.Rows{
			{"timestamp": time.Unix(20, 0).UTC(), "value": float64(23)},
			{"timestamp": time.Unix(15, 0).UTC(), "value": float64(12)},
			{"timestamp": time.Unix(10, 0).UTC(), "value": float64(26)},
			{"timestamp": time.Unix(12, 0).UTC(), "value": float64(25)},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})
}
