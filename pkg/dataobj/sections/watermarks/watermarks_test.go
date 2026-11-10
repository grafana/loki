package watermarks

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

type testColumn struct {
	tag     string
	logical string
	ints    []int64
	strs    []string
}

// rawSectionBuilder writes a watermarks section from hand-made columns, to
// stand in for sections that this version of the builder never writes.
type rawSectionBuilder struct {
	columns []testColumn
}

func (b *rawSectionBuilder) Type() dataobj.SectionType { return sectionType }

func (b *rawSectionBuilder) Reset() { b.columns = nil }

func (b *rawSectionBuilder) Flush(w dataobj.SectionWriter) (int64, error) {
	var enc columnar.Encoder
	defer enc.Reset()

	for _, col := range b.columns {
		physical, encoding := datasetmd.PHYSICAL_TYPE_INT64, datasetmd.ENCODING_TYPE_DELTA
		if col.strs != nil {
			physical, encoding = datasetmd.PHYSICAL_TYPE_BINARY, datasetmd.ENCODING_TYPE_PLAIN
		}
		cb, err := dataset.NewColumnBuilder(col.tag, dataset.BuilderOptions{
			PageMaxRowCount: 100,
			Type:            dataset.ColumnType{Physical: physical, Logical: col.logical},
			Encoding:        encoding,
			Compression:     datasetmd.COMPRESSION_TYPE_NONE,
		})
		if err != nil {
			return 0, err
		}
		for i, v := range col.ints {
			if err := cb.Append(i, dataset.Int64Value(v)); err != nil {
				return 0, err
			}
		}
		for i, v := range col.strs {
			if err := cb.Append(i, dataset.BinaryValue([]byte(v))); err != nil {
				return 0, err
			}
		}
		if err := encodeColumn(&enc, ColumnTypeInvalid, cb); err != nil {
			return 0, err
		}
	}

	enc.SetTenant("tenant")
	return enc.Flush(w)
}

func buildRawObject(t *testing.T, columns ...testColumn) *dataobj.Object {
	t.Helper()

	ob := dataobj.NewBuilder(nil)
	require.NoError(t, ob.Append(&rawSectionBuilder{columns: columns}))
	obj, closer, err := ob.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	return obj
}

func TestOpen(t *testing.T) {
	topic := testColumn{tag: "topic", logical: "topic", strs: []string{"topic"}}
	partition := testColumn{tag: "partition", logical: "partition", ints: []int64{3}}
	watermark := testColumn{tag: "watermark", logical: "watermark", ints: []int64{9}}
	future := testColumn{tag: "future", logical: "future_field", ints: []int64{1}}

	t.Run("returns an error for a section of another type", func(t *testing.T) {
		obj := buildOtherSectionObject(t)

		_, err := Open(t.Context(), obj.Sections()[0])
		require.Error(t, err)
	})

	t.Run("skips a column of an unknown type and reads the known columns", func(t *testing.T) {
		obj := buildRawObject(t, topic, partition, watermark, future)

		sec, err := Open(t.Context(), obj.Sections()[0])
		require.NoError(t, err)
		require.Len(t, sec.inner.Columns(), 4)
		require.Len(t, sec.Columns(), 3)

		var got []Watermark
		for res := range Iter(t.Context(), obj) {
			wm, err := res.Value()
			require.NoError(t, err)
			got = append(got, wm)
		}
		require.Equal(t, []Watermark{{Topic: "topic", Partition: 3, Watermark: 9}}, got)
	})

	for name, columns := range map[string][]testColumn{
		"returns an error when the partition column is missing": {topic, watermark},
		"returns an error when the topic column is missing":     {partition, watermark},
		"returns an error when a column type appears twice":     {topic, partition, partition, watermark},
		"returns an error when no column has a known type":      {future},
	} {
		t.Run(name, func(t *testing.T) {
			obj := buildRawObject(t, columns...)

			_, err := Open(t.Context(), obj.Sections()[0])
			require.Error(t, err)
		})
	}
}
