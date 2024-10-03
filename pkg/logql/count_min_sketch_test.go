package logql

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
)

func TestHeapCountMinSketchVectorHeap(t *testing.T) {
	v := NewHeapCountMinSketchVector(0, promql.Vector{}, 3)

	a := labels.Labels{{Name: "event", Value: "a"}}
	b := labels.Labels{{Name: "event", Value: "b"}}
	c := labels.Labels{{Name: "event", Value: "c"}}
	d := labels.Labels{{Name: "event", Value: "d"}}

	v.Add(a, 2.0)
	v.Add(b, 4.0)
	v.Add(d, 5.5)
	require.Equal(t, "a", v.Metrics[0][0].Value)

	// Adding c drops a
	v.Add(c, 3.0)
	require.Equal(t, "c", v.Metrics[0][0].Value)
	require.Len(t, v.Metrics, v.maxLabels)
	require.NotContains(t, v.observed, a.String())

	// Increasing c to 6.0 should make b with 4,0 the smallest
	v.Add(c, 3.0)
	require.Equal(t, "b", v.Metrics[0][0].Value)

	// Increasing a to 5.0 drops b because it's the smallest
	v.Add(a, 3.0)
	require.Equal(t, "a", v.Metrics[0][0].Value)
	require.Len(t, v.Metrics, v.maxLabels)
	require.NotContains(t, v.observed, b.String())

	// Verify final list
	final := make([]string, v.maxLabels)
	for i, metric := range v.Metrics {
		final[i] = metric[0].Value
	}
	require.ElementsMatch(t, []string{"a", "d", "c"}, final)
}
