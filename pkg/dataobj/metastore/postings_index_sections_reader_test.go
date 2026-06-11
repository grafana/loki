package metastore

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestPostingsIndexSectionsReader_ReadBeforeOpenReturnsError(t *testing.T) {
	t.Parallel()

	r := newPostingsIndexSectionsReader(log.NewNopLogger(), nil, now, now, nil, nil)

	rec, err := r.Read(context.Background())
	require.ErrorIs(t, err, errIndexSectionsReaderNotOpen)
	require.Nil(t, rec)
}

func TestPostingsIndexSectionsReader_NoSelectorReturnsEOF(t *testing.T) {
	t.Parallel()

	r := newPostingsIndexSectionsReader(log.NewNopLogger(), nil, now, now, nil, nil)
	require.NoError(t, r.Open(context.Background()))

	rec, err := r.Read(context.Background())
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, rec)
}

func TestPostingsIndexSectionsReader_MissingOrgIDReturnsError(t *testing.T) {
	t.Parallel()

	obj := buildPostingsFixture(t)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-4*time.Hour), now.Add(-time.Hour), matchers, nil)

	// Context without org ID should fail during Open.
	require.Error(t, r.Open(context.Background()))
}

func TestPostingsIndexSectionsReader_ReadsPointers(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)
	obj := buildPostingsFixture(t)

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, nil)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))
	require.NotNil(t, r.postingsReader, "postings section present ⇒ one postings.Reader opened")

	rec, err := r.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, int64(1), rec.NumRows(), "one matching stream row")
	require.Equal(t, int64(1), columnByName(t, rec, "stream_id.int64").(*array.Int64).Value(0))

	// The postings reader must emit the same schema the streams+pointers path produces.
	requirePostingsSchema(t, rec)

	// Subsequent reads return EOF (pointers are read in a single batch).
	rec, err = r.Read(ctx)
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, rec)
}

func TestPostingsIndexSectionsReader_MultiplePostingsSectionsReturnsError(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	obj := buildMultiPostingsSectionsFixture(t)
	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-4*time.Hour), now.Add(-time.Hour), matchers, nil)

	err := r.Open(ctx)
	require.ErrorContains(t, err, "multiple postings sections found")
}

func TestPostingsIndexSectionsReader_NoPostingsSectionReturnsEOF(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	// The fixture has streams+pointers sections but no postings section.
	obj := buildStreamsPointersFixture(t)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-4*time.Hour), now.Add(-time.Hour), matchers, nil)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))
	require.Nil(t, r.postingsReader, "no postings section ⇒ no postings reader")

	rec, err := r.Read(ctx)
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, rec)
	require.Empty(t, r.matchingStreamRefs)
}

func TestPostingsIndexSectionsReader_StreamLabelPredicateOnDisjointName(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)
	obj := buildPostingsMultiLabelFixture(t)

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}
	predicates := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "team", "ops")}

	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, predicates)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	var total int64
	for {
		rec, err := r.Read(ctx)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if rec != nil {
			total += rec.NumRows()
		}
	}

	require.Greater(t, total, int64(0),
		"the stream-label predicate (team=ops) must be filtered out of the bloom check; "+
			"otherwise MatchSections AND-drops every section and Read returns EOF")
}

func TestPostingsIndexSectionsReader_StreamIDCollisionAcrossObjects(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)
	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	obj := buildPostingsStreamIDCollisionFixture(t)
	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, nil)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	rec, err := r.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, int64(1), rec.NumRows(), "only one object/stream should match app=foo")

	pathCol := columnByName(t, rec, "path.path.utf8").(*array.String)
	require.Equal(t, "obj-a", pathCol.Value(0),
		"stream-id collisions across objects must not leak pointers from non-matching objects")
}

func TestPostingsIndexSectionsReader_EndToEnd(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)
	obj := buildPostingsOnlyFixture(t)

	inWindowStart, inWindowEnd := now.Add(-4*time.Hour), now.Add(-time.Hour)
	appFoo := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	t.Run("selector resolves to matching streams from postings alone", func(t *testing.T) {
		r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, inWindowStart, inWindowEnd, appFoo, nil)
		t.Cleanup(r.Close)
		require.NoError(t, r.Open(ctx))
		require.NotNil(t, r.postingsReader)

		gotStreamIDs := map[int64]struct{}{}
		var rows int64
		for {
			rec, err := r.Read(ctx)
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			if rec == nil || rec.NumRows() == 0 {
				continue
			}
			rows += rec.NumRows()
			sid := columnByName(t, rec, "stream_id.int64").(*array.Int64)
			path := columnByName(t, rec, "path.path.utf8").(*array.String)
			section := columnByName(t, rec, "section.int64").(*array.Int64)
			for i := 0; i < int(rec.NumRows()); i++ {
				gotStreamIDs[sid.Value(i)] = struct{}{}
				require.Equal(t, "test-path", path.Value(i))
				require.Equal(t, int64(0), section.Value(i))
			}
		}

		require.Equal(t, int64(1), rows, "app=foo selects stream 1 only (stream 2 is app=bar)")
		require.Equal(t, map[int64]struct{}{1: {}}, gotStreamIDs)
	})

	t.Run("time window outside the data returns EOF", func(t *testing.T) {
		r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-30*time.Minute), now, appFoo, nil)
		t.Cleanup(r.Close)
		require.NoError(t, r.Open(ctx))

		rec, err := r.Read(ctx)
		require.ErrorIs(t, err, io.EOF)
		require.Nil(t, rec)
	})

	t.Run("structured-metadata bloom selects the section", func(t *testing.T) {
		predicates := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "traceID", "abcd")}
		r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, inWindowStart, inWindowEnd, appFoo, predicates)
		t.Cleanup(r.Close)
		require.NoError(t, r.Open(ctx))

		var rows int64
		for {
			rec, err := r.Read(ctx)
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			if rec != nil {
				rows += rec.NumRows()
			}
		}
		require.Equal(t, int64(1), rows, "traceID=abcd is in the section bloom ⇒ section kept")
	})

	t.Run("structured-metadata bloom miss prunes the section", func(t *testing.T) {
		predicates := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "traceID", "doesnotexist")}
		r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, inWindowStart, inWindowEnd, appFoo, predicates)
		t.Cleanup(r.Close)
		require.NoError(t, r.Open(ctx))

		rec, err := r.Read(ctx)
		require.ErrorIs(t, err, io.EOF, "traceID miss ⇒ no section matched ⇒ EOF")
		require.Nil(t, rec)
	})

	t.Run("non-equal predicates are ignored for bloom filtering", func(t *testing.T) {
		predicates := []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "traceID", "abcd")}
		r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, inWindowStart, inWindowEnd, appFoo, predicates)
		t.Cleanup(r.Close)
		require.NoError(t, r.Open(ctx))

		rec, err := r.Read(ctx)
		require.NoError(t, err)
		require.NotNil(t, rec)
		require.Equal(t, int64(1), rec.NumRows())
	})
}
