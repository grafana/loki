package logs

import (
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
