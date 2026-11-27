package chunkbuilder

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestProcessor(t *testing.T) {
	now := time.Unix(1000, 0)

	t.Run("observes a record that decodes", func(t *testing.T) {
		p := newProcessor(newMetrics(prometheus.NewRegistry()), log.NewNopLogger())
		rec := flatRecord(t, 0, "tenant-a", `{app="test"}`, "line")
		rec.Offset = 7
		rec.Timestamp = now.Add(-90 * time.Second)

		p.process(rec, now)

		require.Equal(t, float64(1), testutil.ToFloat64(p.metrics.records))
		require.Equal(t, float64(len(rec.Value)), testutil.ToFloat64(p.metrics.receivedBytes))
		require.Equal(t, float64(7), testutil.ToFloat64(p.metrics.lastOffset))
		require.Equal(t, float64(90), testutil.ToFloat64(p.metrics.consumptionLag))
		require.Zero(t, testutil.ToFloat64(p.metrics.decodeFailures))
	})

	t.Run("decodes records in both encodings", func(t *testing.T) {
		p := newProcessor(newMetrics(prometheus.NewRegistry()), log.NewNopLogger())

		p.process(flatRecord(t, 0, "tenant-a", `{app="flat"}`, "line"), now)
		p.process(nestedRecord(t, 0, "tenant-a", `{app="nested"}`, "line"), now)

		require.Equal(t, float64(2), testutil.ToFloat64(p.metrics.records))
		require.Zero(t, testutil.ToFloat64(p.metrics.decodeFailures))
	})

	for _, tc := range []struct {
		name string
		rec  func(t *testing.T) *kgo.Record
	}{
		{
			name: "data in neither encoding",
			rec: func(_ *testing.T) *kgo.Record {
				return &kgo.Record{Key: []byte("tenant-a"), Value: []byte("invalid data")}
			},
		},
		{
			name: "no data",
			rec: func(_ *testing.T) *kgo.Record {
				return &kgo.Record{Key: []byte("tenant-a")}
			},
		},
		{
			name: "a stream without labels",
			rec: func(t *testing.T) *kgo.Record {
				return flatRecord(t, 0, "tenant-a", `{}`, "line")
			},
		},
		{
			name: "labels that do not parse",
			rec: func(t *testing.T) *kgo.Record {
				return flatRecord(t, 0, "tenant-a", `{app=`, "line")
			},
		},
		{
			name: "no tenant",
			rec: func(t *testing.T) *kgo.Record {
				return flatRecord(t, 0, "", `{app="test"}`, "line")
			},
		},
	} {
		t.Run("counts a decode failure for a record with "+tc.name, func(t *testing.T) {
			p := newProcessor(newMetrics(prometheus.NewRegistry()), log.NewNopLogger())
			rec := tc.rec(t)
			rec.Offset = 3

			p.process(rec, now)

			require.Equal(t, float64(1), testutil.ToFloat64(p.metrics.decodeFailures))
			require.Equal(t, float64(1), testutil.ToFloat64(p.metrics.records), "a record that fails to decode is still consumed")
			require.Equal(t, float64(3), testutil.ToFloat64(p.metrics.lastOffset))
		})
	}
}
