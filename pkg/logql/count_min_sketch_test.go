package logql

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/sketch"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestHeapCountMinSketchVectorHeap(t *testing.T) {
	v := NewHeapCountMinSketchVector(0, 0, 3)

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

func TestCountMinSketchSerialization(t *testing.T) {
	metric := []labels.Label{{Name: "foo", Value: "bar"}}
	cms, err := sketch.NewCountMinSketch(4, 2)
	require.NoError(t, err)
	vec := HeapCountMinSketchVector{
		CountMinSketchVector: CountMinSketchVector{
			T: 42,
			F: cms,
		},
		observed:  make(map[string]struct{}, 0),
		maxLabels: 10_000,
	}
	vec.Add(metric, 42.0)

	proto := &logproto.CountMinSketchVector{
		TimestampMs: 42,
		Sketch: &logproto.CountMinSketch{
			Depth:    2,
			Width:    4,
			Counters: []uint32{0, 0, 0, 42, 0, 42, 0, 0},
		},
		Metrics: []*logproto.Labels{
			{Metric: []*logproto.LabelPair{{Name: "foo", Value: "bar"}}},
		},
	}

	actual := vec.ToProto()
	require.Equal(t, proto, actual)

	round, err := CountMinSketchVectorFromProto(actual)
	require.NoError(t, err)

	// The HeapCountMinSketchVector is serialized to a CountMinSketchVector.
	require.Equal(t, round, vec.CountMinSketchVector)
}
