package logql

import (
	"math"
	"testing"

	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
)

func TestDefaultEvaluator_DivideByZero(t *testing.T) {
	ev := &defaultEvaluator{}

	require.Equal(t, true, math.IsNaN(ev.mergeBinOp(OpTypeDiv,
		&promql.Sample{
			Point: promql.Point{T: 1, V: 1},
		},
		&promql.Sample{
			Point: promql.Point{T: 1, V: 0},
		},
	).Point.V))

	require.Equal(t, true, math.IsNaN(ev.mergeBinOp(OpTypeMod,
		&promql.Sample{
			Point: promql.Point{T: 1, V: 1},
		},
		&promql.Sample{
			Point: promql.Point{T: 1, V: 0},
		},
	).Point.V))
}
