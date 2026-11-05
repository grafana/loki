package logql

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

const (
	histogramQuantileQuery = `histogram_quantile(0.99, sum by (path) (histogram_over_time({app="api"} | json | unwrap duration_seconds(duration) [5m])))`
	// The binary operations below have a part that the mappers split or shard, and a histogram part that they do not.
	histogramLeftQuery  = histogramQuantileQuery + ` + sum(count_over_time({app="api"}[5m]))`
	histogramRightQuery = `sum(count_over_time({app="api"}[5m])) + ` + histogramQuantileQuery
)

func TestShardMapper_HistogramFunc(t *testing.T) {
	m := NewShardMapper(NewPowerOfTwoStrategy(ConstantShards(2)), nilShardMetrics, nil)

	t.Run("on its own the query runs unsharded", func(t *testing.T) {
		expr := syntax.MustParseExpr(histogramQuantileQuery)
		noop, _, mapped, err := m.Parse(expr)
		require.NoError(t, err)
		require.True(t, noop)
		require.Equal(t, expr.String(), mapped.String())
	})

	for name, query := range map[string]string{
		"on the left of a binary operation":  histogramLeftQuery,
		"on the right of a binary operation": histogramRightQuery,
	} {
		t.Run("the other side is still sharded, histogram "+name, func(t *testing.T) {
			noop, _, mapped, err := m.Parse(syntax.MustParseExpr(query))
			require.NoError(t, err)
			require.False(t, noop)

			histogramSide := syntax.MustParseExpr(histogramQuantileQuery).String()
			require.Contains(t, mapped.String(), histogramSide, "the histogram side is not changed")
			require.Contains(t, mapped.String(), "downstream<", "the other side is sharded")
		})
	}
}

func TestRangeMapper_HistogramFunc(t *testing.T) {
	rvm, err := NewRangeMapper(time.Minute, nilShardMetrics, NewMapperStats())
	require.NoError(t, err)

	t.Run("on its own the query is not split", func(t *testing.T) {
		expr := syntax.MustParseExpr(histogramQuantileQuery)
		noop, mapped, err := rvm.Parse(expr)
		require.NoError(t, err)
		require.True(t, noop)
		require.Equal(t, expr.String(), mapped.String())
	})

	// The range mapper leaves a whole binary operation alone when one of its
	// sides cannot be split, so the query runs as it is. The case on the right
	// is the one that reaches the mapper's switch, because a binary operation is
	// only looked at when its left side can be split.
	for name, query := range map[string]string{
		"left":  histogramLeftQuery,
		"right": histogramRightQuery,
	} {
		t.Run("histogram on the "+name+" of a binary operation, the query is not split", func(t *testing.T) {
			expr := syntax.MustParseExpr(query)
			noop, mapped, err := rvm.Parse(expr)
			require.NoError(t, err)
			require.True(t, noop)
			require.Equal(t, expr.String(), mapped.String())
		})
	}
}
