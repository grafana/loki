package metastore

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
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

// requirePostingsSchema asserts rec carries the postings.ReadPointersForStreams output
// schema (9 pointer columns + InternalLabelsFieldName), proving the postings reader
// emits the same schema as the streams+pointers reader.
func requirePostingsSchema(t *testing.T, rec arrow.RecordBatch) {
	t.Helper()

	require.Equal(t, 10, len(rec.Schema().Fields()),
		"postings.ReadPointersForStreams output has 9 columns + InternalLabelsFieldName")

	for _, name := range []string{
		"path.path.utf8",
		"section.int64",
		"pointer_kind.int64",
		"stream_id.int64",
		"stream_id_ref.int64",
		"min_timestamp.timestamp",
		"max_timestamp.timestamp",
		"row_count.int64",
		"uncompressed_size.int64",
	} {
		columnByName(t, rec, name) // fails the test if the column is absent
	}
}

func columnByName(t *testing.T, rec arrow.RecordBatch, name string) arrow.Array {
	t.Helper()
	for i, f := range rec.Schema().Fields() {
		if f.Name == name {
			return rec.Column(i)
		}
	}
	require.Failf(t, "missing column", "record batch has no column %q", name)
	return nil
}

func buildPostingsFixture(t *testing.T) *dataobj.Object {
	t.Helper()

	builder := newFixtureBuilder(t)

	_, err := builder.AppendStream(tenantID, streams.Stream{
		ID:               1,
		Labels:           labels.New(labels.Label{Name: "app", Value: "foo"}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
		Rows:             1,
	})
	require.NoError(t, err)

	builder.ObserveLabelPosting(tenantID, postings.LabelObservation{
		ObjectPath:       "test-path",
		SectionIndex:     0,
		ColumnName:       "app",
		LabelValue:       "foo",
		StreamID:         1,
		Timestamp:        now.Add(-3 * time.Hour),
		UncompressedSize: 5,
	})

	builder.PrepareBloomColumn(tenantID, "test-path", 0, "traceID", 1)
	require.NoError(t, builder.ObserveBloomPosting(tenantID, postings.BloomObservation{
		ObjectPath:       "test-path",
		SectionIndex:     0,
		ColumnName:       "traceID",
		Value:            "abcd",
		StreamID:         1,
		Timestamp:        now.Add(-3 * time.Hour),
		UncompressedSize: 5,
	}))

	return flushFixture(t, builder)
}

func buildMultiPostingsSectionsFixture(t *testing.T) *dataobj.Object {
	t.Helper()

	first := postings.NewBuilder(nil, 0, 0)
	first.SetTenant(tenantID)
	first.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "test-path-1",
		SectionIndex:     0,
		ColumnName:       "app",
		LabelValue:       "foo",
		StreamID:         1,
		Timestamp:        now.Add(-3 * time.Hour),
		UncompressedSize: 5,
	})

	second := postings.NewBuilder(nil, 0, 0)
	second.SetTenant(tenantID)
	second.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "test-path-2",
		SectionIndex:     0,
		ColumnName:       "app",
		LabelValue:       "foo",
		StreamID:         1,
		Timestamp:        now.Add(-3 * time.Hour),
		UncompressedSize: 5,
	})

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(first))
	require.NoError(t, objBuilder.Append(second))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	return obj
}

func buildPostingsMultiLabelFixture(t *testing.T) *dataobj.Object {
	t.Helper()

	builder := newFixtureBuilder(t)

	_, err := builder.AppendStream(tenantID, streams.Stream{
		ID: 1,
		Labels: labels.New(
			labels.Label{Name: "app", Value: "foo"},
			labels.Label{Name: "team", Value: "ops"},
		),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
		Rows:             1,
	})
	require.NoError(t, err)

	builder.ObserveLabelPosting(tenantID, postings.LabelObservation{
		ObjectPath:       "test-path",
		SectionIndex:     0,
		ColumnName:       "app",
		LabelValue:       "foo",
		StreamID:         1,
		Timestamp:        now.Add(-3 * time.Hour),
		UncompressedSize: 5,
	})
	builder.ObserveLabelPosting(tenantID, postings.LabelObservation{
		ObjectPath:       "test-path",
		SectionIndex:     0,
		ColumnName:       "team",
		LabelValue:       "ops",
		StreamID:         1,
		Timestamp:        now.Add(-3 * time.Hour),
		UncompressedSize: 5,
	})

	builder.PrepareBloomColumn(tenantID, "test-path", 0, "traceID", 1)
	require.NoError(t, builder.ObserveBloomPosting(tenantID, postings.BloomObservation{
		ObjectPath:       "test-path",
		SectionIndex:     0,
		ColumnName:       "traceID",
		Value:            "abcd",
		StreamID:         1,
		Timestamp:        now.Add(-3 * time.Hour),
		UncompressedSize: 5,
	}))

	return flushFixture(t, builder)
}

func buildPostingsStreamIDCollisionFixture(t *testing.T) *dataobj.Object {
	t.Helper()

	builder := postings.NewBuilder(nil, 0, 0)
	builder.SetTenant(tenantID)

	builder.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "obj-a",
		SectionIndex:     0,
		ColumnName:       "app",
		LabelValue:       "foo",
		StreamID:         1,
		Timestamp:        now.Add(-3 * time.Hour),
		UncompressedSize: 5,
	})
	builder.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "obj-b",
		SectionIndex:     0,
		ColumnName:       "app",
		LabelValue:       "bar",
		StreamID:         1,
		Timestamp:        now.Add(-3 * time.Hour),
		UncompressedSize: 5,
	})

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(builder))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	return obj
}

// buildPostingsOnlyFixture writes only postings observations (no AppendStream /
// ObserveLogLine), so Flush emits a postings section and no streams or pointers.
func buildPostingsOnlyFixture(t *testing.T) *dataobj.Object {
	t.Helper()

	builder := newFixtureBuilder(t)

	builder.ObserveLabelPosting(tenantID, postings.LabelObservation{
		ObjectPath: "test-path", SectionIndex: 0, ColumnName: "app", LabelValue: "foo",
		StreamID: 1, Timestamp: now.Add(-3 * time.Hour), UncompressedSize: 5,
	})
	builder.ObserveLabelPosting(tenantID, postings.LabelObservation{
		ObjectPath: "test-path", SectionIndex: 0, ColumnName: "app", LabelValue: "bar",
		StreamID: 2, Timestamp: now.Add(-3 * time.Hour), UncompressedSize: 5,
	})

	builder.PrepareBloomColumn(tenantID, "test-path", 0, "traceID", 1)
	require.NoError(t, builder.ObserveBloomPosting(tenantID, postings.BloomObservation{
		ObjectPath: "test-path", SectionIndex: 0, ColumnName: "traceID", Value: "abcd",
		StreamID: 1, Timestamp: now.Add(-3 * time.Hour), UncompressedSize: 5,
	}))

	return flushFixture(t, builder)
}
