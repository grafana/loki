package querytee

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v4/pkg/querier/queryrange"
)

func TestAllStrategy(t *testing.T) {
	t.Run("selects a request of any type", func(t *testing.T) {
		require.Equal(t, Selection{Selected: true}, allStrategy{}.Select([]string{"a"}, &queryrange.LabelRequest{}))
	})
}
