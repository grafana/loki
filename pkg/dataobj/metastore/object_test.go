package metastore

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

const (
	tenantID = "test-tenant"
)

var (
	now = time.Now().UTC()

	// our streams won't use any log lines, therefore leave them out of the Entry structs
	testStreams = []logproto.Stream{
		{
			Labels:  `{app="foo", env="prod"}`,
			Entries: []logproto.Entry{{Timestamp: now.Add(-2 * time.Hour)}},
		},
		{
			Labels:  `{app="foo", env="dev"}`,
			Entries: []logproto.Entry{{Timestamp: now}},
		},
		{
			Labels:  `{app="bar", env="prod"}`,
			Entries: []logproto.Entry{{Timestamp: now.Add(5 * time.Second)}},
		},
		{
			Labels:  `{app="bar", env="dev"}`,
			Entries: []logproto.Entry{{Timestamp: now.Add(8 * time.Minute)}},
		},
		{
			Labels:  `{app="baz", env="prod", team="a"}`,
			Entries: []logproto.Entry{{Timestamp: now.Add(12 * time.Minute)}},
		},
		{
			Labels:  `{app="foo", env="prod"}`,
			Entries: []logproto.Entry{{Timestamp: now.Add(-12 * time.Hour)}},
		},
		{
			Labels:  `{app="foo", env="prod"}`,
			Entries: []logproto.Entry{{Timestamp: now.Add(12 * time.Hour)}},
		},
	}
)

// Similar to store_test.go -- we need a populated dataobj/builder/metastore to test labels and values
type testDataBuilder struct {
	t      testing.TB
	bucket objstore.Bucket

	builder  *logsobj.Builder
	meta     *TableOfContentsWriter
	uploader *uploader.Uploader
}

func (b *testDataBuilder) addStreamAndFlush(tenant string, stream logproto.Stream) {
	err := b.builder.Append(tenant, stream)
	require.NoError(b.t, err)

	timeRanges := b.builder.TimeRanges()
	obj, closer, err := b.builder.Flush()
	require.NoError(b.t, err)
	defer closer.Close()

	path, err := b.uploader.Upload(b.t.Context(), obj)
	require.NoError(b.t, err)

	require.NoError(b.t, b.meta.WriteEntry(context.Background(), path, timeRanges))
}

func TestLabels(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
	}

	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedLabels, err := mstore.Labels(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedLabels, len(matchers))
	})
}

func TestNonExistentLabels(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "invalid"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "ops"),
	}

	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedLabels, err := mstore.Labels(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedLabels, 0)
	})
}

func TestMixedLabels(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "invalid"),
	}

	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedLabels, err := mstore.Labels(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedLabels, 0)
	})
}

func TestLabelsSingleMatcher(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
	}

	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedLabels, err := mstore.Labels(ctx, start, end, matchers...)
		require.NoError(t, err)

		require.Len(t, matchedLabels, 3)
		for _, expectedLabel := range []string{"env", "team", "app"} {
			require.NotEqual(t, slices.Index(matchedLabels, expectedLabel), -1)
		}
	})
}

func TestLabelsEmptyMatcher(t *testing.T) {
	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedLabels, err := mstore.Labels(ctx, start, end)
		require.NoError(t, err)
		require.Len(t, matchedLabels, 3)
	})
}

func TestValues(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
	}

	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedValues, err := mstore.Values(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedValues, len(matchers))
	})
}

func TestNonExistentValues(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "invalid"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "ops"),
	}

	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedValues, err := mstore.Values(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedValues, 0)
	})
}

func TestMixedValues(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "env", "ops"),
	}

	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedValues, err := mstore.Values(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedValues, 0)
	})
}

func TestValuesSingleMatcher(t *testing.T) {
	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
	}

	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedValues, err := mstore.Values(ctx, start, end, matchers...)
		require.NoError(t, err)
		require.Len(t, matchedValues, 5)
	})
}

func TestValuesEmptyMatcher(t *testing.T) {
	queryMetastore(t, tenantID, func(ctx context.Context, start, end time.Time, mstore Metastore) {
		matchedValues, err := mstore.Values(ctx, start, end)
		require.NoError(t, err)
		require.Len(t, matchedValues, 6)
		for _, expectedValue := range []string{"foo", "prod", "bar", "dev", "baz", "a"} {
			require.NotEqual(t, slices.Index(matchedValues, expectedValue), -1)
		}
	})
}

func TestSectionsForStreamMatchers(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)

	builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	for i, ts := range testStreams {
		lbls, err := syntax.ParseLabels(ts.Labels)
		require.NoError(t, err)

		newIdx, err := builder.AppendStream(tenantID, streams.Stream{
			ID:               int64(i),
			Labels:           lbls,
			MinTimestamp:     ts.Entries[0].Timestamp,
			MaxTimestamp:     ts.Entries[0].Timestamp,
			UncompressedSize: 0,
		})
		require.NoError(t, err)
		err = builder.ObserveLogLine(tenantID, "test-path", 1, newIdx, int64(i), ts.Entries[0].Timestamp, int64(len(ts.Entries[0].Line)))
		require.NoError(t, err)
	}

	// Add one more stream for a different tenant to ensure it is not resolved.
	altTenant := "tenant-alt"
	altTenantSection := int64(99) // Emulate a different section from a log object that doesn't collide with the main tenant's section
	newIdx, err := builder.AppendStream(altTenant, streams.Stream{
		ID:               1,
		Labels:           labels.New(labels.Label{Name: "app", Value: "foo"}, labels.Label{Name: "tenant", Value: altTenant}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
	})
	require.NoError(t, err)
	err = builder.ObserveLogLine(altTenant, "test-path", altTenantSection, newIdx, 1, now.Add(-2*time.Hour), 5)
	require.NoError(t, err)

	// Build and store the object
	timeRanges := builder.TimeRanges()
	require.Len(t, timeRanges, 2)

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	bucket := objstore.NewInMemBucket()

	uploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
	require.NoError(t, uploader.RegisterMetrics(prometheus.NewPedanticRegistry()))

	path, err := uploader.Upload(context.Background(), obj)
	require.NoError(t, err)

	metastoreTocWriter := NewTableOfContentsWriter(bucket, log.NewNopLogger())
	err = metastoreTocWriter.WriteEntry(context.Background(), path, timeRanges)
	require.NoError(t, err)

	mstore := newTestObjectMetastore(bucket)

	tests := []struct {
		name       string
		matchers   []*labels.Matcher
		predicates []*labels.Matcher
		start, end time.Time
		wantCount  int
	}{
		{
			name:       "no matchers returns no sections",
			matchers:   nil,
			predicates: nil,
			start:      now.Add(-time.Hour),
			end:        now.Add(time.Hour),
			wantCount:  0,
		},
		{
			name: "single matcher returns matching sections",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			predicates: nil,
			start:      now.Add(-time.Hour),
			end:        now.Add(time.Hour),
			wantCount:  1,
		},
		{
			name: "non-existent matcher",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "doesnotexist"),
			},
			predicates: nil,
			start:      now.Add(-time.Hour),
			end:        now.Add(time.Hour),
			wantCount:  0,
		},
		{
			name: "matching selector with unsupported predicate type",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "bar", "something"),
			},
			start:     now.Add(-time.Hour),
			end:       now.Add(time.Hour),
			wantCount: 1,
		},
		{
			name: "stream matcher with not matching predicate returns no matching sections",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "bar", "something"),
			},
			start:     now.Add(-time.Hour),
			end:       now.Add(time.Hour),
			wantCount: 0,
		},
		{
			name: "matcher returns no matching sections if time range is out of bounds",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			predicates: nil,
			start:      now.Add(-3 * time.Hour),
			end:        now.Add(-2 * time.Hour),
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sectionsResp, err := mstore.Sections(ctx, SectionsRequest{tt.start, tt.end, tt.matchers, tt.predicates})
			require.NoError(t, err)
			require.Len(t, sectionsResp.Sections, tt.wantCount)
			for _, section := range sectionsResp.Sections {
				require.NotEqual(t, section.SectionIdx, altTenantSection)
			}
		})
	}
}

func TestSectionsForPredicateMatchers(t *testing.T) {
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
	err = builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5)
	require.NoError(t, err)
	err = builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-2*time.Hour), 0)
	require.NoError(t, err)

	traceIDBloom := bloom.NewWithEstimates(10, 0.01)
	traceIDBloom.AddString("abcd")
	traceIDBloom.AddString("1234")
	traceIDBloomBytes, err := traceIDBloom.MarshalBinary()
	require.NoError(t, err)

	err = builder.AppendColumnIndex(tenantID, "test-path", 0, "traceID", 0, traceIDBloomBytes)
	require.NoError(t, err)

	// Build and store the object
	timeRanges := builder.TimeRanges()
	require.Len(t, timeRanges, 1)

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	bucket := objstore.NewInMemBucket()

	uploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
	require.NoError(t, uploader.RegisterMetrics(prometheus.NewPedanticRegistry()))

	path, err := uploader.Upload(context.Background(), obj)
	require.NoError(t, err)

	metastoreTocWriter := NewTableOfContentsWriter(bucket, log.NewNopLogger())
	err = metastoreTocWriter.WriteEntry(context.Background(), path, timeRanges)
	require.NoError(t, err)

	mstore := newTestObjectMetastore(bucket)

	tests := []struct {
		name       string
		predicates []*labels.Matcher
		wantCount  int
	}{
		{
			name:       "no predicates returns all sections",
			predicates: nil,
			wantCount:  1,
		},
		{
			name: "single predicate returns matching sections",
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "traceID", "abcd"),
			},
			wantCount: 1,
		},
		{
			name: "multiple valid predicates returns matching sections",
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "traceID", "abcd"),
				labels.MustNewMatcher(labels.MatchEqual, "traceID", "1234"),
			},
			wantCount: 1,
		},
		{
			name: "missing predicates returns no sections",
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "traceID", "cdef"),
			},
			wantCount: 0,
		},
		{
			name: "partial missing predicates returns no sections",
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "traceID", "abcd"),
				labels.MustNewMatcher(labels.MatchEqual, "traceID", "cdef"),
			},
			wantCount: 0,
		},
		{
			name: "multiple missing predicates returns no sections",
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "traceID", "5678"),
				labels.MustNewMatcher(labels.MatchEqual, "traceID", "cdef"),
			},
			wantCount: 0,
		},
	}

	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sectionsResp, err := mstore.Sections(ctx, SectionsRequest{now.Add(-3 * time.Hour), now.Add(time.Hour), matchers, tt.predicates})
			require.NoError(t, err)
			require.Len(t, sectionsResp.Sections, tt.wantCount)
		})
	}
}

func queryMetastore(t *testing.T, tenant string, mfunc func(context.Context, time.Time, time.Time, Metastore)) {
	now := time.Now().UTC()
	start := now.Add(-time.Hour * 5)
	end := now.Add(time.Hour * 5)

	builder := newTestDataBuilder(t)

	for _, stream := range testStreams {
		builder.addStreamAndFlush(tenant, stream)
	}

	mstore := newTestObjectMetastore(builder.bucket)
	defer func() {
		require.NoError(t, mstore.bucket.Close())
	}()

	ctx := user.InjectOrgID(context.Background(), tenant)

	mfunc(ctx, start, end, mstore)
}

func newTestDataBuilder(t testing.TB) *testDataBuilder {
	bucket := objstore.NewInMemBucket()

	builder, err := logsobj.NewBuilder(logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:          1024 * 1024,      // 1MB
			TargetObjectSize:        10 * 1024 * 1024, // 10MB
			TargetSectionSize:       1024 * 1024,      // 1MB
			BufferSize:              1024 * 1024,      // 1MB
			SectionStripeMergeLimit: 2,
		},
	}, nil)
	require.NoError(t, err)

	logger := log.NewLogfmtLogger(os.Stdout)
	logger = log.With(logger, "test", t.Name())

	meta := NewTableOfContentsWriter(bucket, logger)
	require.NoError(t, meta.RegisterMetrics(prometheus.NewPedanticRegistry()))

	uploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, logger)
	require.NoError(t, uploader.RegisterMetrics(prometheus.NewPedanticRegistry()))

	return &testDataBuilder{
		t:        t,
		bucket:   bucket,
		builder:  builder,
		meta:     meta,
		uploader: uploader,
	}
}

func newTestObjectMetastore(bucket objstore.Bucket) *ObjectMetastore {
	return NewObjectMetastore(bucket, Config{}, log.NewNopLogger(), NewObjectMetastoreMetrics(prometheus.NewRegistry()))
}
