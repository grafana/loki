package postings_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestReader_ReadBeforeOpen(t *testing.T) {
	r := postings.NewReader(postings.ReaderOptions{
		Allocator: memory.DefaultAllocator,
	})
	rec, err := r.Read(context.Background(), 128)
	require.Nil(t, rec)
	require.ErrorContains(t, err, "reader not opened")
}

func TestReader_RoundTrip(t *testing.T) {
	// Build a tiny postings section with one label entry, read it back.
	b := postings.NewBuilder(nil, 0, 0)
	ts := time.Unix(0, 0).UTC()
	err := b.ObserveLabelPosting(postings.LabelObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 1, Timestamp: ts, UncompressedSize: 100})
	require.NoError(t, err)

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	var sec *postings.Section
	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		sec, err = postings.Open(context.Background(), s)
		require.NoError(t, err)
		break
	}
	require.NotNil(t, sec)

	r := postings.NewReader(postings.ReaderOptions{
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
	require.Equal(t, int64(postings.KindLabel), row["kind.int64"])
	require.Equal(t, "/obj", row["object_path.utf8"])
	require.Equal(t, int64(0), row["section_index.int64"])
	require.Equal(t, "env", row["column_name.utf8"])
	require.Equal(t, "prod", row["label_value.utf8"])
	require.Equal(t, int64(100), row["uncompressed_size.int64"])
	require.Equal(t, ts.UTC(), row["min_timestamp.timestamp"])
	require.Equal(t, ts.UTC(), row["max_timestamp.timestamp"])
}

// readTable drains a Reader into an arrow.Table, mirroring the helper in
// streams/reader_test.go.
func readTable(ctx context.Context, r *postings.Reader) (arrow.Table, error) {
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
