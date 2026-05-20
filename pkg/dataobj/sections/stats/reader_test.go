package stats_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestReader_ReadBeforeOpen(t *testing.T) {
	r := stats.NewReader(stats.ReaderOptions{
		Allocator: memory.DefaultAllocator,
	})
	rec, err := r.Read(context.Background(), 128)
	require.Nil(t, rec)
	require.ErrorContains(t, err, "reader not opened")
}

func TestReader_RoundTrip(t *testing.T) {
	b := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(1024*1024, 10000))
	b.SetTenant("test-tenant")
	b.Append(stats.Stat{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		SortSchema:       "service_name",
		Labels:           map[string]string{"service_name": "svc"},
		MinTimestamp:     100,
		MaxTimestamp:     200,
		RowCount:         5,
		UncompressedSize: 50,
	})

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	var sec *stats.Section
	for _, s := range obj.Sections() {
		if !stats.CheckSection(s) {
			continue
		}
		sec, err = stats.Open(context.Background(), s)
		require.NoError(t, err)
		break
	}
	require.NotNil(t, sec)

	r := stats.NewReader(stats.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, r.Open(context.Background()))
	t.Cleanup(func() { _ = r.Close() })

	tbl, err := readTable(context.Background(), r)
	require.NoError(t, err)
	actual, err := arrowtest.TableRows(memory.DefaultAllocator, tbl)
	require.NoError(t, err)

	require.Len(t, actual, 1)
	row := actual[0]
	require.Equal(t, "/obj", row["object_path.utf8"])
	require.Equal(t, "service_name", row["sort_schema.utf8"])
	require.Equal(t, int64(5), row["row_count.int64"])
	require.Equal(t, int64(50), row["uncompressed_size.int64"])
	require.Equal(t, "svc", row["service_name.label.utf8"])
}

// readTable drains a Reader into an arrow.Table, mirroring the helper in
// streams/reader_test.go.
func readTable(ctx context.Context, r *stats.Reader) (arrow.Table, error) {
	var recs []arrow.RecordBatch
	for {
		rec, err := r.Read(ctx, 128)
		if rec != nil && rec.NumRows() > 0 {
			recs = append(recs, rec)
		}
		if err != nil && errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
	}
	if len(recs) == 0 {
		return nil, io.EOF
	}
	return array.NewTableFromRecords(recs[0].Schema(), recs), nil
}
