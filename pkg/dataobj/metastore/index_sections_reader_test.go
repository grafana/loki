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

// fixtureBuildMu serializes indexobj fixture construction across parallel
// tests. Without it, concurrent indexobj.Builder.Flush calls trigger a
// pre-existing data race in pkg/dataobj/internal/dataset/page_compress_writer.go:
// defaultCompressionOptions is a package-global *CompressionOptions whose
// init() lazily assigns o.zstdWriter without synchronization. Two parallel
// fixture builds both call newCompressWriter(... opts=nil ...) → both fall
// through to defaultCompressionOptions → both race on init().
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

// TestIndexSectionsReader_LegacyOnlyObjectStaysOnLegacyPath proves /
// : when an object contains no postings section, the legacy path runs
// even with the gate forced on. r.postingsReaders is empty (no postings
// section to open); r.pointersReaders is populated and serves the query.
// Per the fixture builds a legacy-only object by calling
// AppendStream + ObserveLogLine ONLY (no Observe*Posting methods), so
// indexobj.Builder.Flush emits streams + pointers but skips the empty
// postings builder.
func TestIndexSectionsReader_LegacyOnlyObjectStaysOnLegacyPath(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	obj := buildLegacyOnlyFixture(t)

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, nil, 8192, false)
	// Force the new-path gate ON. With no postings section in the object,
	// the pre-scan finds no postings; precedence does NOT trigger;
	// the legacy errgroup branches open streams + pointers as usual. Proves
	// ("no postings section ⇒ legacy regardless of gate").
	r.useNewPath = true
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	require.Empty(t, r.postingsReaders, "no postings section ⇒ r.postingsReaders is empty")
	require.NotEmpty(t, r.pointersReaders, "legacy path runs ⇒ r.pointersReaders is populated")

	rec, err := r.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, int64(1), rec.NumRows(), "legacy baseline: one matching stream row")

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

// TestIndexSectionsReader_GateOffOnCombinedFixtureRunsLegacyPath proves
// dead-code: with useNewPath=false ( default), the legacy
// path runs even on a combined-section object (streams + pointers +
// postings). r.postingsReaders is empty because the wave-1 errgroup
// branch that opens postings sections is gated on r.useNewPath, so with
// the gate off it never executes. r.pointersReaders is populated and
// serves the query exactly as today.
func TestIndexSectionsReader_GateOffOnCombinedFixtureRunsLegacyPath(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	obj := buildCombinedFixture(t)

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, nil, 8192, false)
	// Do NOT set r.useNewPath here — default ( dead-code).
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	require.Empty(t, r.postingsReaders, "gate off ⇒ wave-1 postings errgroup branch did not run ⇒ r.postingsReaders is empty")
	require.NotEmpty(t, r.pointersReaders, "gate off ⇒ legacy pointers errgroup branch ran ⇒ r.pointersReaders is populated")

	rec, err := r.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, int64(1), rec.NumRows(), "legacy baseline (bit-identical to today): one matching stream row")

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

// TestIndexSectionsReader_NewOnlyObjectUsesPostingsPath proves :
// when an object contains streams + postings sections (NO pointers
// section), the new path takes over — r.pointersReaders and
// r.bloomReaders are empty, r.postingsReaders has length 1, and the
// Arrow batch returned by Read has the same 10-column schema as the
// legacy pointer-scan output ( schema-compatibility schema parity, re-anchored
// at the metastore boundary).
//
// Per the fixture calls AppendStream + ObserveLabelPosting
// + ObserveBloomPosting ONLY — the pointers builder stays empty and Flush
// skips it. AppendStream is still required because 's
// ReadPointers joins against the sibling streams section via the
// OpenWithObject back-pointer.
func TestIndexSectionsReader_NewOnlyObjectUsesPostingsPath(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	obj := buildNewOnlyFixture(t)

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, nil, 8192, false)
	// Force the new-path gate ON. Pre-scan sees a postings section; 	// invariant is satisfied (postings section exists); the new errgroup
	// branch opens the postings section; lazyReadStreams / readPointers
	// dispatch through the new helpers (lazyResolveLabels / readPointersNewPath).
	r.useNewPath = true
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	require.Empty(t, r.pointersReaders, "no pointers section ⇒ r.pointersReaders is empty")
	require.Empty(t, r.bloomReaders, "no pointers section ⇒ r.bloomReaders is empty")
	require.Len(t, r.postingsReaders, 1, "one postings section ⇒ exactly one postings.Reader opened ()")
	require.NotNil(t, r.postingsReaders[0], "wave-1 errgroup populated the reader")

	rec, err := r.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, int64(1), rec.NumRows(), "new-path: one matching stream row from postings.ReadPointers")

	// Schema parity check ( schema-compatibility re-anchored at metastore boundary):
	// readPointersOutputSchema has 9 columns + InternalLabelsFieldName.
	require.Equal(t, 10, len(rec.Schema().Fields()), "postings.ReadPointers output has 9 columns + InternalLabelsFieldName")

	// The 9 well-known columns are pointers-derived field names. The
	// arrow.FixedWidthTypes.Timestamp_ns.Name() value is "timestamp"
	// (the bracketed unit / tz suffixes are NOT part of Name()), so the
	// timestamp fields use "<col>.timestamp" — matches the postings
	// readPointersOutputSchema in pkg/dataobj/sections/postings/reader.go.
	expectedFieldNames := []string{
		"path.path.utf8",
		"section.int64",
		"pointer_kind.int64",
		"stream_id.int64",
		"stream_id_ref.int64",
		"min_timestamp.timestamp",
		"max_timestamp.timestamp",
		"row_count.int64",
		"uncompressed_size.int64",
	}
	for _, name := range expectedFieldNames {
		var found bool
		for _, f := range rec.Schema().Fields() {
			if f.Name == name {
				found = true
				break
			}
		}
		require.True(t, found, "schema must contain field %q ( schema-compatibility schema parity)", name)
	}

	// stream_id column should carry the resolved match (stream 1 → app=foo).
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

// TestIndexSectionsReader_CombinedSectionObjectFavorsPostings proves
// precedence at the slice-population level: when an object
// contains both pointers AND postings sections, the new path wins.
// r.pointersReaders is empty (the pre-scan reset in init() nils
// unopenedPointers when a postings section exists && useNewPath); r.bloomReaders
// is empty for the same reason; r.postingsReaders has length 1.
//
// Per : row-output equivalence between the legacy pointers section
// and the postings section on the same fixture is territory
// (TEST-03). This wave-3 test scope is "the legacy slices are empty
// when precedence fires" — that and only that.
func TestIndexSectionsReader_CombinedSectionObjectFavorsPostings(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	obj := buildCombinedFixture(t)

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, nil, 8192, false)
	// Force the new-path gate ON. Pre-scan finds streams + pointers + postings
	// all present (combined-section). postings observed + useNewPath=true ⇒
	// precedence reset: unopenedPointers and unopenedStreams are
	// nilled before the errgroup runs. Only the postings reader gets opened.
	r.useNewPath = true
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	require.Empty(t, r.pointersReaders, " precedence ⇒ legacy pointers slice is empty even though the section exists")
	require.Empty(t, r.bloomReaders, " precedence ⇒ legacy bloom slice is empty even though the section exists")
	require.Len(t, r.postingsReaders, 1, "one postings section ⇒ exactly one postings.Reader opened ()")
	require.NotNil(t, r.postingsReaders[0], "wave-1 errgroup populated the reader")

	rec, err := r.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, int64(1), rec.NumRows(), "new-path: one matching stream row from postings.ReadPointers (same shape as new-only)")
}

// buildLegacyOnlyFixture produces a dataobj.Object containing streams +
// pointers sections only (no postings). Per , this is
// achieved by calling AppendStream + ObserveLogLine ONLY — the postings
// builder stays empty (EstimatedSize == 0) and Flush skips it.
//
// The fixture mirrors the existing TestIndexSectionsReader_FiltersByStreamMatcherAndTime
// shape so its read output is identical to the legacy baseline.
func buildLegacyOnlyFixture(t *testing.T) *dataobj.Object {
	t.Helper()

	fixtureBuildMu.Lock()
	defer fixtureBuildMu.Unlock()

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

	// Populates the POINTERS builder. NO Observe*Posting calls ⇒ postings
	// builder remains empty ⇒ Flush emits only streams + pointers.
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	return obj
}

// buildNewOnlyFixture produces a dataobj.Object containing streams +
// postings sections only (no pointers). Per , this is
// achieved by calling AppendStream + ObserveLabelPosting + ObserveBloomPosting
// ONLY — the pointers builder stays empty (EstimatedSize == 0) and Flush
// skips it. AppendStream is still required because 's
// postings.Reader.ReadPointers joins against the sibling streams section
// via the OpenWithObject back-pointer.
//
// The label observation must use ColumnName="app", LabelValue="foo" so
// it matches the test's labels.MatchEqual("app", "foo") matcher and the
// AppendStream-recorded stream label set.
func buildNewOnlyFixture(t *testing.T) *dataobj.Object {
	t.Helper()

	fixtureBuildMu.Lock()
	defer fixtureBuildMu.Unlock()

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
		Rows:             1,
	})
	require.NoError(t, err)

	// Populates the POSTINGS builder. ColumnName/LabelValue match the
	// test's matcher (app=foo) so ResolveLabels yields streamID=1.
	require.NoError(t, builder.ObserveLabelPosting(tenantID, postings.LabelObservation{
		ObjectPath:       "test-path",
		SectionIndex:     0,
		ColumnName:       "app",
		LabelValue:       "foo",
		StreamID:         1,
		Timestamp:        now.Add(-3 * time.Hour),
		UncompressedSize: 5,
	}))

	// Populates the POSTINGS builder's bloom column. Minimal stub — no
	// matchers exercise this column at read time in the dispatch test.
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

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	return obj
}

// buildCombinedFixture produces a dataobj.Object containing streams +
// pointers + postings sections (all three). Per , this is
// achieved by calling all of AppendStream + ObserveLogLine +
// ObserveLabelPosting + ObserveBloomPosting — all three section builders
// have non-zero EstimatedSize, so Flush emits all three sections.
//
// Used by both the combined-section dispatch test ( precedence)
// and the gate-off regression test ( dead-code).
func buildCombinedFixture(t *testing.T) *dataobj.Object {
	t.Helper()

	fixtureBuildMu.Lock()
	defer fixtureBuildMu.Unlock()

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
		Rows:             1,
	})
	require.NoError(t, err)

	// Populates the POINTERS builder.
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5))

	// Populates the POSTINGS builder label posting.
	require.NoError(t, builder.ObserveLabelPosting(tenantID, postings.LabelObservation{
		ObjectPath:       "test-path",
		SectionIndex:     0,
		ColumnName:       "app",
		LabelValue:       "foo",
		StreamID:         1,
		Timestamp:        now.Add(-3 * time.Hour),
		UncompressedSize: 5,
	}))

	// Populates the POSTINGS builder bloom column.
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

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	return obj
}

// buildNewOnlyFixtureMultiLabel produces a new-format dataobj.Object where
// the single stream carries TWO stream-label columns (app=foo, team=ops)
// but the postings section only observes a label posting for "app" (the
// "team" column exists in the streams section but has no postings/bloom
// row). This shape is what triggers the false-negative: a predicate
// on "team" must be stripped by filterBloomPredicates before reaching
// postings.MatchSections (which would otherwise AND-drop every section
// looking for a non-existent "team" bloom row).
func buildNewOnlyFixtureMultiLabel(t *testing.T) *dataobj.Object {
	t.Helper()

	fixtureBuildMu.Lock()
	defer fixtureBuildMu.Unlock()

	builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	_, err = builder.AppendStream(tenantID, streams.Stream{
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

	// Observe only "app" in postings; "team" is a streams-section column
	// with no corresponding postings row. The fix relies on
	// postings.Reader.StreamLabelColumnNames() returning {"app", "team"}
	// from the sibling streams section so filterBloomPredicates strips
	// the team= predicate before MatchSections runs.
	require.NoError(t, builder.ObserveLabelPosting(tenantID, postings.LabelObservation{
		ObjectPath:       "test-path",
		SectionIndex:     0,
		ColumnName:       "app",
		LabelValue:       "foo",
		StreamID:         1,
		Timestamp:        now.Add(-3 * time.Hour),
		UncompressedSize: 5,
	}))

	// Minimal bloom column stub (mirrors buildNewOnlyFixture).
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

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	return obj
}

// TestIndexSectionsReader_NewPath_StreamLabelPredicateOnDisjointName is the
// regression test. With useNewPath=true, a matcher on one stream
// label ("app") plus a predicate on a DIFFERENT stream label ("team")
// must NOT cause an empty result. The fix enriches labelNamesByStream
// with streams-section column names via
// postings.Reader.StreamLabelColumnNames(); filterBloomPredicates then
// strips the team= predicate so it never reaches postings.MatchSections.
//
// The existing TestIndexSectionsReader_LabelPredicatesFiltered does not
// cover this because its matcher and predicate share the same name
// (app=foo), which ResolveLabels already records via the matcher-side
// path.
func TestIndexSectionsReader_NewPath_StreamLabelPredicateOnDisjointName(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	obj := buildNewOnlyFixtureMultiLabel(t)

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}
	predicates := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "team", "ops")}

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, predicates, 8192, false)
	r.useNewPath = true
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	require.Len(t, r.postingsReaders, 1, "new-path: one postings reader opened")

	var total int64
	for {
		rec, err := r.Read(ctx)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if rec != nil {
			total += rec.NumRows()
			rec.Release()
		}
	}

	require.Greater(t, total, int64(0),
		"new-path must filter the stream-label predicate (team=ops) out of the bloom check; "+
			"without the  fix, MatchSections AND-drops every section and Read returns EOF")
}

// TestIndexSectionsReader_NewPath_NoPostingsReader_ShortCircuit covers the
// fix: when useNewPath is forced on but r.postingsReaders is empty
// (defensive branch in lazyResolveLabels), Read returns io.EOF cleanly
// and does NOT record the StreamsReadTime stat. We verify the short-
// circuit behaviour indirectly by observing that Read returns EOF
// without panicking and without populating matchingStreamIDs.
func TestIndexSectionsReader_NewPath_NoPostingsReader_ShortCircuit(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	// buildLegacyOnlyFixture has streams + pointers but NO postings. With
	// useNewPath forced on AFTER Open, the pre-scan would normally flip
	// useNewPath off (no postings section ⇒ ). To force the defensive
	// short-circuit in lazyResolveLabels, we must keep useNewPath true
	// after Open() AND ensure r.postingsReaders is empty — set the flag
	// post-Open so the pre-scan does not see it.
	obj := buildLegacyOnlyFixture(t)

	start := now.Add(-4 * time.Hour)
	end := now.Add(-time.Hour)
	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}

	r := newIndexSectionsReader(log.NewNopLogger(), obj, start, end, matchers, nil, 8192, false)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))
	require.Empty(t, r.postingsReaders, "no postings section ⇒ empty slice")

	// Now flip the gate on post-Open so Read dispatches into the new path
	// and exercises the lazyResolveLabels defensive short-circuit.
	r.useNewPath = true

	_, err := r.Read(ctx)
	require.ErrorIs(t, err, io.EOF, "no postings reader ⇒ new path returns io.EOF cleanly")
	require.Empty(t, r.matchingStreamIDs, "short-circuit must not populate matchingStreamIDs")
}
