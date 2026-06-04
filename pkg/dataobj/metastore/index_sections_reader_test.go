package metastore

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bits-and-blooms/bloom/v3"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// fixtureBuildMu serializes indexobj fixture builds across parallel tests: concurrent Builder.Flush
// calls race on the package-global defaultCompressionOptions.zstdWriter lazy init in
// dataset/page_compress_writer.go (a pre-existing race the mutex sidesteps).
var fixtureBuildMu sync.Mutex

func TestIndexSectionsReader_NoSelectorReturnsEOF(t *testing.T) {
	t.Parallel()

	r := newIndexSectionsReader(log.NewNopLogger(), nil, now, now, nil, nil, 8192, false)
	require.NoError(t, r.Open(context.Background()))

	rec, err := r.Read(context.Background())
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, rec)
}

func TestIndexSectionsReader_ReadBeforeOpenReturnsError(t *testing.T) {
	t.Parallel()

	r := newIndexSectionsReader(log.NewNopLogger(), nil, now, now, nil, nil, 8192, false)

	rec, err := r.Read(context.Background())
	require.ErrorIs(t, err, errIndexSectionsReaderNotOpen)
	require.Nil(t, rec)
}

func TestIndexSectionsReader_MissingOrgIDReturnsError(t *testing.T) {
	t.Parallel()

	builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	_, err = builder.AppendStream(tenantID, streams.Stream{
		ID:               1,
		Labels:           labels.New(labels.Label{Name: "app", Value: "foo"}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
	})
	require.NoError(t, err)
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5))
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-2*time.Hour), 0))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, nil, 8192, false)

	// Context without org ID should fail during Open.
	require.Error(t, r.Open(context.Background()))
}

func TestIndexSectionsReader_FiltersByStreamMatcherAndTime(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	_, err = builder.AppendStream(tenantID, streams.Stream{
		ID:               1,
		Labels:           labels.New(labels.Label{Name: "app", Value: "foo"}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
	})
	require.NoError(t, err)
	_, err = builder.AppendStream(tenantID, streams.Stream{
		ID:               2,
		Labels:           labels.New(labels.Label{Name: "app", Value: "bar"}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
	})
	require.NoError(t, err)

	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5))
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 2, 2, now.Add(-3*time.Hour), 5))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, nil, 8192, false)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	rec, err := r.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, int64(1), rec.NumRows())

	var streamIDCol *array.Int64
	for i, f := range rec.Schema().Fields() {
		if f.Name == "stream_id.int64" {
			streamIDCol = rec.Column(i).(*array.Int64)
			break
		}
	}
	require.NotNil(t, streamIDCol)
	require.Equal(t, int64(1), streamIDCol.Value(0))
}

func TestIndexSectionsReader_NoPredicatesPassthrough(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	_, err = builder.AppendStream(tenantID, streams.Stream{
		ID:               1,
		Labels:           labels.New(labels.Label{Name: "app", Value: "foo"}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
	})
	require.NoError(t, err)
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	// No predicates - should pass through all matching records
	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, nil, 8192, false)
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

	require.Equal(t, int64(1), total)
}

func TestIndexSectionsReader_IgnoresNonEqualPredicates(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	_, err = builder.AppendStream(tenantID, streams.Stream{
		ID:               1,
		Labels:           labels.New(labels.Label{Name: "app", Value: "foo"}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
	})
	require.NoError(t, err)
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}
	// Regex predicates should be ignored (only MatchEqual is used for bloom filtering)
	predicates := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchRegexp, "traceID", "abcd"),
	}

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, predicates, 8192, false)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	rec, err := r.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, int64(1), rec.NumRows())
}

func TestIndexSectionsReader_FiltersByBloomOnSectionKey(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	_, err = builder.AppendStream(tenantID, streams.Stream{
		ID:               1,
		Labels:           labels.New(labels.Label{Name: "app", Value: "foo"}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
	})
	require.NoError(t, err)
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5))
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-2*time.Hour), 0))

	traceBloomBytes := newTestBloomBytes(t, "abcd")
	require.NoError(t, builder.AppendColumnIndex(tenantID, "test-path", 0, "traceID", 0, traceBloomBytes))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}
	predicates := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "traceID", "abcd"),
	}

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, predicates, 8192, false)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	rec, err := r.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.GreaterOrEqual(t, rec.NumRows(), int64(1))

	// Verify the path column contains "test-path"
	var pathCol *array.String
	for i, f := range rec.Schema().Fields() {
		if f.Name == "path.path.utf8" {
			pathCol = rec.Column(i).(*array.String)
			break
		}
	}
	require.NotNil(t, pathCol)
	require.Equal(t, "test-path", pathCol.Value(0))
}

func TestIndexSectionsReader_PredicateMissReturnsEOF(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	_, err = builder.AppendStream(tenantID, streams.Stream{
		ID:               1,
		Labels:           labels.New(labels.Label{Name: "app", Value: "foo"}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
	})
	require.NoError(t, err)
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5))
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-2*time.Hour), 0))

	traceBloomBytes := newTestBloomBytes(t, "abcd")
	require.NoError(t, builder.AppendColumnIndex(tenantID, "test-path", 0, "traceID", 0, traceBloomBytes))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}
	// Predicate value that doesn't exist in bloom filter
	predicates := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "traceID", "doesnotexist"),
	}

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, predicates, 8192, false)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	rec, err := r.Read(ctx)
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, rec)
}

func TestIndexSectionsReader_LabelPredicatesFiltered(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	// Create a stream with label app=foo
	_, err = builder.AppendStream(tenantID, streams.Stream{
		ID:               1,
		Labels:           labels.New(labels.Label{Name: "app", Value: "foo"}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
	})
	require.NoError(t, err)
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5))
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-2*time.Hour), 0))

	// Add a bloom filter for a metadata column (traceID), NOT for the stream label (app)
	traceIDBloomBytes := newTestBloomBytes(t, "abcd")
	require.NoError(t, builder.AppendColumnIndex(tenantID, "test-path", 0, "traceID", 0, traceIDBloomBytes))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	// This predicate is on a stream label, not a metadata column.
	// Without the label filtering fix, the bloom filter would not find "foo" in the traceID bloom
	// and incorrectly filter out this section.
	predicates := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
	}

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, predicates, 8192, false)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	// Should return results because the "app" predicate should be filtered out
	// (it's a stream label, not structured metadata)
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

	require.Greater(t, total, int64(0), "expected results to be returned when predicate is on stream label")
}

func TestIndexSectionsReader_MultipleBlooms(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)

	builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	_, err = builder.AppendStream(tenantID, streams.Stream{
		ID:               1,
		Labels:           labels.New(labels.Label{Name: "app", Value: "foo"}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
	})
	require.NoError(t, err)
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5))
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-2*time.Hour), 0))

	traceBloomBytes := newTestBloomBytes(t, "abcd")
	require.NoError(t, builder.AppendColumnIndex(tenantID, "test-path", 0, "traceID", 0, traceBloomBytes))

	userBloomBytes := newTestBloomBytes(t, "user-123")
	require.NoError(t, builder.AppendColumnIndex(tenantID, "test-path", 0, "userID", 1, userBloomBytes))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	tests := []struct {
		name        string
		userIDValue string
		expectEOF   bool
	}{
		{
			name:        "all predicates match",
			userIDValue: "user-123",
			expectEOF:   false,
		},
		{
			name:        "one predicate misses",
			userIDValue: "user-999",
			expectEOF:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newIndexSectionsReader(
				log.NewNopLogger(),
				obj,
				now.Add(-4*time.Hour),
				now.Add(-time.Hour),
				[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")},
				[]*labels.Matcher{
					labels.MustNewMatcher(labels.MatchEqual, "traceID", "abcd"),
					labels.MustNewMatcher(labels.MatchEqual, "userID", tc.userIDValue),
				},
				8192,
				false,
			)
			t.Cleanup(r.Close)
			require.NoError(t, r.Open(ctx))

			rec, err := r.Read(ctx)
			if tc.expectEOF {
				require.ErrorIs(t, err, io.EOF)
				require.Nil(t, rec)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, rec)
			require.GreaterOrEqual(t, rec.NumRows(), int64(1))
		})
	}
}

func TestIndexSectionsReader_Read_SkipsNilStreamsReader(t *testing.T) {
	t.Parallel()

	r := newIndexSectionsReader(log.NewNopLogger(), nil, now, now, nil, nil, 8192, false)
	r.initialized = true
	r.streamsReaders = []*streams.Reader{nil}
	t.Cleanup(r.Close)

	var (
		rec arrow.RecordBatch
		err error
	)
	require.NotPanics(t, func() {
		rec, err = r.Read(context.Background())
	})
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, rec)
}

func TestIndexSectionsReader_Read_SkipsNilPointersReader(t *testing.T) {
	t.Parallel()

	r := newIndexSectionsReader(log.NewNopLogger(), nil, now, now, nil, nil, 8192, false)
	r.initialized = true
	r.readStreams = true
	r.hasData = true
	r.pointersReaders = []*pointers.Reader{nil}
	t.Cleanup(r.Close)

	var (
		rec arrow.RecordBatch
		err error
	)
	require.NotPanics(t, func() {
		rec, err = r.Read(context.Background())
	})
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, rec)
}

func TestIndexSectionsReader_ReadMatchedSectionKeys_SkipsNilBloomReader(t *testing.T) {
	t.Parallel()

	r := newIndexSectionsReader(
		log.NewNopLogger(),
		nil,
		now,
		now,
		nil,
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "traceID", "abcd")},
		8192,
		false,
	)
	r.bloomReaders = []*pointers.Reader{nil}

	var (
		matched map[SectionKey]struct{}
		err     error
	)
	require.NotPanics(t, func() {
		matched, err = r.readMatchedSectionKeys(context.Background())
	})
	require.NoError(t, err)
	require.Empty(t, matched)
}

func newTestBloomBytes(t *testing.T, vals ...string) []byte {
	t.Helper()

	traceBloom := bloom.NewWithEstimates(uint(len(vals)), 0.01)
	for _, v := range vals {
		traceBloom.AddString(v)
	}

	bytes, err := traceBloom.MarshalBinary()
	require.NoError(t, err)
	return bytes
}

func TestIndexSectionsReader_PathSelection(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)
	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	const (
		streamsPointersPath = "streams+pointers"
		postingsPath        = "postings"
	)

	tests := []struct {
		name     string
		buildObj func(*testing.T) *dataobj.Object
		gate     bool
		wantPath string
	}{
		{"streams+pointers object stays on streams+pointers path", buildStreamsPointersFixture, true, streamsPointersPath},
		{"gate off on combined fixture runs streams+pointers path", buildCombinedFixture, false, streamsPointersPath},
		{"postings object uses postings path", buildPostingsFixture, true, postingsPath},
		{"combined section object favors postings", buildCombinedFixture, true, postingsPath},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := newIndexSectionsReader(log.NewNopLogger(), tc.buildObj(t), start, end, matchers, nil, 8192, false)
			r.usePostingsSections = tc.gate
			t.Cleanup(r.Close)
			require.NoError(t, r.Open(ctx))

			switch tc.wantPath {
			case streamsPointersPath:
				require.Empty(t, r.postingsReaders, "streams+pointers path ⇒ no postings readers opened")
				require.NotEmpty(t, r.pointersReaders, "streams+pointers path ⇒ pointers readers populated")
			case postingsPath:
				require.Empty(t, r.pointersReaders, "postings path ⇒ pointers slice empty even when the section exists")
				require.Empty(t, r.bloomReaders, "postings path ⇒ bloom slice empty even when the section exists")
				require.Len(t, r.postingsReaders, 1, "postings path ⇒ exactly one postings.Reader opened")
				require.NotNil(t, r.postingsReaders[0], "wave-1 errgroup populated the reader")
			}

			rec, err := r.Read(ctx)
			require.NoError(t, err)
			require.NotNil(t, rec)
			require.Equal(t, int64(1), rec.NumRows(), "one matching stream row")
			require.Equal(t, int64(1), columnByName(t, rec, "stream_id.int64").(*array.Int64).Value(0))

			// The postings path must emit the same schema the streams+pointers path produces.
			if tc.wantPath == postingsPath {
				requirePostingsSchema(t, rec)
			}
		})
	}
}

// requirePostingsSchema asserts rec carries the postings.ReadPointers output
// schema (9 pointer columns + InternalLabelsFieldName), proving the postings path
// emits the same schema as the streams+pointers path.
func requirePostingsSchema(t *testing.T, rec arrow.RecordBatch) {
	t.Helper()

	require.Equal(t, 10, len(rec.Schema().Fields()),
		"postings.ReadPointers output has 9 columns + InternalLabelsFieldName")

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

// newFixtureBuilder returns an indexobj.Builder configured for the reader fixtures.
func newFixtureBuilder(t *testing.T) *indexobj.Builder {
	t.Helper()

	builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)
	return builder
}

// flushFixture flushes builder under fixtureBuildMu (which guards the racy
// zstdWriter lazy init reached from Flush; see fixtureBuildMu) and registers the
// returned object's closer for cleanup.
func flushFixture(t *testing.T, builder *indexobj.Builder) *dataobj.Object {
	t.Helper()

	fixtureBuildMu.Lock()
	obj, closer, err := builder.Flush()
	fixtureBuildMu.Unlock()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	return obj
}

func buildStreamsPointersFixture(t *testing.T) *dataobj.Object {
	t.Helper()

	builder := newFixtureBuilder(t)

	_, err := builder.AppendStream(tenantID, streams.Stream{
		ID:               1,
		Labels:           labels.New(labels.Label{Name: "app", Value: "foo"}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
	})
	require.NoError(t, err)
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5))

	return flushFixture(t, builder)
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

func buildCombinedFixture(t *testing.T) *dataobj.Object {
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

	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5))

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

func TestIndexSectionsReader_PostingsPath_StreamLabelPredicateOnDisjointName(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	obj := buildPostingsMultiLabelFixture(t)

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}
	predicates := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "team", "ops")}

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, predicates, 8192, false)
	r.usePostingsSections = true
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	require.Len(t, r.postingsReaders, 1, "postings path: one postings reader opened")

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
		"postings path must filter the stream-label predicate (team=ops) out of the bloom check; "+
			"without the  fix, MatchSections AND-drops every section and Read returns EOF")
}

func TestIndexSectionsReader_PostingsPath_NoPostingsReader_ShortCircuit(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	obj := buildStreamsPointersFixture(t)

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, nil, 8192, false)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))
	require.Empty(t, r.postingsReaders, "no postings section ⇒ empty slice")

	// Flip the gate on post-Open (with no postings reader) to hit the defensive short-circuit.
	r.usePostingsSections = true

	_, err := r.Read(ctx)
	require.ErrorIs(t, err, io.EOF, "no postings reader ⇒ postings path returns io.EOF cleanly")
	require.Empty(t, r.matchingStreamIDs, "short-circuit must not populate matchingStreamIDs")
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

func TestIndexSectionsReader_PostingsOnly_EndToEnd(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)
	obj := buildPostingsOnlyFixture(t)

	inWindowStart, inWindowEnd := now.Add(-4*time.Hour), now.Add(-time.Hour)
	appFoo := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	t.Run("object has only a postings section", func(t *testing.T) {
		var nStreams, nPointers, nPostings int
		for _, s := range obj.Sections() {
			switch {
			case streams.CheckSection(s):
				nStreams++
			case pointers.CheckSection(s):
				nPointers++
			case postings.CheckSection(s):
				nPostings++
			}
		}
		require.Zero(t, nStreams, "fixture must not write a streams section")
		require.Zero(t, nPointers, "fixture must not write a pointers section")
		require.GreaterOrEqual(t, nPostings, 1, "fixture must write a postings section")
	})

	t.Run("selector resolves to matching streams from postings alone", func(t *testing.T) {
		r := newIndexSectionsReader(log.NewNopLogger(), obj, inWindowStart, inWindowEnd, appFoo, nil, 8192, false)
		r.usePostingsSections = true
		t.Cleanup(r.Close)
		require.NoError(t, r.Open(ctx))

		require.Empty(t, r.streamsReaders)
		require.Empty(t, r.pointersReaders)
		require.Empty(t, r.bloomReaders)
		require.Len(t, r.postingsReaders, 1)

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
		r := newIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-30*time.Minute), now, appFoo, nil, 8192, false)
		r.usePostingsSections = true
		t.Cleanup(r.Close)
		require.NoError(t, r.Open(ctx))

		rec, err := r.Read(ctx)
		require.ErrorIs(t, err, io.EOF)
		require.Nil(t, rec)
	})

	t.Run("structured-metadata bloom selects the section", func(t *testing.T) {
		predicates := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "traceID", "abcd")}
		r := newIndexSectionsReader(log.NewNopLogger(), obj, inWindowStart, inWindowEnd, appFoo, predicates, 8192, false)
		r.usePostingsSections = true
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
		r := newIndexSectionsReader(log.NewNopLogger(), obj, inWindowStart, inWindowEnd, appFoo, predicates, 8192, false)
		r.usePostingsSections = true
		t.Cleanup(r.Close)
		require.NoError(t, r.Open(ctx))

		rec, err := r.Read(ctx)
		require.ErrorIs(t, err, io.EOF, "traceID miss ⇒ no section matched ⇒ EOF")
		require.Nil(t, rec)
	})
}
