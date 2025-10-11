package executor

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"

	"github.com/grafana/loki/v3/pkg/util/arrowtest"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestNewMathExpressionPipeline(t *testing.T) {
	colTs := "timestamp_ns.builtin.timestamp"
	colVal := "float64.generated.value"

	t.Run("calculates input_1 / 10", func(t *testing.T) {
		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0)

		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN(colTs, false),
			semconv.FieldFromFQN(colVal, false),
		}, nil)

		rowsPipeline1 := []arrowtest.Rows{
			{
				{colTs: time.Unix(20, 0).UTC(), colVal: float64(230)},
				{colTs: time.Unix(15, 0).UTC(), colVal: float64(120)},
				{colTs: time.Unix(10, 0).UTC(), colVal: float64(260)},
				{colTs: time.Unix(12, 0).UTC(), colVal: float64(250)},
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
			{colTs: time.Unix(20, 0).UTC(), colVal: float64(23)},
			{colTs: time.Unix(15, 0).UTC(), colVal: float64(12)},
			{colTs: time.Unix(10, 0).UTC(), colVal: float64(26)},
			{colTs: time.Unix(12, 0).UTC(), colVal: float64(25)},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})
}
