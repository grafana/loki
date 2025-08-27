package logical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestCollectLogfmtFilterKeys(t *testing.T) {
	t.Run("collects fields from label filters after logfmt", func(t *testing.T) {
		expr := syntax.MustParseExpr(`{app="test"} | logfmt | level="error" | method="GET"`)
		keys, hasLogfmt := collectLogfmtFilterKeys(expr)

		require.True(t, hasLogfmt, "should detect logfmt parser")
		require.ElementsMatch(t, []string{"level", "method"}, keys,
			"should collect all filtered field names")
	})

	t.Run("returns empty keys when logfmt has no filters", func(t *testing.T) {
		expr := syntax.MustParseExpr(`{app="test"} | logfmt`)
		keys, hasLogfmt := collectLogfmtFilterKeys(expr)

		require.True(t, hasLogfmt, "should detect logfmt parser")
		require.Empty(t, keys, "should return empty keys for parse-all scenario")
	})

	t.Run("returns false when no logfmt parser", func(t *testing.T) {
		expr := syntax.MustParseExpr(`{app="test"} | level="error"`)
		keys, hasLogfmt := collectLogfmtFilterKeys(expr)

		require.False(t, hasLogfmt, "should not detect logfmt")
		require.Empty(t, keys, "should return empty keys")
	})
}

func TestCollectLogfmtMetricKeys(t *testing.T) {
	t.Run("collects groupBy labels from vector aggregation", func(t *testing.T) {
		expr := syntax.MustParseExpr(`sum by (level, region) (count_over_time({app="test"} | logfmt [5m]))`)
		keys, hints := collectLogfmtMetricKeys(expr)

		require.Len(t, keys, 2, "should collect grouping labels")
		require.Contains(t, keys, "level", "should collect groupBy label")
		require.Contains(t, keys, "region", "should collect groupBy label")
		require.Empty(t, hints, "should return empty hints since there is no unwrap")
	})

	t.Run("collects unwrap identifier with type hint", func(t *testing.T) {
		expr := syntax.MustParseExpr(`sum by (app) (sum_over_time({app="test"} | logfmt | unwrap bytes [5m]))`)
		keys, hints := collectLogfmtMetricKeys(expr)

		require.Len(t, keys, 2, "should collect unwrap field and grouping label")
		require.Contains(t, keys, "bytes", "should collect unwrap field")
		require.Contains(t, keys, "app", "should collect unwrap field")
		require.Equal(t, "int64", hints["bytes"], "should have correct type hint")
	})

	t.Run("returns empty for queries without logfmt", func(t *testing.T) {
		expr := syntax.MustParseExpr(`sum by (app) (count_over_time({app="test"} [5m]))`)
		keys, hints := collectLogfmtMetricKeys(expr)

		require.Empty(t, keys, "should return empty keys without logfmt")
		require.Empty(t, hints, "should return empty hints without logfmt")
	})
}
