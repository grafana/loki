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
	t.Run("calculates a simple expression with 1 input", func(t *testing.T) {
		colTs := "timestamp_ns.builtin.timestamp"
		colVal := "float64.generated.value"
		colEnv := "utf8.label.env"
		colSvc := "utf8.label.service"

		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0)

		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN(colTs, false),
			semconv.FieldFromFQN(colVal, false),
			semconv.FieldFromFQN(colEnv, false),
			semconv.FieldFromFQN(colSvc, false),
		}, nil)

		rowsPipeline1 := []arrowtest.Rows{
			{
				{colTs: time.Unix(20, 0).UTC(), colVal: float64(230), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(15, 0).UTC(), colVal: float64(120), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(10, 0).UTC(), colVal: float64(260), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(12, 0).UTC(), colVal: float64(250), colEnv: "dev", colSvc: "distributor"},
			},
		}
		input1 := NewArrowtestPipeline(alloc, schema, rowsPipeline1...)

		// value / 10
		mathExpr := &physical.MathExpression{
			Expression: &physical.BinaryExpr{
				Left: &physical.ColumnExpr{
					Ref: types.ColumnRef{
						Column: types.ColumnNameGeneratedValue,
						Type:   types.ColumnTypeGenerated,
					},
				},
				Right: physical.NewLiteral(float64(10)),
				Op:    types.BinaryOpDiv,
			},
		}

		pipeline := NewMathExpressionPipeline(mathExpr, input1, expressionEvaluator{})
		defer pipeline.Close()

		// Read the pipeline output
		record, err := pipeline.Read(t.Context())
		require.NoError(t, err)
		defer record.Release()

		expect := arrowtest.Rows{
			{colTs: time.Unix(20, 0).UTC(), colVal: float64(23), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(15, 0).UTC(), colVal: float64(12), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(10, 0).UTC(), colVal: float64(26), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(12, 0).UTC(), colVal: float64(25), colEnv: "dev", colSvc: "distributor"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("calculates a complex expression with 1 input", func(t *testing.T) {
		colTs := "timestamp_ns.builtin.timestamp"
		colVal := "float64.generated.value"
		colEnv := "utf8.label.env"
		colSvc := "utf8.label.service"

		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0)

		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN(colTs, false),
			semconv.FieldFromFQN(colVal, false),
			semconv.FieldFromFQN(colEnv, false),
			semconv.FieldFromFQN(colSvc, false),
		}, nil)

		rowsPipeline1 := []arrowtest.Rows{
			{
				{colTs: time.Unix(20, 0).UTC(), colVal: float64(230), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(15, 0).UTC(), colVal: float64(120), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(10, 0).UTC(), colVal: float64(260), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(12, 0).UTC(), colVal: float64(250), colEnv: "dev", colSvc: "distributor"},
			},
		}
		input1 := NewArrowtestPipeline(alloc, schema, rowsPipeline1...)

		// value * 10 + 100 / 10
		mathExpr := &physical.MathExpression{
			Expression: &physical.BinaryExpr{
				Left: &physical.BinaryExpr{
					Left: &physical.ColumnExpr{
						Ref: types.ColumnRef{
							Column: types.ColumnNameGeneratedValue,
							Type:   types.ColumnTypeGenerated,
						},
					},
					Right: physical.NewLiteral(float64(10)),
					Op:    types.BinaryOpMul,
				},
				Right: &physical.BinaryExpr{
					Left:  physical.NewLiteral(float64(100)),
					Right: physical.NewLiteral(float64(10)),
					Op:    types.BinaryOpDiv,
				},
				Op: types.BinaryOpAdd,
			},
		}

		pipeline := NewMathExpressionPipeline(mathExpr, input1, expressionEvaluator{})
		defer pipeline.Close()

		// Read the pipeline output
		record, err := pipeline.Read(t.Context())
		require.NoError(t, err)
		defer record.Release()

		expect := arrowtest.Rows{
			{colTs: time.Unix(20, 0).UTC(), colVal: float64(2310), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(15, 0).UTC(), colVal: float64(1210), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(10, 0).UTC(), colVal: float64(2610), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(12, 0).UTC(), colVal: float64(2510), colEnv: "dev", colSvc: "distributor"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("calculates a complex ex", func(t *testing.T) {
		colTs := "timestamp_ns.builtin.timestamp"
		colVal := "float64.generated.value"
		colValLeft := "float64.generated.value_left"
		colValRight := "float64.generated.value_right"
		colEnv := "utf8.label.env"
		colSvc := "utf8.label.service"

		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0)

		schema := arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN(colTs, false),
			semconv.FieldFromFQN(colValLeft, false),
			semconv.FieldFromFQN(colValRight, false),
			semconv.FieldFromFQN(colEnv, false),
			semconv.FieldFromFQN(colSvc, false),
		}, nil)

		rowsPipeline1 := []arrowtest.Rows{
			{
				{colTs: time.Unix(20, 0).UTC(), colValLeft: float64(230), colValRight: float64(2), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(15, 0).UTC(), colValLeft: float64(120), colValRight: float64(10), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(10, 0).UTC(), colValLeft: float64(260), colValRight: float64(4), colEnv: "prod", colSvc: "distributor"},
				{colTs: time.Unix(12, 0).UTC(), colValLeft: float64(250), colValRight: float64(20), colEnv: "dev", colSvc: "distributor"},
			},
		}
		input1 := NewArrowtestPipeline(alloc, schema, rowsPipeline1...)

		// value_left / value_right
		mathExpr := &physical.MathExpression{
			Expression: &physical.BinaryExpr{
				Left: &physical.ColumnExpr{
					Ref: types.ColumnRef{
						Column: "value_left",
						Type:   types.ColumnTypeGenerated,
					},
				},
				Right: &physical.ColumnExpr{
					Ref: types.ColumnRef{
						Column: "value_right",
						Type:   types.ColumnTypeGenerated,
					},
				},
				Op: types.BinaryOpDiv,
			},
		}

		pipeline := NewMathExpressionPipeline(mathExpr, input1, expressionEvaluator{})
		defer pipeline.Close()

		// Read the pipeline output
		record, err := pipeline.Read(t.Context())
		require.NoError(t, err)
		defer record.Release()

		expect := arrowtest.Rows{
			{colTs: time.Unix(20, 0).UTC(), colVal: float64(115), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(15, 0).UTC(), colVal: float64(12), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(10, 0).UTC(), colVal: float64(65), colEnv: "prod", colSvc: "distributor"},
			{colTs: time.Unix(12, 0).UTC(), colVal: float64(12.5), colEnv: "dev", colSvc: "distributor"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})
}
