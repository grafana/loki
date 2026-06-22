package metastore

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// buildPostingsIndexObject builds a *dataobj.Object containing a single tenant
// postings section with the given label postings.
func buildPostingsIndexObject(t *testing.T, tenant string, lps []postings.LabelObservation) (*dataobj.Object, func()) {
	t.Helper()

	b := postings.NewBuilder(nil, 0, 0, 1<<20)
	b.SetTenant(tenant)
	for _, lp := range lps {
		b.ObserveLabelPosting(lp)
	}

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	return obj, func() { _ = closer.Close() }
}

func TestPostingsIndexSectionsReader_ResolvesAndEmitsPointersBatch(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)

	ts := now.Add(-2 * time.Hour)
	obj, closer := buildPostingsIndexObject(t, tenantID, []postings.LabelObservation{
		{ObjectPath: "src-obj", SectionIndex: 3, ColumnName: "app", LabelValue: "nginx", StreamID: 7, Timestamp: ts, UncompressedSize: 0},
		{ObjectPath: "src-obj", SectionIndex: 3, ColumnName: "app", LabelValue: "loki", StreamID: 9, Timestamp: ts, UncompressedSize: 0},
	})
	defer closer()

	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")}
	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-4*time.Hour), now, matchers, nil, 8192)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	rec, err := r.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, int64(1), rec.NumRows())

	// The emitted batch must decode through the same path consumer A uses.
	buf := make([]pointers.SectionPointer, rec.NumRows())
	n, err := pointers.FromRecordBatch(rec, buf, pointers.PopulateSection)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, "src-obj", buf[0].Path)
	require.Equal(t, int64(3), buf[0].Section)
	require.Equal(t, int64(7), buf[0].StreamIDRef)

	// Drained.
	_, err = r.Read(ctx)
	require.ErrorIs(t, err, io.EOF)
}

func TestPostingsIndexSectionsReader_ZeroMatchersEOF(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)
	obj, closer := buildPostingsIndexObject(t, tenantID, []postings.LabelObservation{
		{ObjectPath: "src-obj", SectionIndex: 0, ColumnName: "app", LabelValue: "nginx", StreamID: 1, Timestamp: now, UncompressedSize: 0},
	})
	defer closer()

	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-time.Hour), now, nil, nil, 8192)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	rec, err := r.Read(ctx)
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, rec)
}

func TestPostingsIndexSectionsReader_ReadBeforeOpenErrors(t *testing.T) {
	r := newPostingsIndexSectionsReader(log.NewNopLogger(), nil, now, now, nil, nil, 8192)
	rec, err := r.Read(context.Background())
	require.ErrorIs(t, err, errIndexSectionsReaderNotOpen)
	require.Nil(t, rec)
}

func TestPostingsReader_ExpandsBitmapToStreamRows(t *testing.T) {
	results := []postings.SectionResult{{
		ObjectPath:     "obj-a",
		SectionIndex:   1,
		StreamBitmap:   []byte{0b0000_0110}, // streams 1, 2
		MinTimestamp:   100,
		MaxTimestamp:   400,
		AmbiguousNames: []string{"pod"},
	}}

	rows := expandResults(results)
	require.Len(t, rows, 2)
	require.Equal(t, int64(1), rows[0].streamID)
	require.Equal(t, int64(2), rows[1].streamID)
	for _, row := range rows {
		require.Equal(t, "obj-a", row.objectPath)
		require.Equal(t, int64(1), row.sectionIndex)
		require.Equal(t, int64(100), row.minTimestamp)
		require.Equal(t, int64(400), row.maxTimestamp)
		require.Equal(t, []string{"pod"}, row.ambiguousNames)
	}

	rec := sectionResultsToRecordBatch(rows)
	require.Equal(t, int64(2), rec.NumRows())
}
