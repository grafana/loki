package metastore

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestPostingsIndexSectionsReader_ReadBeforeOpenReturnsError(t *testing.T) {
	t.Parallel()

	r := newPostingsIndexSectionsReader(log.NewNopLogger(), nil, now, now, nil, nil, 0, nil)

	rec, err := r.Read(context.Background())
	require.ErrorIs(t, err, errIndexSectionsReaderNotOpen)
	require.Nil(t, rec)
}

func TestPostingsIndexSectionsReader_NoSelectorReturnsEOF(t *testing.T) {
	t.Parallel()

	r := newPostingsIndexSectionsReader(log.NewNopLogger(), nil, now, now, nil, nil, 0, nil)
	require.NoError(t, r.Open(context.Background()))

	rec, err := r.Read(context.Background())
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, rec)
}

func TestPostingsIndexSectionsReader_MissingOrgIDReturnsError(t *testing.T) {
	t.Parallel()

	obj := buildPostingsFixture(t)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-4*time.Hour), now.Add(-time.Hour), matchers, nil, 0, nil)

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

	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, nil, 0, nil)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	rec, err := r.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, int64(1), rec.NumRows(), "one matching stream row")
	require.Equal(t, int64(1), columnByName(t, rec, "stream_id.int64").(*array.Int64).Value(0))

	// The postings reader must emit the same schema the streams+pointers path produces.
	requirePostingsSchema(t, rec)

	// Subsequent reads return EOF once the resolved rows are drained (here the
	// single row fits in one batch).
	rec, err = r.Read(ctx)
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, rec)
}

// Postings spill across sections within an object, so a tenant can have more
// than one postings section. Reads must span all of them and combine the
// results.
func TestPostingsIndexSectionsReader_CombinesAcrossPostingsSections(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	obj := buildMultiPostingsSectionsFixture(t)
	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-4*time.Hour), now.Add(-time.Hour), matchers, nil, 0, nil)
	t.Cleanup(r.Close)

	require.NoError(t, r.Open(ctx))
	require.Len(t, r.postingsReaders, 2, "object has two postings sections ⇒ two readers")

	rec, err := r.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, int64(2), rec.NumRows(), "one pointer row from each postings section")

	pathCol := columnByName(t, rec, "path.path.utf8").(*array.String)
	gotPaths := map[string]struct{}{}
	for i := 0; i < int(rec.NumRows()); i++ {
		gotPaths[pathCol.Value(i)] = struct{}{}
	}
	require.Equal(t, map[string]struct{}{"test-path-1": {}, "test-path-2": {}}, gotPaths)

	rec, err = r.Read(ctx)
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, rec)
}

// readPointers must page its output in batchSize-row batches across successive
// Read calls, mirroring the streams+pointers path, rather than returning the
// whole result in one batch.
func TestPostingsIndexSectionsReader_PagesPointerOutput(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	// Two postings sections ⇒ two pointer rows for app=foo; batchSize=1 forces
	// two batches.
	obj := buildMultiPostingsSectionsFixture(t)
	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-4*time.Hour), now.Add(-time.Hour), matchers, nil, 1, nil)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	var batches, rows int64
	for {
		rec, err := r.Read(ctx)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		require.NotNil(t, rec)
		require.LessOrEqual(t, rec.NumRows(), int64(1), "each batch is bounded by batchSize=1")
		batches++
		rows += rec.NumRows()
	}

	require.Equal(t, int64(2), rows, "all matching pointer rows are emitted")
	require.Equal(t, int64(2), batches, "two rows at batchSize=1 ⇒ two batches")
}

// A single stream's label postings can be split across postings sections. A
// selector with one label in each section must still resolve the stream: the
// matcher AND has to be evaluated over all sections together, never per section
// (per-section ANDing would drop the stream in both sections and lose it).
func TestPostingsIndexSectionsReader_ANDMatchersAcrossPostingsSections(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
	}

	obj := buildPostingsANDAcrossSectionsFixture(t)
	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-4*time.Hour), now.Add(-time.Hour), matchers, nil, 0, nil)
	t.Cleanup(r.Close)

	require.NoError(t, r.Open(ctx))
	require.Len(t, r.postingsReaders, 2)

	rec, err := r.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, int64(1), rec.NumRows(), "stream matches both labels only when sections are ANDed together")
	require.Equal(t, int64(1), columnByName(t, rec, "stream_id.int64").(*array.Int64).Value(0))
	require.Equal(t, "test-path", columnByName(t, rec, "path.path.utf8").(*array.String).Value(0))

	rec, err = r.Read(ctx)
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, rec)
}

func TestPostingsIndexSectionsReader_NoPostingsSectionReturnsEOF(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	// The fixture has streams+pointers sections but no postings section.
	obj := buildStreamsPointersFixture(t)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-4*time.Hour), now.Add(-time.Hour), matchers, nil, 0, nil)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))
	require.Empty(t, r.postingsReaders, "no postings section ⇒ no postings readers")

	rec, err := r.Read(ctx)
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, rec)
	require.Empty(t, r.pointerRows, "no postings section ⇒ no matching streams")
}

func TestPostingsIndexSectionsReader_StreamLabelPredicateOnDisjointName(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)
	obj := buildPostingsMultiLabelFixture(t)

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}
	predicates := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "team", "ops")}

	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, predicates, 0, nil)
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

func TestPostingsIndexSectionsReader_StreamLabelPredicateOnlyOnNonMatchingStreams(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)
	obj := buildPostingsPredicateLabelOnlyOnNonMatchingStreamFixture(t)

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}
	predicates := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "team", "ops")}

	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, predicates, 0, nil)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	rec, err := r.Read(ctx)
	require.ErrorIs(t, err, io.EOF,
		"team=ops must not be dropped when team is absent on matched streams; keeping it should prune via bloom path")
	require.Nil(t, rec)
}

func TestPostingsIndexSectionsReader_StreamIDCollisionAcrossObjects(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)
	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	obj := buildPostingsStreamIDCollisionFixture(t)
	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, nil, 0, nil)
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
		r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, inWindowStart, inWindowEnd, appFoo, nil, 0, nil)
		t.Cleanup(r.Close)
		require.NoError(t, r.Open(ctx))
		require.NotEmpty(t, r.postingsReaders)

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
		r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-30*time.Minute), now, appFoo, nil, 0, nil)
		t.Cleanup(r.Close)
		require.NoError(t, r.Open(ctx))

		rec, err := r.Read(ctx)
		require.ErrorIs(t, err, io.EOF)
		require.Nil(t, rec)
	})

	t.Run("structured-metadata bloom selects the section", func(t *testing.T) {
		predicates := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "traceID", "abcd")}
		r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, inWindowStart, inWindowEnd, appFoo, predicates, 0, nil)
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
		r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, inWindowStart, inWindowEnd, appFoo, predicates, 0, nil)
		t.Cleanup(r.Close)
		require.NoError(t, r.Open(ctx))

		rec, err := r.Read(ctx)
		require.ErrorIs(t, err, io.EOF, "traceID miss ⇒ no section matched ⇒ EOF")
		require.Nil(t, rec)
	})

	t.Run("non-equal predicates are ignored for bloom filtering", func(t *testing.T) {
		predicates := []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "traceID", "abcd")}
		r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, inWindowStart, inWindowEnd, appFoo, predicates, 0, nil)
		t.Cleanup(r.Close)
		require.NoError(t, r.Open(ctx))

		rec, err := r.Read(ctx)
		require.NoError(t, err)
		require.NotNil(t, rec)
		require.Equal(t, int64(1), rec.NumRows())
	})
}

func TestPostingsIndexSectionsReader_RetryAfterBloomErrorStillFiltersPredicates(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)
	obj := buildPostingsOnlyFixture(t)

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}
	// This predicate intentionally misses so a correctly filtered retry returns EOF.
	predicates := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "traceID", "doesnotexist")}

	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, predicates, 0, nil)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))
	require.NoError(t, r.lazyResolveStreams(ctx))
	require.NotEmpty(t, r.pointerRows, "fixture should resolve at least one pointer row before bloom filtering")

	// Simulate a transient bloom-read failure by swapping in an unopened reader.
	originalReaders := r.postingsReaders
	require.NotEmpty(t, originalReaders, "fixture must open at least one postings reader")
	r.postingsReaders = []*postings.Reader{
		postings.NewReader(postings.ReaderOptions{
			Columns:   originalReaders[0].Columns(),
			Allocator: memory.DefaultAllocator,
		}),
	}

	rec, err := r.readPointers(ctx)
	require.Error(t, err, "first attempt should fail while bloom filtering")
	require.Nil(t, rec)

	// Contract for retry safety: bloom filtering must still be pending after an
	// error; otherwise a retry can leak unfiltered pointers.
	require.False(t, r.bloomFiltered, "failed bloom filtering must not mark bloom filtering as complete")

	// Restore healthy readers and retry.
	r.postingsReaders = originalReaders

	rec, err = r.readPointers(ctx)
	require.ErrorIs(t, err, io.EOF,
		"retry must still apply bloom predicates; traceID miss should prune all rows")
	require.Nil(t, rec)
}

// TestPointersRecordSchema_ParityWithPointersReader proves the batches built by
// buildPointersRecord carry the exact schema a pointers.Reader produces on the
// full 9-column stream-pointer projection, so the postings path is a drop-in
// replacement for the streams+pointers path.
func TestPointersRecordSchema_ParityWithPointersReader(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)
	obj := buildStreamsPointersFixture(t)

	var sec *pointers.Section
	for _, s := range obj.Sections() {
		if !pointers.CheckSection(s) {
			continue
		}
		opened, err := pointers.Open(ctx, s)
		require.NoError(t, err)
		sec = opened
		break
	}
	require.NotNil(t, sec, "pointers section missing from fixture")

	cols, err := findPointersColumnsByTypes(
		sec.Columns(),
		pointers.ColumnTypePath,
		pointers.ColumnTypeSection,
		pointers.ColumnTypePointerKind,
		pointers.ColumnTypeStreamID,
		pointers.ColumnTypeStreamIDRef,
		pointers.ColumnTypeMinTimestamp,
		pointers.ColumnTypeMaxTimestamp,
		pointers.ColumnTypeRowCount,
		pointers.ColumnTypeUncompressedSize,
	)
	require.NoError(t, err)

	r := pointers.NewReader(pointers.ReaderOptions{
		Columns:   cols,
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, r.Open(ctx))
	t.Cleanup(func() { _ = r.Close() })

	batch, err := r.Read(ctx, 128)
	if err != nil {
		require.ErrorIs(t, err, io.EOF, "Read may return a batch alongside io.EOF")
	}
	require.NotNil(t, batch)

	require.True(t, pointersRecordSchema().Equal(batch.Schema()),
		"buildPointersRecord schema must match the pointers.Reader schema byte-for-byte; got\n  postings=%s\n  pointers=%s",
		pointersRecordSchema(), batch.Schema())
}

// TestBuildPointersRecord_Values asserts the per-column values emitted for a
// resolved pointer row, including the postings-path invariants: pointer_kind is
// always PointerKindStreamIndex, stream_id_ref mirrors stream_id, row_count and
// uncompressed_size are zero, and the internal labels column is null.
func TestBuildPointersRecord_Values(t *testing.T) {
	t.Parallel()

	rows := []postings.PointerRow{{
		ObjectPath:   "obj-a",
		SectionIndex: 2,
		StreamID:     7,
		MinTimestamp: 100,
		MaxTimestamp: 200,
		HasBounds:    true,
	}}

	rec := buildPointersRecord(memory.DefaultAllocator, rows)
	require.Equal(t, int64(1), rec.NumRows())

	require.Equal(t, "obj-a", columnByName(t, rec, "path.path.utf8").(*array.String).Value(0))
	require.Equal(t, int64(2), columnByName(t, rec, "section.int64").(*array.Int64).Value(0))
	require.Equal(t, int64(pointers.PointerKindStreamIndex), columnByName(t, rec, "pointer_kind.int64").(*array.Int64).Value(0))
	require.Equal(t, int64(7), columnByName(t, rec, "stream_id.int64").(*array.Int64).Value(0))
	require.Equal(t, int64(7), columnByName(t, rec, "stream_id_ref.int64").(*array.Int64).Value(0))
	require.Equal(t, arrow.Timestamp(100), columnByName(t, rec, "min_timestamp.timestamp").(*array.Timestamp).Value(0))
	require.Equal(t, arrow.Timestamp(200), columnByName(t, rec, "max_timestamp.timestamp").(*array.Timestamp).Value(0))
	require.Equal(t, int64(0), columnByName(t, rec, "row_count.int64").(*array.Int64).Value(0))
	require.Equal(t, int64(0), columnByName(t, rec, "uncompressed_size.int64").(*array.Int64).Value(0))
	require.True(t, columnByName(t, rec, pointers.InternalLabelsFieldName).IsNull(0))
}

// requirePostingsSchema asserts rec carries the postings pointer-scan output
// schema (9 pointer columns + InternalLabelsFieldName), proving the postings reader
// emits the same schema as the streams+pointers reader.
func requirePostingsSchema(t *testing.T, rec arrow.RecordBatch) {
	t.Helper()

	require.Equal(t, 10, len(rec.Schema().Fields()),
		"postings pointer-scan output has 9 columns + InternalLabelsFieldName")

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

	first := postings.NewBuilder(nil, 0, 0, 1<<20)
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

	second := postings.NewBuilder(nil, 0, 0, 1<<20)
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

// buildPostingsANDAcrossSectionsFixture splits one stream's label postings
// across two postings sections: section 1 carries app=foo, section 2 carries
// env=prod, both for the same (object path, stream id). A selector matching
// both labels only resolves the stream if the sections are combined.
func buildPostingsANDAcrossSectionsFixture(t *testing.T) *dataobj.Object {
	t.Helper()

	first := postings.NewBuilder(nil, 0, 0, 1<<20)
	first.SetTenant(tenantID)
	first.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "test-path",
		SectionIndex:     0,
		ColumnName:       "app",
		LabelValue:       "foo",
		StreamID:         1,
		Timestamp:        now.Add(-3 * time.Hour),
		UncompressedSize: 5,
	})

	second := postings.NewBuilder(nil, 0, 0, 1<<20)
	second.SetTenant(tenantID)
	second.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "test-path",
		SectionIndex:     0,
		ColumnName:       "env",
		LabelValue:       "prod",
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

	builder := postings.NewBuilder(nil, 0, 0, 1<<20)
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

func buildPostingsPredicateLabelOnlyOnNonMatchingStreamFixture(t *testing.T) *dataobj.Object {
	t.Helper()

	builder := postings.NewBuilder(nil, 0, 0, 1<<20)
	builder.SetTenant(tenantID)

	// Matching stream for app=foo (no team label).
	builder.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "obj-a",
		SectionIndex:     0,
		ColumnName:       "app",
		LabelValue:       "foo",
		StreamID:         1,
		Timestamp:        now.Add(-3 * time.Hour),
		UncompressedSize: 5,
	})

	// Non-matching stream carries the team label.
	builder.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "obj-b",
		SectionIndex:     0,
		ColumnName:       "app",
		LabelValue:       "bar",
		StreamID:         2,
		Timestamp:        now.Add(-3 * time.Hour),
		UncompressedSize: 5,
	})
	builder.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "obj-b",
		SectionIndex:     0,
		ColumnName:       "team",
		LabelValue:       "ops",
		StreamID:         2,
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
