package indexpointers

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// TestIter_ForwardCompatUnknownColumn reproduces the panic that occurs when an
// older reader iterates an indexpointers section written by a newer Loki that
// added columns the reader doesn't recognize (e.g. file_size /
// uncompressed_logs_size from PR #23331).
//
// The section is built with the three long-standing columns plus one extra
// column carrying a logical type unknown to [ParseColumnType]. A reader that
// predates the new column must skip it and still decode the known columns
// rather than panicking with an index-out-of-range in decodeRow.
func TestIter_ForwardCompatUnknownColumn(t *testing.T) {
	pointers := []IndexPointer{
		{Path: "path1", StartTs: unixTime(10), EndTs: unixTime(20)},
		{Path: "path2", StartTs: unixTime(30), EndTs: unixTime(40)},
	}

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(&unknownColumnSectionBuilder{tenant: "tenant-1", pointers: pointers}))

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	sec, err := Open(t.Context(), obj.Sections()[0])
	require.NoError(t, err)

	// The physical section carries the unknown column, but the typed view drops
	// it. This length mismatch is exactly what makes decodeRow's positional
	// lookup run off the end of the recognized-columns slice.
	require.Len(t, sec.inner.Columns(), 4, "physical section should carry the unknown column")
	require.Len(t, sec.Columns(), 3, "unknown column should be dropped from the typed view")

	var got []IndexPointer
	for res := range Iter(t.Context(), obj) {
		tip, err := res.Value()
		require.NoError(t, err)
		got = append(got, tip.IndexPointer)
	}

	require.Len(t, got, len(pointers))
	for i, want := range pointers {
		require.Equal(t, want.Path, got[i].Path)
		require.Equal(t, want.StartTs.UnixNano(), got[i].StartTs.UnixNano())
		require.Equal(t, want.EndTs.UnixNano(), got[i].EndTs.UnixNano())
		require.Zero(t, got[i].FileSize)
		require.Zero(t, got[i].UncompressedLogsSize)
	}
}

// unknownColumnSectionBuilder is a test-only [dataobj.SectionBuilder] that
// encodes an indexpointers section with the standard path/min/max columns plus
// one extra column whose logical type is unknown to the current reader. It
// stands in for a section written by a newer version of Loki.
type unknownColumnSectionBuilder struct {
	tenant   string
	pointers []IndexPointer
}

func (b *unknownColumnSectionBuilder) Type() dataobj.SectionType { return sectionType }

func (b *unknownColumnSectionBuilder) Reset() { b.pointers = nil }

func (b *unknownColumnSectionBuilder) Flush(w dataobj.SectionWriter) (int64, error) {
	var enc columnar.Encoder
	defer enc.Reset()

	int64Column := func(name, logical string) (*dataset.ColumnBuilder, error) {
		return dataset.NewColumnBuilder(name, dataset.BuilderOptions{
			PageMaxRowCount: 100,
			Type:            dataset.ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: logical},
			Encoding:        datasetmd.ENCODING_TYPE_DELTA,
			Compression:     datasetmd.COMPRESSION_TYPE_NONE,
		})
	}

	pathBuilder, err := dataset.NewColumnBuilder("path", dataset.BuilderOptions{
		PageMaxRowCount: 100,
		Type:            dataset.ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: ColumnTypePath.String()},
		Encoding:        datasetmd.ENCODING_TYPE_PLAIN,
		Compression:     datasetmd.COMPRESSION_TYPE_ZSTD,
	})
	if err != nil {
		return 0, err
	}
	minBuilder, err := int64Column("min_timestamp", ColumnTypeMinTimestamp.String())
	if err != nil {
		return 0, err
	}
	maxBuilder, err := int64Column("max_timestamp", ColumnTypeMaxTimestamp.String())
	if err != nil {
		return 0, err
	}
	// A column written by a newer Loki. Its logical type is not known to
	// ParseColumnType, so an older reader drops it from Section.Columns().
	futureBuilder, err := int64Column("future_field", "future_field")
	if err != nil {
		return 0, err
	}

	for i, p := range b.pointers {
		_ = pathBuilder.Append(i, dataset.BinaryValue([]byte(p.Path)))
		_ = minBuilder.Append(i, dataset.Int64Value(p.StartTs.UnixNano()))
		_ = maxBuilder.Append(i, dataset.Int64Value(p.EndTs.UnixNano()))
		_ = futureBuilder.Append(i, dataset.Int64Value(int64(i+1)))
	}

	// The unknown column is encoded last so it lands at a physical index beyond
	// the recognized columns, reproducing "index out of range [3] with length 3".
	errs := []error{
		encodeColumn(&enc, ColumnTypePath, pathBuilder),
		encodeColumn(&enc, ColumnTypeMinTimestamp, minBuilder),
		encodeColumn(&enc, ColumnTypeMaxTimestamp, maxBuilder),
		encodeColumn(&enc, ColumnTypeInvalid, futureBuilder),
	}
	if err := errors.Join(errs...); err != nil {
		return 0, err
	}

	enc.SetTenant(b.tenant)
	return enc.Flush(w)
}
