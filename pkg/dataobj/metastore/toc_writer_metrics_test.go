package metastore

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestNewTocWriterMetrics(t *testing.T) {
	t.Run("creates a series for every operation and result before the first write, with conflict only for attempts and cancelled only for calls", func(t *testing.T) {
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
			"replace_index_pointers/already_applied",
			"replace_index_pointers/failed",
			"replace_index_pointers/superseded",
			"replace_index_pointers/written",
			"write_entry/already_applied",
			"write_entry/failed",
			"write_entry/superseded",
			"write_entry/written",
		}
		require.ElementsMatch(t, append(want, "replace_index_pointers/conflict", "write_entry/conflict"), series["loki_metastore_toc_write_attempt_duration_seconds"])
		require.ElementsMatch(t, append(want, "replace_index_pointers/cancelled", "write_entry/cancelled"), series["loki_metastore_toc_write_duration_seconds"])
	})
}
