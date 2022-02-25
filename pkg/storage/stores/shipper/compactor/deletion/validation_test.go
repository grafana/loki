package deletion

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvalidLogQLExpressionForDeletion(t *testing.T) {
	t.Run("invalid logql", func(t *testing.T) {
		err := checkLogQLExpressionForDeletion("gjgjg ggj")
		require.ErrorIs(t, err, errInvalidLogQL)
	})

	t.Run("matcher expression", func(t *testing.T) {
		err := checkLogQLExpressionForDeletion(`{env="dev", secret="true"}`)
		require.NoError(t, err)
	})

	t.Run("pipeline expression with line filter", func(t *testing.T) {
		err := checkLogQLExpressionForDeletion(`{env="dev", secret="true"} |= "social sec number"`)
		require.NoError(t, err)
	})

	t.Run("pipeline expression with label filter ", func(t *testing.T) {
		err := checkLogQLExpressionForDeletion(`{env="dev", secret="true"} | json bob="top.params[0]"`)
		require.ErrorIs(t, err, errUnsupportedLogQL)
	})

	t.Run("pipeline expression with ", func(t *testing.T) {
		err := checkLogQLExpressionForDeletion(`count_over_time({job="mysql"}[5m])`)
		require.ErrorIs(t, err, errUnsupportedLogQL)
	})
}
