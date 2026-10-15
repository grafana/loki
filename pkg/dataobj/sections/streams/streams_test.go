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

func TestSection_HasColumn(t *testing.T) {
	tests := map[string]struct {
		columns []*Column
		lookup  ColumnType
		want    bool
	}{
		"a section with no columns at all holds nothing": {
			columns: nil,
			lookup:  ColumnTypeShardBucket,
			want:    false,
		},
		"a section written before the column type existed does not hold it": {
			columns: []*Column{
				{Type: ColumnTypeStreamID},
				{Type: ColumnTypeMinTimestamp},
				{Type: ColumnTypeLabel, Name: "app"},
			},
			lookup: ColumnTypeShardBucket,
			want:   false,
		},
		"a section holding it last is recognised": {
			columns: []*Column{
				{Type: ColumnTypeStreamID},
				{Type: ColumnTypeShardBucket},
			},
			lookup: ColumnTypeShardBucket,
			want:   true,
		},
		"a section holding it first is recognised too": {
			columns: []*Column{
				{Type: ColumnTypeShardBucket},
				{Type: ColumnTypeStreamID},
			},
			lookup: ColumnTypeShardBucket,
			want:   true,
		},
		"a label column is found by its type, whatever its name": {
			columns: []*Column{
				{Type: ColumnTypeStreamID},
				{Type: ColumnTypeLabel, Name: "app"},
			},
			lookup: ColumnTypeLabel,
			want:   true,
		},
		"a type no column carries is not found": {
			columns: []*Column{
				{Type: ColumnTypeStreamID},
				{Type: ColumnTypeShardBucket},
			},
			lookup: ColumnTypeMaxTimestamp,
			want:   false,
		},
		"the invalid type is not found in a section of valid columns": {
			columns: []*Column{
				{Type: ColumnTypeStreamID},
				{Type: ColumnTypeShardBucket},
			},
			lookup: ColumnTypeInvalid,
			want:   false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			section := &Section{columns: test.columns}
			require.Equal(t, test.want, section.HasColumn(test.lookup))
		})
	}
}

func TestSection_ShardLayout(t *testing.T) {
	openSection := func(t *testing.T, sb dataobj.SectionBuilder) *Section {
		t.Helper()
		obj := buildObjectWithUnknownColumn(t, sb)
		sec, err := Open(t.Context(), obj.Sections()[0])
		require.NoError(t, err)
		return sec
	}

	t.Run("returns the layout that the builder recorded", func(t *testing.T) {
		layout, err := ShardLayoutFromBuckets(4)
		require.NoError(t, err)
		builder := NewBuilder(nil, 1024, 0, layout)
		builder.SetTenant("tenant")
		builder.Record(labels.FromStrings("app", "foo"), time.Unix(10, 0), 1)

		got, err := openSection(t, builder).ShardLayout()
		require.NoError(t, err)
		require.Equal(t, layout, got)
	})

	t.Run("returns the default layout when the section has no shard count", func(t *testing.T) {
		got, err := openSection(t, &unknownColumnSectionBuilder{}).ShardLayout()
		require.NoError(t, err)
		require.Equal(t, DefaultShardLayout(), got)
	})

	t.Run("returns an error when the recorded shard count is not a power of two", func(t *testing.T) {
		_, err := openSection(t, &sortInfoSectionBuilder{info: &datasetmd.SortInfo{ShardCount: 24}}).ShardLayout()
		require.ErrorContains(t, err, "not a power of two")
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
	_ = streamIDBuilder.Append(0, dataset.Int64Value(1))
	if err := encodeTestColumns(&enc, streamIDBuilder); err != nil {
		return 0, err
	}
	enc.SetTenant("tenant")
	enc.SetSortInfo(b.info)
	return enc.Flush(w)
}
