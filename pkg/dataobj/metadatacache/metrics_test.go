package metadatacache

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestNewMetrics(t *testing.T) {
	t.Run("exposes a zero series for every kind and operation before any cache call", func(t *testing.T) {
		reg := prometheus.NewPedanticRegistry()

		NewMetrics(reg)

		for name, wantSeries := range map[string]int{
			"loki_dataobj_metadata_cache_hits_total":   2,
			"loki_dataobj_metadata_cache_misses_total": 2,
			"loki_dataobj_metadata_cache_errors_total": 4,
		} {
			gotSeries, err := testutil.GatherAndCount(reg, name)
			require.NoError(t, err)
			require.Equal(t, wantSeries, gotSeries, name)
		}
	})
}
