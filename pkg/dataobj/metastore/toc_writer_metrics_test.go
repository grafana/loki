package metastore

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestNewTocWriterMetrics(t *testing.T) {
	t.Run("creates a series for every operation and result before the first write", func(t *testing.T) {
		reg := prometheus.NewPedanticRegistry()
		NewTocWriterMetrics(reg)

		families, err := reg.Gather()
		require.NoError(t, err)

		series := make(map[string][]string)
		for _, family := range families {
			for _, metric := range family.GetMetric() {
				labels := make(map[string]string)
				for _, label := range metric.GetLabel() {
					labels[label.GetName()] = label.GetValue()
				}
				series[family.GetName()] = append(series[family.GetName()], labels["op"]+"/"+labels["result"])
			}
		}

		want := []string{
			"replace_index_pointers/already_present",
			"replace_index_pointers/failed",
			"replace_index_pointers/race_lost",
			"replace_index_pointers/written",
			"write_entry/already_present",
			"write_entry/failed",
			"write_entry/race_lost",
			"write_entry/written",
		}
		require.ElementsMatch(t, want, series["loki_metastore_toc_change_attempt_seconds"])
		require.ElementsMatch(t, want, series["loki_metastore_toc_change_duration_seconds"])
	})
}
