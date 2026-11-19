package stats_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestLayoutSectionBuilder(t *testing.T) {
	layout := streams.NewLayout(streams.MustShardLayoutFromBuckets(8), []string{"label:service_name"})
	openSection := func(t *testing.T) *stats.Section {
		t.Helper()
		builder := dataobj.NewBuilder(nil)
		require.NoError(t, builder.Append(stats.NewLayoutSectionBuilder("acme", layout)))
		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })
		sections := obj.Sections()
		require.Len(t, sections, 1)
		require.True(t, stats.CheckSection(sections[0]))
		require.Equal(t, "acme", sections[0].Tenant)
		sec, err := stats.Open(t.Context(), sections[0])
		require.NoError(t, err)
		return sec
	}

	t.Run("writes an empty stats section that records the layout", func(t *testing.T) {
		sec := openSection(t)
		require.True(t, sec.Empty())
		got, ok, err := sec.Layout()
		require.NoError(t, err)
		require.True(t, ok)
		require.True(t, got.Equal(layout))
	})

	t.Run("writes a section that a row reader reads as no rows", func(t *testing.T) {
		reader := stats.NewRowReader(t.Context(), openSection(t))
		require.False(t, reader.Next())
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
	})

	t.Run("returns an error and writes no section for the zero layout", func(t *testing.T) {
		builder := dataobj.NewBuilder(nil)
		require.ErrorContains(t, builder.Append(stats.NewLayoutSectionBuilder("acme", streams.Layout{})), "zero layout")
		require.Zero(t, builder.Bytes())
	})
}
