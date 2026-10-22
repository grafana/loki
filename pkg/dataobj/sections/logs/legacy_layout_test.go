package logs

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestSection_LegacyLayout(t *testing.T) {
	open := func(t *testing.T, info *datasetmd.SortInfo) *Section {
		t.Helper()
		objBuilder := dataobj.NewBuilder(nil)
		require.NoError(t, objBuilder.Append(&sortInfoSectionBuilder{info: info}))
		obj, closer, err := objBuilder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { _ = closer.Close() })
		sec, err := Open(t.Context(), obj.Sections()[0])
		require.NoError(t, err)
		return sec
	}
	defaultBuckets := streams.DefaultShardLayout().Buckets()

	t.Run("returns the layout that a legacy writer recorded", func(t *testing.T) {
		got, ok := open(t, &datasetmd.SortInfo{
			SchemaLabels: []string{"label:cluster"},
			StreamOrder:  datasetmd.STREAM_ORDER_STABLE_HASH_V1,
			ShardCount:   defaultBuckets,
		}).LegacyLayout()
		require.True(t, ok)
		require.True(t, streams.NewLayout(streams.DefaultShardLayout(), []string{"label:cluster"}).Equal(got))
	})

	t.Run("reports no layout when the section records only column sorts", func(t *testing.T) {
		_, ok := open(t, &datasetmd.SortInfo{ColumnSorts: []*datasetmd.SortInfo_ColumnSort{{ColumnIndex: 0}}}).LegacyLayout()
		require.False(t, ok)
	})

	t.Run("reports no layout when the stream order is unspecified", func(t *testing.T) {
		_, ok := open(t, &datasetmd.SortInfo{SchemaLabels: []string{"label:app"}, ShardCount: defaultBuckets}).LegacyLayout()
		require.False(t, ok)
	})

	t.Run("reports no layout when the shard count is not the default", func(t *testing.T) {
		_, ok := open(t, &datasetmd.SortInfo{
			StreamOrder: datasetmd.STREAM_ORDER_STABLE_HASH_V1,
			ShardCount:  defaultBuckets / 2,
		}).LegacyLayout()
		require.False(t, ok)
	})
}

// TestLegacyFixtures checks that the shared legacy fixtures keep the legacy
// format. Fixtures in the current format would let the fallback tests pass
// without the fallback.
func TestLegacyFixtures(t *testing.T) {
	for name, schema := range map[string][]string{
		"legacy-cluster-a": {"label:cluster"},
		"legacy-cluster-b": {"label:cluster"},
		"legacy-app":       {"label:app"},
	} {
		t.Run(name+" records its layout only in its logs sections", func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "legacy", name+".dataobj"))
			require.NoError(t, err)
			obj, err := dataobj.FromReaderAt(bytes.NewReader(data), int64(len(data)))
			require.NoError(t, err)

			require.Positive(t, obj.Sections().Count(streams.CheckSection))
			for _, sec := range obj.Sections().Filter(streams.CheckSection) {
				opened, err := streams.Open(t.Context(), sec)
				require.NoError(t, err)
				_, ok, err := opened.Layout()
				require.NoError(t, err)
				require.False(t, ok)
			}

			var sections []*dataobj.Section
			for _, sec := range obj.Sections().Filter(CheckSection) {
				sections = append(sections, sec)
			}
			got, err := LegacyLayoutOf(t.Context(), sections)
			require.NoError(t, err)
			require.True(t, streams.NewLayout(streams.DefaultShardLayout(), schema).Equal(got), "got layout %+v", got)
		})
	}
}

func TestLegacyLayoutOf(t *testing.T) {
	legacy := func(schemaLabels ...string) *datasetmd.SortInfo {
		return &datasetmd.SortInfo{
			SchemaLabels: schemaLabels,
			StreamOrder:  datasetmd.STREAM_ORDER_STABLE_HASH_V1,
			ShardCount:   streams.DefaultShardLayout().Buckets(),
		}
	}
	sections := func(t *testing.T, infos ...*datasetmd.SortInfo) []*dataobj.Section {
		t.Helper()
		objBuilder := dataobj.NewBuilder(nil)
		for _, info := range infos {
			require.NoError(t, objBuilder.Append(&sortInfoSectionBuilder{info: info}))
		}
		obj, closer, err := objBuilder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { _ = closer.Close() })
		var out []*dataobj.Section
		for _, sec := range obj.Sections().Filter(CheckSection) {
			out = append(out, sec)
		}
		return out
	}
	clusterLayout := streams.NewLayout(streams.DefaultShardLayout(), []string{"label:cluster"})

	t.Run("returns the layout that every section records", func(t *testing.T) {
		got, err := LegacyLayoutOf(t.Context(), sections(t, legacy("label:cluster"), legacy("label:cluster")))
		require.NoError(t, err)
		require.True(t, clusterLayout.Equal(got))
	})

	t.Run("skips nil sections", func(t *testing.T) {
		secs := sections(t, legacy("label:cluster"))
		got, err := LegacyLayoutOf(t.Context(), []*dataobj.Section{nil, secs[0], nil})
		require.NoError(t, err)
		require.True(t, clusterLayout.Equal(got))
	})

	t.Run("returns an error when the sections record different layouts", func(t *testing.T) {
		_, err := LegacyLayoutOf(t.Context(), sections(t, legacy("label:cluster"), legacy("label:app")))
		require.ErrorContains(t, err, "different legacy layouts")
	})

	t.Run("returns an error when a section records no legacy layout", func(t *testing.T) {
		_, err := LegacyLayoutOf(t.Context(), sections(t, legacy("label:cluster"), &datasetmd.SortInfo{}))
		require.ErrorContains(t, err, "no legacy layout")
	})

	t.Run("returns an error when every section is nil", func(t *testing.T) {
		_, err := LegacyLayoutOf(t.Context(), []*dataobj.Section{nil})
		require.ErrorContains(t, err, "records no layout")
	})
}

// sortInfoSectionBuilder encodes a logs section with stream ID and timestamp
// columns and the given sort info.
type sortInfoSectionBuilder struct {
	info *datasetmd.SortInfo
}

func (b *sortInfoSectionBuilder) Type() dataobj.SectionType { return sectionType }

func (b *sortInfoSectionBuilder) Reset() {}

func (b *sortInfoSectionBuilder) Flush(w dataobj.SectionWriter) (int64, error) {
	var enc columnar.Encoder
	defer enc.Reset()

	streamIDBuilder := newInt64ColumnBuilder("stream_id", ColumnTypeStreamID.String())
	timestampBuilder := newInt64ColumnBuilder("timestamp", ColumnTypeTimestamp.String())
	if err := streamIDBuilder.Append(0, dataset.Int64Value(1)); err != nil {
		return 0, err
	}
	if err := timestampBuilder.Append(0, dataset.Int64Value(1)); err != nil {
		return 0, err
	}
	if err := encodeTestColumns(&enc, streamIDBuilder, timestampBuilder); err != nil {
		return 0, err
	}
	enc.SetTenant("tenant")
	enc.SetSortInfo(b.info)
	return enc.Flush(w)
}
