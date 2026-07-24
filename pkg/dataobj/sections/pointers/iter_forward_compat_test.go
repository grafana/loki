package pointers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// TestIter_ForwardCompatUnknownColumn ensures an older reader can iterate a
// pointers section written by a newer Loki that added a column this reader
// doesn't recognize, instead of panicking with an index-out-of-range in
// decodeRow.
func TestIter_ForwardCompatUnknownColumn(t *testing.T) {
	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(&unknownColumnSectionBuilder{}))

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	sec, err := Open(t.Context(), obj.Sections()[0])
	require.NoError(t, err)

	require.Len(t, sec.inner.Columns(), 3, "physical section should carry the unknown column")
	require.Len(t, sec.Columns(), 2, "unknown column should be dropped from the typed view")

	var got []SectionPointer
	for res := range Iter(t.Context(), obj) {
		p, err := res.Value()
		require.NoError(t, err)
		got = append(got, p)
	}

	require.Len(t, got, 2)
	require.Equal(t, "path1", got[0].Path)
	require.Equal(t, "path2", got[1].Path)
	require.Equal(t, int64(1), got[0].StreamID)
	require.Equal(t, int64(2), got[1].StreamID)
}

// unknownColumnSectionBuilder is a test-only [dataobj.SectionBuilder] that
// encodes a pointers section with path/stream_id columns plus one column whose
// logical type is unknown to the current reader, standing in for a section
// written by a newer version of Loki.
type unknownColumnSectionBuilder struct{}

func (b *unknownColumnSectionBuilder) Type() dataobj.SectionType { return sectionType }

func (b *unknownColumnSectionBuilder) Reset() {}

func (b *unknownColumnSectionBuilder) Flush(w dataobj.SectionWriter) (int64, error) {
	var enc columnar.Encoder
	defer enc.Reset()

	paths := []string{"path1", "path2"}
	streamIDs := []int64{1, 2}

	pathBuilder := newColumnBuilder("path", ColumnTypePath.String(), datasetmd.PHYSICAL_TYPE_BINARY)
	streamIDBuilder := newColumnBuilder("stream_id", ColumnTypeStreamID.String(), datasetmd.PHYSICAL_TYPE_INT64)
	// A column written by a newer Loki. Its logical type is unknown to
	// ParseColumnType, so an older reader drops it from Section.Columns().
	futureBuilder := newColumnBuilder("future_field", "future_field", datasetmd.PHYSICAL_TYPE_INT64)

	for i := range paths {
		_ = pathBuilder.Append(i, dataset.BinaryValue([]byte(paths[i])))
		_ = streamIDBuilder.Append(i, dataset.Int64Value(streamIDs[i]))
		_ = futureBuilder.Append(i, dataset.Int64Value(int64(i+1)))
	}

	// The unknown column is encoded last so it lands at a physical index beyond
	// the recognized columns, reproducing the index-out-of-range panic.
	if err := encodeTestColumns(&enc, pathBuilder, streamIDBuilder, futureBuilder); err != nil {
		return 0, err
	}

	enc.SetTenant("tenant-1")
	return enc.Flush(w)
}

func newColumnBuilder(name, logical string, physical datasetmd.PhysicalType) *dataset.ColumnBuilder {
	// INT64 columns require DELTA encoding; binary columns use PLAIN.
	encoding := datasetmd.ENCODING_TYPE_PLAIN
	if physical == datasetmd.PHYSICAL_TYPE_INT64 {
		encoding = datasetmd.ENCODING_TYPE_DELTA
	}
	b, err := dataset.NewColumnBuilder(name, dataset.BuilderOptions{
		PageMaxRowCount: 100,
		Type:            dataset.ColumnType{Physical: physical, Logical: logical},
		Encoding:        encoding,
		Compression:     datasetmd.COMPRESSION_TYPE_NONE,
	})
	if err != nil {
		panic(err)
	}
	return b
}

func encodeTestColumns(enc *columnar.Encoder, builders ...*dataset.ColumnBuilder) error {
	for _, b := range builders {
		column, err := b.Flush()
		if err != nil {
			return err
		}
		columnEnc, err := enc.OpenColumn(column.ColumnDesc())
		if err != nil {
			return err
		}
		for _, page := range column.Pages {
			if err := columnEnc.AppendPage(page); err != nil {
				return err
			}
		}
		if err := columnEnc.Commit(); err != nil {
			return err
		}
	}
	return nil
}
