package indexobj

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestTenantLayout(t *testing.T) {
	appLayout := streams.NewLayout(streams.DefaultShardLayout(), []string{"label:app"})
	clusterLayout := streams.NewLayout(streams.DefaultShardLayout(), []string{"label:cluster"})

	// buildStats builds an object with one stats section of tenant per layout.
	buildStats := func(t *testing.T, tenant string, layouts ...streams.Layout) *dataobj.Object {
		t.Helper()
		builder := dataobj.NewBuilder(nil)
		for i, layout := range layouts {
			statsBuilder := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(2048, 1000), layout)
			statsBuilder.SetTenant(tenant)
			statsBuilder.Append(stats.Stat{ObjectPath: "logs/obj", SectionIndex: int64(i)})
			require.NoError(t, builder.Append(statsBuilder))
		}
		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })
		return obj
	}

	t.Run("returns the layout that every stats section records", func(t *testing.T) {
		got, ok, err := TenantLayout(t.Context(), buildStats(t, "acme", appLayout, appLayout), "acme")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, appLayout, got)
	})

	t.Run("reports no layout when the index has no stats section for the tenant", func(t *testing.T) {
		_, ok, err := TenantLayout(t.Context(), buildStats(t, "other", appLayout), "acme")
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("returns a mixed layouts error when stats sections record different layouts", func(t *testing.T) {
		_, _, err := TenantLayout(t.Context(), buildStats(t, "acme", appLayout, clusterLayout), "acme")
		require.ErrorIs(t, err, ErrMixedLayouts)
	})

	t.Run("derives the layout of a legacy index from its stats and postings rows", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "legacy", "legacy-index.dataobj"))
		require.NoError(t, err)
		obj, err := dataobj.FromReaderAt(bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)

		got, ok, err := TenantLayout(t.Context(), obj, "acme")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, streams.NewLayout(streams.DefaultShardLayout(), []string{"label:service_name"}), got)
	})
}

func TestBuilder_SetLayout(t *testing.T) {
	appLayout := streams.NewLayout(streams.DefaultShardLayout(), []string{"label:app"})
	ts := time.Unix(10, 0).UTC()
	newBuilder := func(t *testing.T) *Builder {
		t.Helper()
		b, err := NewBuilder(testTenant, testBuilderConfig, nil, NewBuilderMetrics(nil))
		require.NoError(t, err)
		return b
	}

	t.Run("records the layout in every stats section", func(t *testing.T) {
		b := newBuilder(t)
		require.NoError(t, b.SetLayout(appLayout))
		require.NoError(t, b.AppendStat("logs/obj", 0, 0, map[string]string{"app": "a"}, ts, ts, 1, 10))

		obj, closer, err := b.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		got, ok, err := TenantLayout(t.Context(), obj, testTenant)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, appLayout, got)
	})

	t.Run("accepts the same layout again", func(t *testing.T) {
		b := newBuilder(t)
		require.NoError(t, b.SetLayout(appLayout))
		require.NoError(t, b.SetLayout(streams.NewLayout(streams.DefaultShardLayout(), []string{"label:app"})))
	})

	t.Run("returns an error for another layout", func(t *testing.T) {
		b := newBuilder(t)
		require.NoError(t, b.SetLayout(appLayout))
		require.ErrorContains(t, b.SetLayout(streams.NewLayout(streams.DefaultShardLayout(), []string{"label:cluster"})), "already has stream layout")
	})

	t.Run("returns an error for a stat appended before the layout is set", func(t *testing.T) {
		require.ErrorContains(t, newBuilder(t).AppendStat("logs/obj", 0, 0, nil, ts, ts, 1, 10), "before the stream layout is set")
	})

	t.Run("accepts another layout after Reset", func(t *testing.T) {
		b := newBuilder(t)
		require.NoError(t, b.SetLayout(appLayout))
		b.Reset()
		require.NoError(t, b.SetLayout(streams.NewLayout(streams.DefaultShardLayout(), []string{"label:cluster"})))
	})
}
