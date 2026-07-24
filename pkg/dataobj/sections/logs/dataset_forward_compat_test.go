package logs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// TestMakeColumnarDataset_ForwardCompatUnknownColumn guards the sortmerge path,
// which lives in a package that can't import the internal columnar encoder and
// so can't build a section carrying an unknown column itself.
//
// It reproduces sortmerge's exact composition — MakeColumnarDataset, then a row
// reader feeding DecodeRow against Section.Columns() — over a section written
// by a newer Loki. Before the funnel fix, MakeColumnarDataset built from every
// physical column, so DecodeRow's positional lookup ran off the end of the
// recognized columns and panicked.
func TestMakeColumnarDataset_ForwardCompatUnknownColumn(t *testing.T) {
	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(&unknownColumnSectionBuilder{}))

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	sec, err := Open(t.Context(), obj.Sections()[0])
	require.NoError(t, err)

	require.Len(t, sec.inner.Columns(), 3, "physical section should carry the unknown column")
	require.Len(t, sec.Columns(), 2, "unknown column should be dropped from the typed view")

	dset, err := MakeColumnarDataset(sec)
	require.NoError(t, err)

	columns, err := result.Collect(dset.ListColumns(t.Context()))
	require.NoError(t, err)

	r := dataset.NewRowReader(dataset.RowReaderOptions{
		Dataset:  dset,
		Columns:  columns,
		Prefetch: true,
	})
	defer r.Close()
	require.NoError(t, r.Open(t.Context()))

	var got []Record
	seq := NewDatasetSequence(r, 128)
	for seq.Next() {
		row, err := seq.At().Value()
		require.NoError(t, err)

		var record Record
		require.NoError(t, DecodeRow(sec.Columns(), row, &record, nil))
		got = append(got, Record{StreamID: record.StreamID, Timestamp: record.Timestamp})
	}

	require.Len(t, got, 2)
	require.Equal(t, int64(1), got[0].StreamID)
	require.Equal(t, int64(2), got[1].StreamID)
	require.Equal(t, int64(10), got[0].Timestamp.UnixNano())
	require.Equal(t, int64(30), got[1].Timestamp.UnixNano())
}
