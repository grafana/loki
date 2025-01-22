package logql

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/sketch"
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
		observed:  make(map[uint64]struct{}, 0),
		maxLabels: 10_000,
		buffer:    make([]byte, 0, 1024),
	}
	vec.Add(metric, 42.0)

	hllBytes, _ := vec.F.HyperLogLog.MarshalBinary()
	proto := &logproto.CountMinSketchVector{
		TimestampMs: 42,
		Sketch: &logproto.CountMinSketch{
			Depth:       2,
			Width:       4,
			Counters:    []float64{0, 42, 0, 0, 0, 42, 0, 0},
			Hyperloglog: hllBytes,
		},
		Metrics: []*logproto.Labels{
			{Metric: []*logproto.LabelPair{{Name: "foo", Value: "bar"}}},
		},
	}

	actual, err := vec.ToProto()
	require.NoError(t, err)
	require.Equal(t, proto, actual)

	round, err := CountMinSketchVectorFromProto(actual)
	require.NoError(t, err)

	// The HeapCountMinSketchVector is serialized to a CountMinSketchVector.
	require.Equal(t, round, vec.CountMinSketchVector)
}

func BenchmarkHeapCountMinSketchVectorAdd(b *testing.B) {
	maxLabels := 10_000
	v := NewHeapCountMinSketchVector(0, maxLabels, maxLabels)
	if len(v.Metrics) > maxLabels || cap(v.Metrics) > maxLabels+1 {
		b.Errorf("Length or capcity of metrics is too high: len=%d cap=%d", len(v.Metrics), cap(v.Metrics))
	}

	eventsCount := 100_000
	uniqueEventsCount := 20_000
	events := make([]labels.Labels, eventsCount)
	for i := range events {
		events[i] = labels.Labels{{Name: "event", Value: fmt.Sprintf("%d", i%uniqueEventsCount)}}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		for _, event := range events {
			v.Add(event, rand.Float64())
			if len(v.Metrics) > maxLabels || cap(v.Metrics) > maxLabels+1 {
				b.Errorf("Length or capcity of metrics is too high: len=%d cap=%d", len(v.Metrics), cap(v.Metrics))
			}
		}
	}
}
