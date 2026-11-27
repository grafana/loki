package postings

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	lokimemory "github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestReader_ReadBeforeOpen(t *testing.T) {
	r := NewReader(ReaderOptions{})
	rec, err := r.Read(context.Background(), lokimemory.NewAllocator(nil), 128)
	require.Nil(t, rec)
	require.ErrorContains(t, err, "reader not opened")
}

func TestReader_RoundTrip(t *testing.T) {
	// Build a tiny postings section with one label entry, read it back.
	b := NewBuilder(nil, 0, 0, 1<<20)
	ts := time.Unix(0, 0).UTC()
	b.ObserveLabelPosting(LabelObservation{ObjectPath: "/obj", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 1, Time: At(ts), UncompressedSize: 100})

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	var sec *Section
	for _, s := range obj.Sections() {
		if !CheckSection(s) {
			continue
		}
		sec, err = Open(context.Background(), s)
		require.NoError(t, err)
		break
	}
	require.NotNil(t, sec)

	r := NewReader(ReaderOptions{
		Columns: sec.Columns(),
	})
	require.NoError(t, r.Open(context.Background()))
	t.Cleanup(func() { _ = r.Close() })

	tbl, err := readTable(context.Background(), r)
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(memory.DefaultAllocator, tbl)
	require.NoError(t, err)

	require.Len(t, actual, 1)
	row := actual[0]
	require.Equal(t, int64(KindLabel), row["kind.int64"])
	require.Equal(t, "/obj", row["object_path.utf8"])
	require.Equal(t, int64(0), row["section_index.int64"])
	require.Equal(t, "env", row["column_name.utf8"])
	require.Equal(t, "prod", row["label_value.utf8"])
	require.Equal(t, int64(100), row["uncompressed_size.int64"])
	require.Equal(t, ts.UTC(), row["min_timestamp.timestamp"])
	require.Equal(t, ts.UTC(), row["max_timestamp.timestamp"])
	require.Equal(t, int64(0), row["min_shard_bucket.int64"])
	require.Equal(t, int64(0), row["max_shard_bucket.int64"])
}
