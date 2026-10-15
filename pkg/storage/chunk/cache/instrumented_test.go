package cache

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

// The typed wrappers must expose the same metrics and query statistics as the
// byte-slice wrappers so every cache appears on the same dashboards.
func TestTypedWrappersMatchByteSliceWrappers(t *testing.T) {
	ctx := context.Background()
	typedReg, byteReg := prometheus.NewRegistry(), prometheus.NewRegistry()

	int64sSize := func(v []int64) int { return len(v) * 8 }
	typedEmbedded := NewTypedEmbeddedCache[string, []int64]("typed", EmbeddedCacheConfig{MaxSizeItems: 2}, typedReg, log.NewNopLogger(), stats.IndexCache,
		func(entry *Entry[string, []int64]) uint64 {
			return EntryOverhead(entry) + uint64(len(entry.Key)) + uint64(cap(entry.Value))*8
		}, nil)
	typed := CollectTypedStats(InstrumentTyped("typed", typedEmbedded, typedReg, int64sSize), int64sSize)
	t.Cleanup(typed.Stop)

	bytes := CollectStats(Instrument("bytes", NewEmbeddedCache("bytes", EmbeddedCacheConfig{MaxSizeItems: 2}, byteReg, log.NewNopLogger(), stats.IndexCache), byteReg))
	t.Cleanup(bytes.Stop)

	typedStats, typedCtx := stats.NewContext(ctx)
	require.NoError(t, typed.Store(typedCtx, []string{"key"}, [][]int64{{1, 2}}))
	found, values, missing, err := typed.Fetch(typedCtx, []string{"key", "other"})
	require.NoError(t, err)
	require.Equal(t, []string{"key"}, found)
	require.Equal(t, [][]int64{{1, 2}}, values)
	require.Equal(t, []string{"other"}, missing)

	byteStats, byteCtx := stats.NewContext(ctx)
	require.NoError(t, bytes.Store(byteCtx, []string{"key"}, [][]byte{make([]byte, 16)}))
	_, _, _, err = bytes.Fetch(byteCtx, []string{"key", "other"})
	require.NoError(t, err)

	// Download time depends on the clock, so compare everything else.
	want, got := byteStats.Caches().Index, typedStats.Caches().Index
	want.DownloadTime, got.DownloadTime = 0, 0
	require.Equal(t, want, got)
	require.Equal(t, int64(16), got.BytesReceived)

	typedFamilies, err := typedReg.Gather()
	require.NoError(t, err)
	byteFamilies, err := byteReg.Gather()
	require.NoError(t, err)
	require.Len(t, typedFamilies, len(byteFamilies))
	for i, want := range byteFamilies {
		got := typedFamilies[i]
		require.Equal(t, want.GetName(), got.GetName())
		require.Equal(t, want.GetType(), got.GetType())
		switch want.GetType() {
		case dto.MetricType_HISTOGRAM:
			wantBuckets, gotBuckets := want.GetMetric()[0].GetHistogram().GetBucket(), got.GetMetric()[0].GetHistogram().GetBucket()
			require.Len(t, gotBuckets, len(wantBuckets), "%s must use the same buckets for every cache", want.GetName())
			for j := range wantBuckets {
				require.Equal(t, wantBuckets[j].GetUpperBound(), gotBuckets[j].GetUpperBound())
			}
		case dto.MetricType_COUNTER:
			require.Equal(t, want.GetMetric()[0].GetCounter().GetValue(), got.GetMetric()[0].GetCounter().GetValue(), want.GetName())
		case dto.MetricType_GAUGE:
			require.Equal(t, want.GetMetric()[0].GetGauge().GetValue(), got.GetMetric()[0].GetGauge().GetValue(), want.GetName())
		}
	}
}
