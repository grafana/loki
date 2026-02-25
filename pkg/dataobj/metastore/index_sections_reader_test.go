package metastore

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bits-and-blooms/bloom/v3"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestIndexSectionsReader_NoSelectorReturnsEOF(t *testing.T) {
	t.Parallel()

	r := newIndexSectionsReader(log.NewNopLogger(), nil, now, now, nil, nil)
	require.NoError(t, r.Open(context.Background()))

	rec, err := r.Read(context.Background())
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, rec)
}

func TestIndexSectionsReader_ReadBeforeOpenReturnsError(t *testing.T) {
	t.Parallel()

	r := newIndexSectionsReader(log.NewNopLogger(), nil, now, now, nil, nil)

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

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, nil)

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

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, nil)
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
	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, nil)
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

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, predicates)
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

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, predicates)
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

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, predicates)
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

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, predicates)
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

	r := newIndexSectionsReader(log.NewNopLogger(), nil, now, now, nil, nil)
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

	r := newIndexSectionsReader(log.NewNopLogger(), nil, now, now, nil, nil)
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
