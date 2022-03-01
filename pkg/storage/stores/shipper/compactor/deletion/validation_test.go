package deletion

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseLogQLExpressionForDeletion(t *testing.T) {
	t.Run("invalid logql", func(t *testing.T) {
		matchers, err := parseDeletionQuery("gjgjg ggj")
		require.Nil(t, matchers)
		require.ErrorIs(t, err, errInvalidQuery)
	})

	t.Run("matcher expression", func(t *testing.T) {
		matchers, err := parseDeletionQuery(`{env="dev", secret="true"}`)
		require.NotNil(t, matchers)
		require.NoError(t, err)
	})

	t.Run("pipeline expression with line filter", func(t *testing.T) {
		matchers, err := parseDeletionQuery(`{env="dev", secret="true"} |= "social sec number"`)
		require.Nil(t, matchers)
		require.ErrorIs(t, err, errUnsupportedQuery)
	})

	t.Run("pipeline expression with label filter ", func(t *testing.T) {
		matchers, err := parseDeletionQuery(`{env="dev", secret="true"} | json bob="top.params[0]"`)
		require.Nil(t, matchers)
		require.ErrorIs(t, err, errUnsupportedQuery)
	})

	t.Run("metrics query", func(t *testing.T) {
		matchers, err := parseDeletionQuery(`count_over_time({job="mysql"}[5m])`)
		require.Nil(t, matchers)
		require.ErrorIs(t, err, errUnsupportedQuery)
	})
}
