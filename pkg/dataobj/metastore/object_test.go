package metastore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	dskitmetrics "github.com/grafana/dskit/metrics"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/xcap"
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

// addStreamAndFlush flushes stream into its own object, records it in the
// tenant's ToCs and returns the object's path.
func (b *testDataBuilder) addStreamAndFlush(tenant string, stream logproto.Stream) string {
	err := b.builder.Append(tenant, stream, now)
	require.NoError(b.t, err)

	timeRanges := b.builder.TimeRanges()
	obj, closer, err := b.builder.Flush()
	require.NoError(b.t, err)
	defer closer.Close()

	path, err := b.uploader.Upload(b.t.Context(), obj)
	require.NoError(b.t, err)

	require.NoError(b.t, writeTimeRanges(context.Background(), b.meta, path, timeRanges))
	return path
}

// getRecordingBucket records the names of every object read through Get.
type getRecordingBucket struct {
	objstore.Bucket

	mtx   sync.Mutex
	names []string
}

func (b *getRecordingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	b.mtx.Lock()
	b.names = append(b.names, name)
	b.mtx.Unlock()
	return b.Bucket.Get(ctx, name)
}

func (b *getRecordingBucket) tocReads() []string {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	var out []string
	for _, name := range b.names {
		if strings.HasPrefix(name, TocPrefix) {
			out = append(out, name)
		}
	}
	return out
}

func TestObjectMetastore_ReadsOnlyQueriedTenantToCs(t *testing.T) {
	const (
		tenantA = "tenant-a"
		tenantB = "tenant-b"
	)

	builder := newTestDataBuilder(t)
	var pathsA, pathsB []string
	for _, stream := range testStreams {
		pathsA = append(pathsA, builder.addStreamAndFlush(tenantA, stream))
		pathsB = append(pathsB, builder.addStreamAndFlush(tenantB, logproto.Stream{
			Labels:  `{app="only-in-b"}`,
			Entries: stream.Entries,
		}))
	}

	var (
		start = now.Add(-24 * time.Hour)
		end   = now.Add(24 * time.Hour)
	)

	// Tenant B's objects are reachable through its own ToCs, so tenant A not
	// seeing them below is down to ToC isolation rather than missing data.
	gotB, err := newTestObjectMetastore(builder.bucket).DataObjects(user.InjectOrgID(context.Background(), tenantB), start, end)
	require.NoError(t, err)
	require.ElementsMatch(t, pathsB, gotB)

	for _, tc := range []struct {
		name  string
		query func(ctx context.Context, t *testing.T, mstore *ObjectMetastore)
	}{
		{
			name: "GetIndexes",
			query: func(ctx context.Context, t *testing.T, mstore *ObjectMetastore) {
				resp, err := mstore.GetIndexes(ctx, GetIndexesRequest{Start: start, End: end})
				require.NoError(t, err)
				require.NotEmpty(t, resp.TableOfContentsPaths)
				for _, path := range resp.TableOfContentsPaths {
					require.True(t, strings.HasSuffix(path, "/"+tenantA+"/toc.toc"), "unexpected ToC path %s", path)
				}
				var got []string
				for _, idx := range resp.Indexes {
					got = append(got, idx.Path)
				}
				require.ElementsMatch(t, pathsA, got)
			},
		},
		{
			name: "DataObjects",
			query: func(ctx context.Context, t *testing.T, mstore *ObjectMetastore) {
				got, err := mstore.DataObjects(ctx, start, end)
				require.NoError(t, err)
				require.ElementsMatch(t, pathsA, got)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket := &getRecordingBucket{Bucket: builder.bucket}
			mstore := newTestObjectMetastore(bucket)
			ctx := user.InjectOrgID(context.Background(), tenantA)

			tc.query(ctx, t, mstore)

			tocReads := bucket.tocReads()
			require.NotEmpty(t, tocReads)
			for _, name := range tocReads {
				require.True(t, strings.HasSuffix(name, "/"+tenantA+"/toc.toc"), "read ToC %s of another tenant", name)
			}
		})
	}
}

func TestObjectMetastore_Sections_StreamMatchers(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)

	builder, err := indexobj.NewBuilder(tenantID, logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil, indexobj.NewBuilderMetrics(nil))
	require.NoError(t, err)

	for i, ts := range testStreams {
		lbls, err := syntax.ParseLabels(ts.Labels)
		require.NoError(t, err)

		observeLabelPostings(builder, "test-path", 1, int64(i), lbls, ts.Entries[0].Timestamp, int64(len(ts.Entries[0].Line)))
	}

	// Build and store the object
	timeRanges := []dataobj.TimeRange{builder.TimeRange()}

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	bucket := objstore.NewInMemBucket()

	uploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
	require.NoError(t, uploader.RegisterMetrics(prometheus.NewPedanticRegistry()))

	path, err := uploader.Upload(context.Background(), obj)
	require.NoError(t, err)

	metastoreTocWriter := NewTableOfContentsWriter(bucket, DefaultTocWriterBackoffConfig, DefaultTocBuilderConfig, log.NewNopLogger(), NewTocWriterMetrics(nil), indexobj.NewBuilderMetrics(nil))
	err = writeTimeRanges(context.Background(), metastoreTocWriter, path, timeRanges)
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
			name: "matcher returns no matching sections if time range ends before the section in its ToC window",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			predicates: nil,
			start:      now.Add(-12 * time.Hour).Truncate(MetastoreWindowSize),
			end:        now.Add(-12*time.Hour - time.Nanosecond),
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, capture := xcap.NewCapture(ctx, nil)

			sectionsResp, err := mstore.Sections(ctx, SectionsRequest{Start: tt.start, End: tt.end, Matchers: tt.matchers, Predicates: tt.predicates})
			require.NoError(t, err)
			require.Len(t, sectionsResp.Sections, tt.wantCount)

			// Without a matcher, Sections returns before it reads the object store.
			if len(tt.matchers) > 0 {
				require.Positive(t, xcap.ValueFromRegion[int64](capture, RegionSections, StatMetastoreSectionsDuration))
				require.Positive(t, xcap.Value[int64](capture, dataobj.StatObjectRequestsGet))
				require.Positive(t, xcap.Value[int64](capture, dataobj.StatObjectBytesDownloaded))

				var getIndexesDuration dto.Metric
				require.NoError(t, mstore.metrics.getIndexesTotalDuration.WithLabelValues(resultSuccess).(prometheus.Histogram).Write(&getIndexesDuration))
				require.Positive(t, getIndexesDuration.GetHistogram().GetSampleCount())
			}
		})
	}
}

func TestObjectMetastore_Sections_PredicateMatchers(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)

	builder, err := indexobj.NewBuilder(tenantID, logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil, indexobj.NewBuilderMetrics(nil))
	require.NoError(t, err)

	observeLabelPostings(builder, "test-path", 0, 1, labels.FromStrings("app", "foo"), now.Add(-3*time.Hour), 5)
	observeLabelPostings(builder, "test-path", 0, 1, labels.FromStrings("app", "foo"), now.Add(-2*time.Hour), 0)

	observeBloomPostings(t, builder, "test-path", 0, 1, now.Add(-3*time.Hour), "traceID", "abcd", "1234")
	observeBloomPostings(t, builder, "test-path", 0, 1, now.Add(-3*time.Hour), "spanID", "wxyz")

	// Build and store the object
	timeRanges := []dataobj.TimeRange{builder.TimeRange()}

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	bucket := objstore.NewInMemBucket()

	uploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
	require.NoError(t, uploader.RegisterMetrics(prometheus.NewPedanticRegistry()))

	path, err := uploader.Upload(context.Background(), obj)
	require.NoError(t, err)

	metastoreTocWriter := NewTableOfContentsWriter(bucket, DefaultTocWriterBackoffConfig, DefaultTocBuilderConfig, log.NewNopLogger(), NewTocWriterMetrics(nil), indexobj.NewBuilderMetrics(nil))
	err = writeTimeRanges(context.Background(), metastoreTocWriter, path, timeRanges)
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
				labels.MustNewMatcher(labels.MatchEqual, "spanID", "wxyz"),
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
				labels.MustNewMatcher(labels.MatchEqual, "spanID", "cdef"),
			},
			wantCount: 0,
		},
		{
			name: "multiple missing predicates returns no sections",
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "traceID", "5678"),
				labels.MustNewMatcher(labels.MatchEqual, "spanID", "cdef"),
			},
			wantCount: 0,
		},
	}

	matchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sectionsResp, err := mstore.Sections(ctx, SectionsRequest{Start: now.Add(-3 * time.Hour), End: now.Add(time.Hour), Matchers: matchers, Predicates: tt.predicates})
			require.NoError(t, err)
			require.Len(t, sectionsResp.Sections, tt.wantCount)
		})
	}
}

func TestObjectMetastore_Sections_ShardBucketRange(t *testing.T) {
	ctx := user.InjectOrgID(t.Context(), tenantID)

	observation := func(objectPath string, streamID int64, bucket uint32) postings.LabelObservation {
		return postings.LabelObservation{
			ObjectPath: objectPath, SectionIndex: 0, ColumnName: "app", LabelValue: "foo",
			StreamID: streamID, Timestamp: now, ShardBucket: bucket,
		}
	}

	bucketClient := objstore.NewInMemBucket()
	t.Cleanup(func() { require.NoError(t, bucketClient.Close()) })

	// The two index objects cover disjoint bands of buckets, so a range selects one.
	lowIndex := createIndexObjectWithPostings(ctx, t, bucketClient, []postings.LabelObservation{observation("obj-low", 1, 0), observation("obj-low", 2, 3)})
	highIndex := createIndexObjectWithPostings(ctx, t, bucketClient, []postings.LabelObservation{observation("obj-high", 1, 20), observation("obj-high", 2, 31)})

	m := newTestObjectMetastore(bucketClient)

	requestWithBucketRange := func(bucketRange *postings.ShardBucketRange) SectionsRequest {
		return SectionsRequest{
			Start:            now.Add(-time.Hour),
			End:              now.Add(time.Hour),
			Matchers:         []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")},
			ShardBucketRange: bucketRange,
		}
	}

	requestWithBucketRangeAndReturnStreamsByObject := func(t *testing.T, bucketRange *postings.ShardBucketRange) map[string][]int64 {
		t.Helper()

		resp, err := m.Sections(ctx, requestWithBucketRange(bucketRange))
		require.NoError(t, err)

		out := map[string][]int64{}
		for _, section := range resp.Sections {
			ids := slices.Clone(section.StreamIDs)
			slices.Sort(ids)
			out[section.ObjectPath] = ids
		}
		return out
	}

	t.Run("no range resolves the sections of every index object", func(t *testing.T) {
		require.Equal(t, map[string][]int64{"obj-low": {1, 2}, "obj-high": {1, 2}}, requestWithBucketRangeAndReturnStreamsByObject(t, nil))
	})

	t.Run("a range resolves only the sections whose buckets overlap it, with every stream of their rows", func(t *testing.T) {
		require.Equal(t, map[string][]int64{"obj-low": {1, 2}}, requestWithBucketRangeAndReturnStreamsByObject(t, &postings.ShardBucketRange{From: 0, To: 3}))
		require.Equal(t, map[string][]int64{"obj-high": {1, 2}}, requestWithBucketRangeAndReturnStreamsByObject(t, &postings.ShardBucketRange{From: 31, To: 31}))
	})

	t.Run("a range between the buckets of every section resolves no section", func(t *testing.T) {
		require.Empty(t, requestWithBucketRangeAndReturnStreamsByObject(t, &postings.ShardBucketRange{From: 4, To: 19}))
	})

	t.Run("a range that resolves no section leaves the resolved-sections ratio sum finite", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		recording := NewObjectMetastore(bucketClient, Config{}, log.NewNopLogger(), NewObjectMetastoreMetrics(reg))

		resp, err := recording.Sections(ctx, requestWithBucketRange(&postings.ShardBucketRange{From: 4, To: 19}))
		require.NoError(t, err)
		require.Empty(t, resp.Sections)

		families, err := dskitmetrics.NewMetricFamilyMapFromGatherer(reg)
		require.NoError(t, err)
		ratio, err := dskitmetrics.FindHistogramWithNameAndLabels(families, "loki_metastore_resolved_sections_ratio")
		require.NoError(t, err)
		require.False(t, math.IsNaN(ratio.GetSampleSum()), "the ratio of no section to no resolved section is not a number")
	})

	t.Run("a section that two postings index objects list with the same streams resolves once", func(t *testing.T) {
		bucketClient := objstore.NewInMemBucket()
		t.Cleanup(func() { require.NoError(t, bucketClient.Close()) })

		shared := []postings.LabelObservation{observation("test-path", 1, 0), observation("test-path", 2, 3)}
		createIndexObjectWithPostings(ctx, t, bucketClient, shared)
		createIndexObjectWithPostings(ctx, t, bucketClient, append(slices.Clone(shared), observation("other-path", 1, 0)))

		resp, err := newTestObjectMetastore(bucketClient).Sections(ctx, requestWithBucketRange(&postings.ShardBucketRange{From: 0, To: 3}))
		require.NoError(t, err)

		var paths []string
		for _, section := range resp.Sections {
			paths = append(paths, section.ObjectPath)
		}
		require.ElementsMatch(t, []string{"test-path", "other-path"}, paths)
	})

	t.Run("two index objects that list disjoint streams for a section still fail under a range", func(t *testing.T) {
		bucketClient := objstore.NewInMemBucket()
		t.Cleanup(func() { require.NoError(t, bucketClient.Close()) })

		createIndexObjectWithPostings(ctx, t, bucketClient, []postings.LabelObservation{observation("test-path", 1, 0)})
		createIndexObjectWithPostings(ctx, t, bucketClient, []postings.LabelObservation{observation("test-path", 2, 0)})

		_, err := newTestObjectMetastore(bucketClient).Sections(ctx, requestWithBucketRange(&postings.ShardBucketRange{From: 0, To: 0}))
		require.ErrorIs(t, err, errDifferentSectionStatistics)
	})

	t.Run("a range that starts after it ends fails and names an index object", func(t *testing.T) {
		_, err := m.Sections(ctx, requestWithBucketRange(&postings.ShardBucketRange{From: 5, To: 1}))
		require.ErrorContains(t, err, "invalid shard bucket range [5, 1]")
		require.Regexp(t, regexp.QuoteMeta(lowIndex)+"|"+regexp.QuoteMeta(highIndex), err.Error())
	})
}

func TestObjectMetastore_Sections_DuplicateSections(t *testing.T) {
	// indexedLine is one log line an index object records for a section of a log object.
	type indexedLine struct {
		objectPath string
		section    int64
		streamID   int64
		labels     string
		timestamp  time.Time
	}

	// addIndexObject builds an index object holding lines, uploads it to bucket, adds it to the ToC
	// and returns its path. The key of an uploaded object comes from its content, so two index
	// objects need different lines to get different keys.
	addIndexObject := func(t *testing.T, bucket objstore.Bucket, lines []indexedLine) string {
		t.Helper()

		builder, err := indexobj.NewBuilder(tenantID, logsobj.BuilderBaseConfig{
			TargetPageSize:          1024 * 1024,
			TargetObjectSize:        10 * 1024 * 1024,
			TargetSectionSize:       128,
			BufferSize:              1024 * 1024,
			SectionStripeMergeLimit: 2,
		}, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)

		for _, line := range lines {
			lbls, err := syntax.ParseLabels(line.labels)
			require.NoError(t, err)

			observeLabelPostings(builder, line.objectPath, line.section, line.streamID, lbls, line.timestamp, 1)
		}

		timeRanges := []dataobj.TimeRange{builder.TimeRange()}
		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { _ = closer.Close() })

		up := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
		require.NoError(t, up.RegisterMetrics(prometheus.NewPedanticRegistry()))
		path, err := up.Upload(context.Background(), obj)
		require.NoError(t, err)

		toc := NewTableOfContentsWriter(bucket, DefaultTocWriterBackoffConfig, DefaultTocBuilderConfig, log.NewNopLogger(), NewTocWriterMetrics(nil), indexobj.NewBuilderMetrics(nil))
		require.NoError(t, writeTimeRanges(context.Background(), toc, path, timeRanges))
		return path
	}

	// addPostingsIndexObject builds an index object with only a postings section, uploads it to
	// bucket, adds it to the ToC and returns its path. The metastore reads it in the postings flow,
	// which reports no row count or size.
	addPostingsIndexObject := func(t *testing.T, bucket objstore.Bucket, observations []postings.LabelObservation) string {
		t.Helper()

		obj, closer := buildPostingsIndexObject(t, tenantID, observations)
		t.Cleanup(closer)

		up := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
		require.NoError(t, up.RegisterMetrics(prometheus.NewPedanticRegistry()))
		path, err := up.Upload(context.Background(), obj)
		require.NoError(t, err)

		toc := NewTableOfContentsWriter(bucket, DefaultTocWriterBackoffConfig, DefaultTocBuilderConfig, log.NewNopLogger(), NewTocWriterMetrics(nil), indexobj.NewBuilderMetrics(nil))
		require.NoError(t, toc.WriteEntry(context.Background(), tenantID, TableOfContentsEntry{
			Path:      path,
			StartTime: now.Add(-time.Minute),
			EndTime:   now.Add(time.Minute),
		}))
		return path
	}

	ctx := user.InjectOrgID(context.Background(), tenantID)
	request := SectionsRequest{
		Start:    now.Add(-time.Hour),
		End:      now.Add(time.Hour),
		Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")},
	}

	sectionOfTestObject := []indexedLine{
		{objectPath: "test-path", section: 1, streamID: 1, labels: `{app="foo", env="prod"}`, timestamp: now},
		{objectPath: "test-path", section: 1, streamID: 1, labels: `{app="foo", env="prod"}`, timestamp: now.Add(time.Second)},
		{objectPath: "test-path", section: 1, streamID: 2, labels: `{app="foo", env="dev"}`, timestamp: now},
	}
	sectionOfOtherObject := []indexedLine{
		{objectPath: "other-path", section: 1, streamID: 1, labels: `{app="foo", env="prod"}`, timestamp: now},
	}

	observe := func(objectPath string, streamID int64) postings.LabelObservation {
		return postings.LabelObservation{ObjectPath: objectPath, SectionIndex: 0, ColumnName: "app", LabelValue: "foo", StreamID: streamID, Timestamp: now}
	}

	newMetastore := func(bucket objstore.Bucket) (*ObjectMetastore, *prometheus.Registry, *bytes.Buffer) {
		reg := prometheus.NewRegistry()
		logs := bytes.NewBuffer(nil)
		return NewObjectMetastore(bucket, Config{}, log.NewLogfmtLogger(logs), NewObjectMetastoreMetrics(reg)), reg, logs
	}

	requireDuplicateSections := func(t *testing.T, reg *prometheus.Registry, wantIdentical, wantDiverged int) {
		t.Helper()

		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(fmt.Sprintf(`
		# HELP loki_metastore_duplicate_sections_total Total number of duplicate sections found when more than one index object describes the same section. A section in N index objects counts N-1 with diverged=false. The copies of a diverged section disagree on the streams, the row count or the size. That fails the section lookup at the first one, so diverged=true counts at most one for each lookup
		# TYPE loki_metastore_duplicate_sections_total counter
		loki_metastore_duplicate_sections_total{diverged="false"} %d
		loki_metastore_duplicate_sections_total{diverged="true"} %d
		`, wantIdentical, wantDiverged)), "loki_metastore_duplicate_sections_total"))
	}

	t.Run("returns the duplicate section once when two index objects list the same streams", func(t *testing.T) {
		single := objstore.NewInMemBucket()
		addIndexObject(t, single, sectionOfTestObject)
		singleMetastore, singleReg, _ := newMetastore(single)
		want, err := singleMetastore.Sections(ctx, request)
		require.NoError(t, err)
		require.Len(t, want.Sections, 1)
		requireDuplicateSections(t, singleReg, 0, 0)

		duplicated := objstore.NewInMemBucket()
		firstIndex := addIndexObject(t, duplicated, sectionOfTestObject)
		secondIndex := addIndexObject(t, duplicated, append(append([]indexedLine{}, sectionOfTestObject...), sectionOfOtherObject...))
		duplicatedMetastore, duplicatedReg, logs := newMetastore(duplicated)
		got, err := duplicatedMetastore.Sections(ctx, request)
		require.NoError(t, err)

		byKey := map[SectionKey]*DataobjSectionDescriptor{}
		for _, section := range got.Sections {
			byKey[section.SectionKey] = section
		}
		require.Len(t, got.Sections, len(byKey), "a section was returned twice")

		duplicatedSection := byKey[want.Sections[0].SectionKey]
		require.NotNil(t, duplicatedSection)
		require.Equal(t, want.Sections[0].RowCount, duplicatedSection.RowCount)
		require.Equal(t, want.Sections[0].Size, duplicatedSection.Size)
		require.ElementsMatch(t, want.Sections[0].StreamIDs, duplicatedSection.StreamIDs)
		requireDuplicateSections(t, duplicatedReg, 1, 0)
		require.Contains(t, logs.String(), "level=info")
		require.Contains(t, logs.String(), firstIndex)
		require.Contains(t, logs.String(), secondIndex)
	})

	t.Run("keeps the sections of different objects", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		addIndexObject(t, bucket, sectionOfTestObject)
		addIndexObject(t, bucket, sectionOfOtherObject)
		mstore, reg, _ := newMetastore(bucket)

		got, err := mstore.Sections(ctx, request)
		require.NoError(t, err)

		var paths []string
		for _, section := range got.Sections {
			paths = append(paths, section.ObjectPath)
		}
		require.ElementsMatch(t, []string{"test-path", "other-path"}, paths)
		requireDuplicateSections(t, reg, 0, 0)
	})

	t.Run("adds one to the duplicate sections metric per extra index object describing a section", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		addIndexObject(t, bucket, sectionOfTestObject)
		addIndexObject(t, bucket, append(append([]indexedLine{}, sectionOfTestObject...), sectionOfOtherObject...))
		addIndexObject(t, bucket, append(append([]indexedLine{}, sectionOfTestObject...), indexedLine{
			objectPath: "third-path", section: 1, streamID: 1, labels: `{app="foo", env="prod"}`, timestamp: now,
		}))
		mstore, reg, _ := newMetastore(bucket)

		_, err := mstore.Sections(ctx, request)
		require.NoError(t, err)

		requireDuplicateSections(t, reg, 2, 0)
	})

	t.Run("returns the duplicate section once when two postings index objects list the same streams", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		addPostingsIndexObject(t, bucket, []postings.LabelObservation{observe("src-obj", 1), observe("src-obj", 2)})
		addPostingsIndexObject(t, bucket, []postings.LabelObservation{observe("src-obj", 1), observe("src-obj", 2), observe("other-obj", 1)})
		mstore, reg, _ := newMetastore(bucket)

		got, err := mstore.Sections(ctx, request)
		require.NoError(t, err)

		streamsByObject := map[string][]int64{}
		for _, section := range got.Sections {
			require.NotContains(t, streamsByObject, section.ObjectPath, "a section was returned twice")
			streamsByObject[section.ObjectPath] = section.StreamIDs
		}
		require.ElementsMatch(t, []int64{1, 2}, streamsByObject["src-obj"])
		require.Contains(t, streamsByObject, "other-obj")
		requireDuplicateSections(t, reg, 1, 0)
	})

	t.Run("fails the lookup when two postings index objects list different streams of a section", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		firstIndex := addPostingsIndexObject(t, bucket, []postings.LabelObservation{observe("src-obj", 1), observe("src-obj", 2)})
		secondIndex := addPostingsIndexObject(t, bucket, []postings.LabelObservation{observe("src-obj", 1), observe("src-obj", 2), observe("src-obj", 3)})
		mstore, reg, logs := newMetastore(bucket)

		_, err := mstore.Sections(ctx, request)

		require.ErrorIs(t, err, errDifferentSectionStatistics)
		require.Contains(t, logs.String(), "level=error")
		require.Contains(t, logs.String(), "object=src-obj")
		require.Contains(t, logs.String(), "section=0")
		require.Contains(t, logs.String(), firstIndex)
		require.Contains(t, logs.String(), secondIndex)
		requireDuplicateSections(t, reg, 0, 1)
	})
}

func TestObjectMetastore_Sections_LabelsByStreamID(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)

	builder, err := indexobj.NewBuilder(tenantID, logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil, indexobj.NewBuilderMetrics(nil))
	require.NoError(t, err)

	// Stream 1: app=foo, env=prod
	observeLabelPostings(builder, "test-path", 0, 1, labels.FromStrings("app", "foo", "env", "prod"), now.Add(-3*time.Hour), 5)

	// Stream 2: app=bar, env=dev
	observeLabelPostings(builder, "test-path", 1, 2, labels.FromStrings("app", "bar", "env", "dev"), now.Add(-1*time.Hour), 10)

	// Stream 3: app=foo, env=dev (shares app label with stream 1, env with stream 2)
	observeLabelPostings(builder, "test-path", 2, 3, labels.FromStrings("app", "foo", "env", "dev"), now.Add(-30*time.Minute), 7)

	// Build and store the object
	timeRanges := []dataobj.TimeRange{builder.TimeRange()}

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	bucket := objstore.NewInMemBucket()

	uploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
	require.NoError(t, uploader.RegisterMetrics(prometheus.NewPedanticRegistry()))

	path, err := uploader.Upload(context.Background(), obj)
	require.NoError(t, err)

	metastoreTocWriter := NewTableOfContentsWriter(bucket, DefaultTocWriterBackoffConfig, DefaultTocBuilderConfig, log.NewNopLogger(), NewTocWriterMetrics(nil), indexobj.NewBuilderMetrics(nil))
	err = writeTimeRanges(context.Background(), metastoreTocWriter, path, timeRanges)
	require.NoError(t, err)

	mstore := newTestObjectMetastore(bucket)

	tests := []struct {
		name       string
		matchers   []*labels.Matcher
		predicates []*labels.Matcher
		start, end time.Time
		wantCount  int
		wantLabels []string // Expected section-level ambiguous labels
	}{
		{
			name: "no predicates doesn't return any labels",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			predicates: nil,
			start:      now.Add(-4 * time.Hour),
			end:        now.Add(time.Hour),
			wantCount:  2,
			wantLabels: nil,
		},
		{
			name: "ambiguous predicates returns predicate label names",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "bar"),
			},
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
			},
			start:      now.Add(-4 * time.Hour),
			end:        now.Add(time.Hour),
			wantCount:  1,
			wantLabels: []string{"env"},
		},
		{
			// A non-equality label filter (e.g. `| env!="prod"`) references a
			// stream label by name just like an equality filter. Its label name
			// must be recognized as a stream label so the physical planner keeps
			// it ambiguous instead of mistyping it as structured metadata and
			// pushing it to the dataobj scan (which would drop all matching rows).
			name: "not-equal predicate returns predicate label names",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "bar"),
			},
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchNotEqual, "env", "prod"),
			},
			start:      now.Add(-4 * time.Hour),
			end:        now.Add(time.Hour),
			wantCount:  1,
			wantLabels: []string{"env"},
		},
		{
			// Same as above but for a regex-match label filter (e.g. `| env=~"d.*"`).
			name: "regex-match predicate returns predicate label names",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "bar"),
			},
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "env", "d.*"),
			},
			start:      now.Add(-4 * time.Hour),
			end:        now.Add(time.Hour),
			wantCount:  1,
			wantLabels: []string{"env"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sectionsResp, err := mstore.Sections(ctx, SectionsRequest{Start: tt.start, End: tt.end, Matchers: tt.matchers, Predicates: tt.predicates})
			require.NoError(t, err)
			require.Len(t, sectionsResp.Sections, tt.wantCount)

			if tt.wantLabels != nil {
				// Collect all labels across all sections
				var gotLabels []string
				for _, section := range sectionsResp.Sections {
					gotLabels = append(gotLabels, section.AmbiguousPredicates...)
				}

				require.ElementsMatch(t, tt.wantLabels, gotLabels, "labels mismatch")
			}
		})
	}
}

// TestIndexSectionsReader_LabelPredicatesNotFilteredByBlooms tests that predicates on known
// stream labels are not incorrectly filtered out by bloom filters. Before this change, label filters
// after stream selectors (e.g. `{app="foo"} | app="foo"`) would incorrectly return no results
// because the bloom filter lookup would fail for labels that only exist as indexed stream labels,
// not as structured metadata.
func TestIndexSectionsReader_LabelPredicatesNotFilteredByBlooms(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)

	builder, err := indexobj.NewBuilder(tenantID, logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil, indexobj.NewBuilderMetrics(nil))
	require.NoError(t, err)

	// Index a stream with label app=foo
	observeLabelPostings(builder, "test-path", 0, 1, labels.FromStrings("app", "foo"), now.Add(-3*time.Hour), 5)
	observeLabelPostings(builder, "test-path", 0, 1, labels.FromStrings("app", "foo"), now.Add(-2*time.Hour), 0)

	// Add a bloom filter for a metadata column (traceID), NOT for the stream label (app)
	observeBloomPostings(t, builder, "test-path", 0, 1, now.Add(-3*time.Hour), "traceID", "abcd")

	// Build and store the object
	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	bucket := objstore.NewInMemBucket()

	uploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
	require.NoError(t, uploader.RegisterMetrics(prometheus.NewPedanticRegistry()))

	path, err := uploader.Upload(context.Background(), obj)
	require.NoError(t, err)

	mstore := newTestObjectMetastore(bucket)

	tests := []struct {
		name          string
		matchers      []*labels.Matcher
		predicates    []*labels.Matcher
		expectResults bool
	}{
		{
			name: "predicate on stream label should not be filtered by blooms",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			expectResults: true,
		},
		{
			name: "predicate on metadata column still works",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "traceID", "abcd"),
			},
			expectResults: true,
		},
		{
			name: "predicate on metadata column with non-matching value returns no results",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "traceID", "not-in-bloom"),
			},
			expectResults: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := mstore.IndexSectionsReader(ctx, IndexSectionsReaderRequest{
				IndexPath: path,
				SectionsRequest: SectionsRequest{
					Start:      now.Add(-4 * time.Hour),
					End:        now.Add(time.Hour),
					Matchers:   tt.matchers,
					Predicates: tt.predicates,
				},
			})
			require.NoError(t, err)
			t.Cleanup(resp.Reader.Close)
			require.NoError(t, resp.Reader.Open(ctx))

			// Read all records from the reader
			var totalRows int64
			for {
				rec, err := resp.Reader.Read(ctx)
				if err != nil {
					break
				}
				totalRows += rec.NumRows()
			}

			if !tt.expectResults {
				require.Equal(t, int64(0), totalRows, "expected no results")
			} else {
				require.NotEmpty(t, totalRows, "expected results to be returned")
			}
		})
	}
}

// TestDataobjSectionDescriptorMerge_NilLabels verifies that merging labels into
// a descriptor created with no ambiguous labels unions them in cleanly.
func TestDataobjSectionDescriptorMerge_NilLabels(t *testing.T) {
	ptr1 := pointers.SectionPointer{
		Path:        "test-path",
		Section:     0,
		StreamIDRef: 1,
		StartTs:     now.Add(-2 * time.Hour),
		EndTs:       now.Add(-1 * time.Hour),
		LineCount:   10,
	}
	ptr2 := pointers.SectionPointer{
		Path:        "test-path",
		Section:     0,
		StreamIDRef: 2,
		StartTs:     now.Add(-3 * time.Hour),
		EndTs:       now,
		LineCount:   5,
	}

	// Create descriptor with no ambiguous labels.
	desc := NewSectionDescriptor(ptr1, nil)

	desc.Merge(ptr2, []string{"env"})

	require.ElementsMatch(t, []string{"env"}, desc.AmbiguousPredicates)
}

func newTestDataBuilder(t testing.TB) *testDataBuilder {
	bucket := objstore.NewInMemBucket()

	builder, err := logsobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,      // 1MB
		TargetObjectSize:        10 * 1024 * 1024, // 10MB
		TargetSectionSize:       1024 * 1024,      // 1MB
		BufferSize:              1024 * 1024,      // 1MB
		SectionStripeMergeLimit: 2,
	}, nil, logsobj.NewBuilderMetrics(), log.NewNopLogger(), nil)
	require.NoError(t, err)

	logger := log.NewLogfmtLogger(os.Stdout)
	logger = log.With(logger, "test", t.Name())

	meta := NewTableOfContentsWriter(bucket, DefaultTocWriterBackoffConfig, DefaultTocBuilderConfig, logger, NewTocWriterMetrics(prometheus.NewPedanticRegistry()), indexobj.NewBuilderMetrics(nil))

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

// uploadIndexObject uploads obj to a fresh in-memory bucket and returns a
// metastore plus the object's path.
// observeLabelPostings records a label posting for each label of a stream, as
// the index Calculator does for each log line.
func observeLabelPostings(builder *indexobj.Builder, path string, section, streamID int64, lbls labels.Labels, ts time.Time, size int64) {
	shardBucket := streams.DefaultShardLayout().BucketFromLabels(lbls)
	lbls.Range(func(l labels.Label) {
		builder.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath:       path,
			ShardBuckets:     int64(streams.DefaultShardLayout().Buckets()),
			SectionIndex:     section,
			ColumnName:       l.Name,
			LabelValue:       l.Value,
			StreamID:         streamID,
			Timestamp:        ts,
			UncompressedSize: size,
			ShardBucket:      shardBucket,
		})
	})
}

// observeBloomPostings records the values of a structured metadata column of a
// stream in a bloom posting.
func observeBloomPostings(t testing.TB, builder *indexobj.Builder, path string, section, streamID int64, ts time.Time, column string, values ...string) {
	t.Helper()

	builder.PrepareBloomColumn(path, section, column, uint(len(values)), int64(streams.DefaultShardLayout().Buckets()))
	for _, value := range values {
		require.NoError(t, builder.ObserveBloomPosting(postings.BloomObservation{
			ObjectPath:   path,
			ShardBuckets: int64(streams.DefaultShardLayout().Buckets()),
			SectionIndex: section,
			ColumnName:   column,
			Value:        value,
			StreamID:     streamID,
			Timestamp:    ts,
		}))
	}
}

func uploadIndexObject(t *testing.T, obj *dataobj.Object) (*ObjectMetastore, string) {
	t.Helper()
	bucket := objstore.NewInMemBucket()
	up := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
	require.NoError(t, up.RegisterMetrics(prometheus.NewPedanticRegistry()))
	path, err := up.Upload(context.Background(), obj)
	require.NoError(t, err)
	return newTestObjectMetastore(bucket), path
}

func TestIndexSectionsReader_SelectsPostingsWhenPresent(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)
	obj, closer := buildPostingsIndexObject(t, tenantID, []postings.LabelObservation{
		{ObjectPath: "src-obj", SectionIndex: 0, ColumnName: "app", LabelValue: "foo", StreamID: 1, Timestamp: now.Add(-2 * time.Hour)},
	})
	defer closer()
	m, path := uploadIndexObject(t, obj)

	resp, err := m.IndexSectionsReader(ctx, IndexSectionsReaderRequest{
		IndexPath: path,
		SectionsRequest: SectionsRequest{
			Start:    now.Add(-4 * time.Hour),
			End:      now,
			Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")},
		},
	})
	require.NoError(t, err)
	require.IsType(t, &postingsIndexSectionsReader{}, unwrapReader(resp.Reader))
}

// unwrapReader returns the reader that IndexSectionsReader selected, unwrapping
// the metrics decorator.
func unwrapReader(r ArrowRecordBatchReader) ArrowRecordBatchReader {
	if ir, ok := r.(*instrumentedReader); ok {
		return ir.ArrowRecordBatchReader
	}
	return r
}

func TestIterTableOfContentsPaths(t *testing.T) {
	now := time.Date(2025, 1, 1, 15, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name     string
		start    time.Time
		end      time.Time
		expected []string
	}{
		{
			name:     "within single window",
			start:    now,
			end:      now.Add(1 * time.Hour),
			expected: []string{"tocs/2025-01-01T12_00_00Z/tenant/toc.toc"},
		},
		{
			name:     "same start and end",
			start:    now,
			end:      now,
			expected: []string{"tocs/2025-01-01T12_00_00Z/tenant/toc.toc"},
		},
		{
			name:  "begin at start of window",
			start: now.Add(-3 * time.Hour),
			end:   now,
			expected: []string{
				"tocs/2025-01-01T12_00_00Z/tenant/toc.toc",
			},
		},
		{
			name:  "end at start of next window",
			start: now.Add(-4 * time.Hour),
			end:   now.Add(-3 * time.Hour),
			expected: []string{
				"tocs/2025-01-01T00_00_00Z/tenant/toc.toc",
				"tocs/2025-01-01T12_00_00Z/tenant/toc.toc",
			},
		},
		{
			name:  "start and end in different windows",
			start: now.Add(-12 * time.Hour),
			end:   now,
			expected: []string{
				"tocs/2025-01-01T00_00_00Z/tenant/toc.toc",
				"tocs/2025-01-01T12_00_00Z/tenant/toc.toc",
			},
		},
		{
			name:  "span several windows",
			start: now,
			end:   now.Add(48 * time.Hour),
			expected: []string{
				"tocs/2025-01-01T12_00_00Z/tenant/toc.toc",
				"tocs/2025-01-02T00_00_00Z/tenant/toc.toc",
				"tocs/2025-01-02T12_00_00Z/tenant/toc.toc",
				"tocs/2025-01-03T00_00_00Z/tenant/toc.toc",
				"tocs/2025-01-03T12_00_00Z/tenant/toc.toc",
			},
		},
		{
			name:  "start and end in different years",
			start: time.Date(2024, 12, 31, 3, 0, 0, 0, time.UTC),
			end:   time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC),
			expected: []string{
				"tocs/2024-12-31T00_00_00Z/tenant/toc.toc",
				"tocs/2024-12-31T12_00_00Z/tenant/toc.toc",
				"tocs/2025-01-01T00_00_00Z/tenant/toc.toc",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			iter := IterTableOfContentsPaths("tenant", tc.start, tc.end)
			actual := []string{}
			for path := range iter {
				actual = append(actual, path)
			}
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestTableOfContentsPath(t *testing.T) {
	window := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	require.Equal(t, "tocs/2025-01-01T12_00_00Z/", TableOfContentsWindowPrefix(window))
	require.Equal(t, "tocs/2025-01-01T12_00_00Z/tenant/toc.toc", TableOfContentsPath("tenant", window))
}

func TestListTableOfContentsTenants(t *testing.T) {
	for _, tc := range []struct {
		name   string
		bucket func(t *testing.T) objstore.Bucket
	}{
		{name: "in-memory", bucket: func(*testing.T) objstore.Bucket { return objstore.NewInMemBucket() }},
		{name: "filesystem", bucket: func(t *testing.T) objstore.Bucket {
			bucket, err := filesystem.NewBucket(t.TempDir())
			require.NoError(t, err)
			return bucket
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				ctx    = t.Context()
				window = time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
				other  = window.Add(MetastoreWindowSize)
				bucket = tc.bucket(t)
			)
			for _, name := range []string{
				TableOfContentsPath("tenant-b", window),
				TableOfContentsPath("tenant-a", window),
				TableOfContentsPath("tenant-c", other),
				TableOfContentsWindowPrefix(window) + "not-a-toc.txt",
				// A shared ToC from before ToCs were split per tenant.
				"tocs/2025-01-01T12_00_00Z.toc",
			} {
				require.NoError(t, bucket.Upload(ctx, name, strings.NewReader("")))
			}

			tenants, err := ListTableOfContentsTenants(ctx, bucket, window)
			require.NoError(t, err)
			require.Equal(t, []string{"tenant-a", "tenant-b"}, tenants)

			tenants, err = ListTableOfContentsTenants(ctx, bucket, window.Add(-MetastoreWindowSize))
			require.NoError(t, err)
			require.Empty(t, tenants)
		})
	}
}

func TestObjectMetastore_readIndexSections(t *testing.T) {
	ctx := user.InjectOrgID(t.Context(), tenantID)
	request := SectionsRequest{
		Start:    now.Add(-time.Hour),
		End:      now.Add(time.Hour),
		Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")},
	}

	t.Run("returns the sections of the index object with its path and the number of rows read", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		indexPath := createIndexObjectWithPostings(ctx, t, bucket, []postings.LabelObservation{
			{ObjectPath: "object-a", SectionIndex: 0, ColumnName: "app", LabelValue: "foo", StreamID: 1, Timestamp: now},
			{ObjectPath: "object-b", SectionIndex: 0, ColumnName: "app", LabelValue: "foo", StreamID: 1, Timestamp: now},
		})

		sections, readRows, err := newTestObjectMetastore(bucket).readIndexSections(ctx, request, indexPath)

		require.NoError(t, err)
		require.Len(t, sections, 2)
		require.ElementsMatch(t, []string{"object-a", "object-b"}, []string{sections[0].ObjectPath, sections[1].ObjectPath})
		require.Equal(t, indexPath, sections[0].IndexPath)
		require.Equal(t, indexPath, sections[1].IndexPath)
		require.Positive(t, readRows)
	})

	t.Run("returns no section and no rows when the index object lists no section for the request", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		indexPath := createIndexObjectWithPostings(ctx, t, bucket, []postings.LabelObservation{
			{ObjectPath: "object-a", SectionIndex: 0, ColumnName: "app", LabelValue: "bar", StreamID: 1, Timestamp: now},
		})

		sections, readRows, err := newTestObjectMetastore(bucket).readIndexSections(ctx, request, indexPath)

		require.NoError(t, err)
		require.Empty(t, sections)
		require.Zero(t, readRows)
	})

	t.Run("returns an error that names the index object when the index object does not exist", func(t *testing.T) {
		sections, readRows, err := newTestObjectMetastore(objstore.NewInMemBucket()).readIndexSections(ctx, request, "missing-index-object")

		require.ErrorContains(t, err, "missing-index-object")
		require.Empty(t, sections)
		require.Zero(t, readRows)
	})
}

// createIndexObjectWithPostings uploads an index object with one postings section, adds it to the ToC,
// and returns its path.
func createIndexObjectWithPostings(ctx context.Context, t testing.TB, bucket objstore.Bucket, observations []postings.LabelObservation) string {
	t.Helper()

	obj, closer := buildPostingsIndexObject(t, tenantID, observations)
	t.Cleanup(closer)

	up := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
	require.NoError(t, up.RegisterMetrics(prometheus.NewPedanticRegistry()))
	path, err := up.Upload(ctx, obj)
	require.NoError(t, err)

	toc := NewTableOfContentsWriter(bucket, DefaultTocWriterBackoffConfig, DefaultTocBuilderConfig, log.NewNopLogger(), NewTocWriterMetrics(nil), indexobj.NewBuilderMetrics(nil))
	require.NoError(t, toc.WriteEntry(ctx, tenantID, TableOfContentsEntry{
		Path:      path,
		StartTime: now.Add(-time.Minute),
		EndTime:   now.Add(time.Minute),
	}))
	return path
}
