package streams

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// TestIter_ForwardCompatUnknownColumn ensures an older reader can iterate a
// streams section written by a newer Loki that added a column this reader
// doesn't recognize, instead of panicking with an index-out-of-range in
// decodeRow.
func TestIter_ForwardCompatUnknownColumn(t *testing.T) {
	obj := buildObjectWithUnknownColumn(t, &unknownColumnSectionBuilder{})

	sec, err := Open(t.Context(), obj.Sections()[0])
	require.NoError(t, err)

	require.Len(t, sec.inner.Columns(), 3, "physical section should carry the unknown column")
	require.Len(t, sec.Columns(), 2, "unknown column should be dropped from the typed view")

	var got []Stream
	for res := range Iter(t.Context(), obj) {
		s, err := res.Value()
		require.NoError(t, err)
		got = append(got, s)
	}

	require.Len(t, got, 2)
	require.Equal(t, int64(1), got[0].ID)
	require.Equal(t, int64(2), got[1].ID)
	require.Equal(t, int64(10), got[0].MinTimestamp.UnixNano())
	require.Equal(t, int64(30), got[1].MinTimestamp.UnixNano())
}

func buildObjectWithUnknownColumn(t *testing.T, sb dataobj.SectionBuilder) *dataobj.Object {
	t.Helper()

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(sb))

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	return obj
}

// unknownColumnSectionBuilder is a test-only [dataobj.SectionBuilder] that
// encodes a streams section with stream_id/min_timestamp columns plus one
// column whose logical type is unknown to the current reader, standing in for
// a section written by a newer version of Loki.
type unknownColumnSectionBuilder struct{}

func (b *unknownColumnSectionBuilder) Type() dataobj.SectionType { return sectionType }

func (b *unknownColumnSectionBuilder) Reset() {}

func (b *unknownColumnSectionBuilder) Flush(w dataobj.SectionWriter) (int64, error) {
	var enc columnar.Encoder
	defer enc.Reset()

	streamIDs := []int64{1, 2}
	minTimestamps := []int64{10, 30}

	streamIDBuilder := newInt64ColumnBuilder("stream_id", ColumnTypeStreamID.String())
	minTimestampBuilder := newInt64ColumnBuilder("min_timestamp", ColumnTypeMinTimestamp.String())
	// A column written by a newer Loki. Its logical type is unknown to
	// ParseColumnType, so an older reader drops it from Section.Columns().
	futureBuilder := newInt64ColumnBuilder("future_field", "future_field")

	for i := range streamIDs {
		_ = streamIDBuilder.Append(i, dataset.Int64Value(streamIDs[i]))
		_ = minTimestampBuilder.Append(i, dataset.Int64Value(minTimestamps[i]))
		_ = futureBuilder.Append(i, dataset.Int64Value(int64(i+1)))
	}

	// The unknown column is encoded last so it lands at a physical index beyond
	// the recognized columns, reproducing the index-out-of-range panic.
	if err := encodeTestColumns(&enc, streamIDBuilder, minTimestampBuilder, futureBuilder); err != nil {
		return 0, err
	}

	enc.SetTenant("tenant-1")
	return enc.Flush(w)
}

func newInt64ColumnBuilder(name, logical string) *dataset.ColumnBuilder {
	b, err := dataset.NewColumnBuilder(name, dataset.BuilderOptions{
		PageMaxRowCount: 100,
		Type:            dataset.ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: logical},
		Encoding:        datasetmd.ENCODING_TYPE_DELTA,
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
