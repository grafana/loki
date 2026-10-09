package metastore

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestNewTocWriterMetrics(t *testing.T) {
	t.Run("creates a series for every operation and result before the first write", func(t *testing.T) {
		reg := prometheus.NewPedanticRegistry()
		NewTocWriterMetrics(reg)

		for _, name := range []string{
			"loki_metastore_toc_change_attempt_seconds",
			"loki_metastore_toc_change_duration_seconds",
		} {
			count, err := testutil.GatherAndCount(reg, name)
			require.NoError(t, err)
			require.Equal(t, 8, count, name)
		}
	})
}
