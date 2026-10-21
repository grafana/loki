package streams

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

func TestLayout_Equal(t *testing.T) {
	four, err := ShardLayoutFromBuckets(4)
	require.NoError(t, err)

	t.Run("reports equal layouts with the same shard layout, schema labels, and stream order", func(t *testing.T) {
		require.True(t, NewLayout(four, []string{"label:app"}).Equal(NewLayout(four, []string{"label:app"})))
	})

	t.Run("reports different layouts when the shard layouts differ", func(t *testing.T) {
		require.False(t, NewLayout(four, []string{"label:app"}).Equal(NewLayout(DefaultShardLayout(), []string{"label:app"})))
	})

	t.Run("reports different layouts when the schema labels differ", func(t *testing.T) {
		require.False(t, NewLayout(four, []string{"label:app"}).Equal(NewLayout(four, []string{"label:cluster"})))
	})

	t.Run("reports different layouts when the stream orders differ", func(t *testing.T) {
		require.False(t, NewLayout(four, nil).Equal(Layout{shardLayout: four}))
	})
}

func TestNewLayout(t *testing.T) {
	t.Run("copies the schema labels so later changes to the input do not affect the layout", func(t *testing.T) {
		schemaLabels := []string{"label:app"}
		layout := NewLayout(DefaultShardLayout(), schemaLabels)
		schemaLabels[0] = "label:cluster"

		require.Equal(t, []string{"label:app"}, layout.SchemaLabels())
	})

	t.Run("orders stream IDs by the stable hash stream key", func(t *testing.T) {
		require.Equal(t, StreamOrderStableHashV1, NewLayout(DefaultShardLayout(), nil).StreamOrder())
	})

}

func TestSection_Layout(t *testing.T) {
	openSection := func(t *testing.T, sb dataobj.SectionBuilder) *Section {
		t.Helper()
		obj := buildObjectWithUnknownColumn(t, sb)
		sec, err := Open(t.Context(), obj.Sections()[0])
		require.NoError(t, err)
		return sec
	}
	newBuilder := func() *Builder {
		b := NewBuilder(nil, 1024, 0, DefaultShardLayout())
		b.SetTenant("tenant")
		return b
	}

	t.Run("returns the layout that the builder recorded", func(t *testing.T) {
		four, err := ShardLayoutFromBuckets(4)
		require.NoError(t, err)
		want := NewLayout(four, []string{"label:app", "label:cluster"})
		b := newBuilder()
		b.SetLayout(want)
		b.Record(labels.FromStrings("app", "foo"), time.Unix(10, 0), 1)

		got, ok, err := openSection(t, b).Layout()
		require.NoError(t, err)
		require.True(t, ok)
		require.True(t, want.Equal(got), "got %+v, want %+v", got, want)
	})

	t.Run("buckets streams with the shard layout of the recorded layout", func(t *testing.T) {
		four, err := ShardLayoutFromBuckets(4)
		require.NoError(t, err)
		ls := labels.FromStrings("app", "foo")
		b := newBuilder()
		b.SetLayout(NewLayout(four, nil))
		b.Record(ls, time.Unix(10, 0), 1)

		var buckets []int64
		for res := range IterSection(t.Context(), openSection(t, b)) {
			stream, err := res.Value()
			require.NoError(t, err)
			buckets = append(buckets, stream.ShardBucket)
		}
		require.Equal(t, []int64{int64(four.BucketFromLabels(ls))}, buckets)
	})

	t.Run("returns a single-bucket layout that the builder recorded", func(t *testing.T) {
		one, err := ShardLayoutFromBuckets(1)
		require.NoError(t, err)
		want := NewLayout(one, nil)
		b := newBuilder()
		b.SetLayout(want)
		b.Record(labels.FromStrings("app", "foo"), time.Unix(10, 0), 1)

		got, ok, err := openSection(t, b).Layout()
		require.NoError(t, err)
		require.True(t, ok)
		require.True(t, want.Equal(got))
	})

	t.Run("keeps the layout across Reset", func(t *testing.T) {
		want := NewLayout(DefaultShardLayout(), []string{"label:app"})
		b := newBuilder()
		b.SetLayout(want)
		b.Record(labels.FromStrings("app", "foo"), time.Unix(10, 0), 1)
		b.Reset()
		b.SetTenant("tenant")
		b.Record(labels.FromStrings("app", "bar"), time.Unix(10, 0), 1)

		got, ok, err := openSection(t, b).Layout()
		require.NoError(t, err)
		require.True(t, ok)
		require.True(t, want.Equal(got))
	})

	t.Run("reports no layout when the builder recorded none", func(t *testing.T) {
		b := newBuilder()
		b.Record(labels.FromStrings("app", "foo"), time.Unix(10, 0), 1)

		_, ok, err := openSection(t, b).Layout()
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("returns an error when the recorded shard count is not a power of two", func(t *testing.T) {
		_, _, err := openSection(t, &sortInfoSectionBuilder{info: &datasetmd.SortInfo{ShardCount: 24, StreamOrder: datasetmd.STREAM_ORDER_STABLE_HASH_V1}}).Layout()
		require.ErrorContains(t, err, "not a power of two")
	})

	t.Run("returns an error when the recorded shard count is zero", func(t *testing.T) {
		_, _, err := openSection(t, &sortInfoSectionBuilder{info: &datasetmd.SortInfo{StreamOrder: datasetmd.STREAM_ORDER_STABLE_HASH_V1}}).Layout()
		require.ErrorContains(t, err, "not a power of two")
	})

	t.Run("returns an error when the recorded stream order is unspecified", func(t *testing.T) {
		_, _, err := openSection(t, &sortInfoSectionBuilder{info: &datasetmd.SortInfo{ShardCount: 32}}).Layout()
		require.ErrorContains(t, err, "unsupported stream order")
	})

	t.Run("returns an error when the section records column sorts", func(t *testing.T) {
		_, _, err := openSection(t, &sortInfoSectionBuilder{info: &datasetmd.SortInfo{
			ShardCount:  32,
			StreamOrder: datasetmd.STREAM_ORDER_STABLE_HASH_V1,
			ColumnSorts: []*datasetmd.SortInfo_ColumnSort{{ColumnIndex: 0}},
		}}).Layout()
		require.ErrorContains(t, err, "column sorts")
	})
}

// sortInfoSectionBuilder encodes a streams section with one stream ID column
// and the given sort info.
type sortInfoSectionBuilder struct {
	info *datasetmd.SortInfo
}

func (b *sortInfoSectionBuilder) Type() dataobj.SectionType { return sectionType }

func (b *sortInfoSectionBuilder) Reset() {}

func (b *sortInfoSectionBuilder) Flush(w dataobj.SectionWriter) (int64, error) {
	var enc columnar.Encoder
	defer enc.Reset()

	streamIDBuilder := newInt64ColumnBuilder("stream_id", ColumnTypeStreamID.String())
	if err := streamIDBuilder.Append(0, dataset.Int64Value(1)); err != nil {
		return 0, err
	}
	if err := encodeTestColumns(&enc, streamIDBuilder); err != nil {
		return 0, err
	}
	enc.SetTenant("tenant")
	enc.SetSortInfo(b.info)
	return enc.Flush(w)
}

func TestBuilder_SetLayout(t *testing.T) {
	t.Run("panics when the builder already holds streams", func(t *testing.T) {
		b := NewBuilder(nil, 1024, 0, DefaultShardLayout())
		b.Record(labels.FromStrings("app", "foo"), time.Unix(10, 0), 1)

		require.Panics(t, func() { b.SetLayout(NewLayout(DefaultShardLayout(), nil)) })
	})

	t.Run("panics when the layout is the zero value", func(t *testing.T) {
		b := NewBuilder(nil, 1024, 0, DefaultShardLayout())

		require.Panics(t, func() { b.SetLayout(Layout{}) })
	})
}
