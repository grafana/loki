package deletion

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseLogQLExpressionForDeletion(t *testing.T) {
	t.Run("invalid logql", func(t *testing.T) {
		logSelectorExpr, err := parseDeletionQuery("gjgjg ggj")
		require.Nil(t, logSelectorExpr)
		require.ErrorIs(t, err, errInvalidQuery)
	})

	t.Run("matcher expression", func(t *testing.T) {
		logSelectorExpr, err := parseDeletionQuery(`{env="dev", secret="true"}`)
		require.NotNil(t, logSelectorExpr)
		require.NoError(t, err)
	})

	t.Run("pipeline expression with line filter", func(t *testing.T) {
		logSelectorExpr, err := parseDeletionQuery(`{env="dev", secret="true"} |= "social sec number"`)
		require.NotNil(t, logSelectorExpr)
		require.NoError(t, err)
	})

	/* syntax.ParseLogSelector does not reject these
	t.Run("pipeline expression with label filter ", func(t *testing.T) {
		logSelectorExpr, err := parseDeletionQuery(`{env="dev", secret="true"} | json bob="top.params[0]"`)
		require.Nil(t, logSelectorExpr)
		require.ErrorIs(t, err, errUnsupportedQuery)
	})

	t.Run("metrics query", func(t *testing.T) {
		logSelectorExpr, err := parseDeletionQuery(`count_over_time({job="mysql"}[5m])`)
		require.Nil(t, logSelectorExpr)
		require.ErrorIs(t, err, errUnsupportedQuery)
	})
	*/
}
