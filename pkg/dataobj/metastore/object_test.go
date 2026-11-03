package metastore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
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
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metadatacache"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
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
	obj, closer, err := b.builder.FlushAndSort(b.t.Context())
	require.NoError(b.t, err)
	defer closer.Close()

	path, err := b.uploader.Upload(b.t.Context(), obj)
	require.NoError(b.t, err)

	require.NoError(b.t, writeTimeRanges(context.Background(), b.meta, path, timeRanges))
	return path
}

// getRecordingBucket records the names of every object read through Get, and counts the reads
// through GetRange.
type getRecordingBucket struct {
	objstore.Bucket

	mtx   sync.Mutex
	names []string

	rangeReads atomic.Int64
}

func (b *getRecordingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	b.mtx.Lock()
	b.names = append(b.names, name)
	b.mtx.Unlock()
	return b.Bucket.Get(ctx, name)
}

func (b *getRecordingBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	b.rangeReads.Inc()
	return b.Bucket.GetRange(ctx, name, off, length)
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

			require.Positive(t, xcap.ValueFromRegion[int64](capture, RegionSections, StatMetastoreSectionsDuration))
			require.Positive(t, xcap.Value[int64](capture, dataobj.StatObjectRequestsGet))
			require.Positive(t, xcap.Value[int64](capture, dataobj.StatObjectBytesDownloaded))

			var getIndexesDuration dto.Metric
			require.NoError(t, mstore.metrics.getIndexesTotalDuration.WithLabelValues(resultSuccess).(prometheus.Histogram).Write(&getIndexesDuration))
			require.Positive(t, getIndexesDuration.GetHistogram().GetSampleCount())
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
	lowIndex := createIndexObject(ctx, t, bucketClient, []postings.LabelObservation{observation("obj-low", 1, 0), observation("obj-low", 2, 3)})
	highIndex := createIndexObject(ctx, t, bucketClient, []postings.LabelObservation{observation("obj-high", 1, 20), observation("obj-high", 2, 31)})

	m := newTestObjectMetastore(bucketClient)

	requestWithBucketRange := func(bucketRange *postings.ShardBucketRange) SectionsRequest {
		return SectionsRequest{
			Start:           now.Add(-time.Hour),
			End:             now.Add(time.Hour),
			Matchers:        []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")},
			StreamHashRange: hashRangeOfBuckets(streams.DefaultShardLayout(), bucketRange),
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
		createIndexObject(ctx, t, bucketClient, shared)
		createIndexObject(ctx, t, bucketClient, append(slices.Clone(shared), observation("other-path", 1, 0)))

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

		createIndexObject(ctx, t, bucketClient, []postings.LabelObservation{observation("test-path", 1, 0)})
		createIndexObject(ctx, t, bucketClient, []postings.LabelObservation{observation("test-path", 2, 0)})

		_, err := newTestObjectMetastore(bucketClient).Sections(ctx, requestWithBucketRange(&postings.ShardBucketRange{From: 0, To: 0}))
		require.ErrorIs(t, err, errDifferentSectionStatistics)
	})

	t.Run("a range that starts after it ends fails and names an index object", func(t *testing.T) {
		_, err := m.Sections(ctx, requestWithBucketRange(&postings.ShardBucketRange{From: 5, To: 1}))
		require.ErrorContains(t, err, "invalid shard bucket range [5, 1]")
		require.Regexp(t, regexp.QuoteMeta(lowIndex)+"|"+regexp.QuoteMeta(highIndex), err.Error())
	})
}

func TestObjectMetastore_Sections_StreamHashRange(t *testing.T) {
	ctx := user.InjectOrgID(t.Context(), tenantID)

	observation := func(objectPath string, bucket uint32) postings.LabelObservation {
		return postings.LabelObservation{
			ObjectPath: objectPath, SectionIndex: 0, ColumnName: "app", LabelValue: "foo",
			StreamID: 1, Timestamp: now, ShardBucket: bucket,
		}
	}

	// The two index objects have layouts of 8 and 64 buckets. In both, one section lies in the
	// upper half of the hashes and one in the lower half. Buckets 5 and 40 are in the upper half.
	bucketClient := objstore.NewInMemBucket()
	t.Cleanup(func() { require.NoError(t, bucketClient.Close()) })
	createIndexObject(ctx, t, bucketClient, []postings.LabelObservation{
		observation("obj-8-lower", 1), observation("obj-8-upper", 5),
	}, streams.NewLayout(streams.MustShardLayoutFromBuckets(8), nil))
	createIndexObject(ctx, t, bucketClient, []postings.LabelObservation{
		observation("obj-64-lower", 20), observation("obj-64-upper", 40),
	}, streams.NewLayout(streams.MustShardLayoutFromBuckets(64), nil))
	m := newTestObjectMetastore(bucketClient)

	resolvedObjects := func(t *testing.T, hashRange *streams.HashRange) []string {
		t.Helper()

		resp, err := m.Sections(ctx, SectionsRequest{
			Start:           now.Add(-time.Hour),
			End:             now.Add(time.Hour),
			Matchers:        []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")},
			StreamHashRange: hashRange,
		})
		require.NoError(t, err)
		return objectPathsOf(resp.Sections)
	}

	t.Run("prunes each index object with the shard layout that it records", func(t *testing.T) {
		upperHalf := &streams.HashRange{From: 1 << 63, To: math.MaxUint64}
		require.ElementsMatch(t, []string{"obj-8-upper", "obj-64-upper"}, resolvedObjects(t, upperHalf))
	})

	t.Run("resolves every section when the range covers every bucket of each layout", func(t *testing.T) {
		everything := &streams.HashRange{From: 0, To: math.MaxUint64}
		require.ElementsMatch(t, []string{"obj-8-lower", "obj-8-upper", "obj-64-lower", "obj-64-upper"}, resolvedObjects(t, everything))
	})

	t.Run("resolves every section when the request has no range", func(t *testing.T) {
		require.ElementsMatch(t, []string{"obj-8-lower", "obj-8-upper", "obj-64-lower", "obj-64-upper"}, resolvedObjects(t, nil))
	})
}

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
func addIndexObject(t *testing.T, bucket objstore.Bucket, lines []indexedLine) string {
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
	path, err := up.Upload(t.Context(), obj)
	require.NoError(t, err)

	toc := NewTableOfContentsWriter(bucket, DefaultTocWriterBackoffConfig, DefaultTocBuilderConfig, log.NewNopLogger(), NewTocWriterMetrics(nil), indexobj.NewBuilderMetrics(nil))
	require.NoError(t, writeTimeRanges(t.Context(), toc, path, timeRanges))
	return path
}

func TestObjectMetastore_Sections_DuplicateSections(t *testing.T) {
	// addPostingsIndexObject builds an index object with only a postings section, uploads it to
	// bucket, adds it to the ToC and returns its path. The metastore reads it in the postings flow,
	// which reports no row count or size.
	addPostingsIndexObject := func(t *testing.T, bucket objstore.Bucket, observations []postings.LabelObservation) string {
		t.Helper()

		obj, closer := buildIndexObject(t, tenantID, observations)
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
		singleMetastore, singleReg, _ := newTestMetastore(single)
		want, err := singleMetastore.Sections(ctx, request)
		require.NoError(t, err)
		require.Len(t, want.Sections, 1)
		requireDuplicateSections(t, singleReg, 0, 0)

		duplicated := objstore.NewInMemBucket()
		firstIndex := addIndexObject(t, duplicated, sectionOfTestObject)
		secondIndex := addIndexObject(t, duplicated, append(append([]indexedLine{}, sectionOfTestObject...), sectionOfOtherObject...))
		duplicatedMetastore, duplicatedReg, logs := newTestMetastore(duplicated)
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
		mstore, reg, _ := newTestMetastore(bucket)

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
		mstore, reg, _ := newTestMetastore(bucket)

		_, err := mstore.Sections(ctx, request)
		require.NoError(t, err)

		requireDuplicateSections(t, reg, 2, 0)
	})

	t.Run("returns the duplicate section once when two postings index objects list the same streams", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		addPostingsIndexObject(t, bucket, []postings.LabelObservation{observe("src-obj", 1), observe("src-obj", 2)})
		addPostingsIndexObject(t, bucket, []postings.LabelObservation{observe("src-obj", 1), observe("src-obj", 2), observe("other-obj", 1)})
		mstore, reg, _ := newTestMetastore(bucket)

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
		mstore, reg, logs := newTestMetastore(bucket)

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

func TestObjectMetastore_Sections(t *testing.T) {
	ctx := user.InjectOrgID(t.Context(), tenantID)
	request := SectionsRequest{
		Start:    now.Add(-time.Hour),
		End:      now.Add(time.Hour),
		Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")},
	}

	t.Run("returns no section with the error when the sequence fails after a batch", func(t *testing.T) {
		stream1 := postings.LabelObservation{ObjectPath: "src-obj", SectionIndex: 0, ColumnName: "app", LabelValue: "foo", StreamID: 1, Timestamp: now}
		stream2 := postings.LabelObservation{ObjectPath: "src-obj", SectionIndex: 0, ColumnName: "app", LabelValue: "foo", StreamID: 2, Timestamp: now}
		stream3 := postings.LabelObservation{ObjectPath: "src-obj", SectionIndex: 0, ColumnName: "app", LabelValue: "foo", StreamID: 3, Timestamp: now}
		bucket := objstore.NewInMemBucket()
		createIndexObject(ctx, t, bucket, []postings.LabelObservation{stream1, stream2})
		createIndexObject(ctx, t, bucket, []postings.LabelObservation{stream1, stream2, stream3})
		m, _, _ := newTestMetastore(bucket)

		resp, err := m.Sections(ctx, request)

		require.ErrorIs(t, err, errDifferentSectionStatistics)
		require.Empty(t, resp.Sections)
	})

	t.Run("returns no section with the error and reads nothing when the request has no matcher", func(t *testing.T) {
		inMem := objstore.NewInMemBucket()
		addIndexObject(t, inMem, sectionOf("object-a"))
		bucket := &getRecordingBucket{Bucket: inMem}
		m, _, _ := newTestMetastore(bucket)
		noMatcher := request
		noMatcher.Matchers = nil

		resp, err := m.Sections(ctx, noMatcher)

		require.ErrorIs(t, err, errNoMatchers)
		require.Empty(t, resp.Sections)
		require.Empty(t, bucket.names)
	})
}

func TestObjectMetastore_MetadataCache(t *testing.T) {
	ctx := user.InjectOrgID(t.Context(), tenantID)
	request := SectionsRequest{
		Start:    now.Add(-time.Hour),
		End:      now.Add(time.Hour),
		Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")},
	}

	t.Run("resolves a cached index object without reading it from object storage", func(t *testing.T) {
		inMem := objstore.NewInMemBucket()
		addIndexObject(t, inMem, sectionOf("object-a"))
		bucket := &getRecordingBucket{Bucket: inMem}
		m, _ := newTestMetastoreWithCache(bucket)

		cold, err := m.Sections(ctx, request)
		require.NoError(t, err)
		require.NotEmpty(t, cold.Sections)
		require.Positive(t, bucket.rangeReads.Load())

		bucket.rangeReads.Store(0)
		warm, err := m.Sections(ctx, request)
		require.NoError(t, err)
		require.Zero(t, bucket.rangeReads.Load())
		require.Equal(t, cold.Sections, warm.Sections)
	})

	t.Run("reads the ToC from object storage on every resolution and never caches it", func(t *testing.T) {
		inMem := objstore.NewInMemBucket()
		addIndexObject(t, inMem, sectionOf("object-a"))
		bucket := &getRecordingBucket{Bucket: inMem}
		m, memoryCache := newTestMetastoreWithCache(bucket)

		_, err := m.Sections(ctx, request)
		require.NoError(t, err)
		tocReadsPerResolution := len(bucket.tocReads())
		require.Positive(t, tocReadsPerResolution)

		_, err = m.Sections(ctx, request)
		require.NoError(t, err)

		require.Len(t, bucket.tocReads(), 2*tocReadsPerResolution)
		require.NotEmpty(t, memoryCache.GetInternal(), "the index object is cached, so the check below is not vacuous")
		for key := range memoryCache.GetInternal() {
			require.NotContains(t, key, TocPrefix)
		}
	})
}

func TestObjectMetastore_SectionsStream(t *testing.T) {
	ctx := user.InjectOrgID(t.Context(), tenantID)
	request := SectionsRequest{
		Start:    now.Add(-time.Hour),
		End:      now.Add(time.Hour),
		Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")},
	}

	// collectSectionBatches ranges over seq to its end. It returns the batches that seq yielded and
	// the error that ended it. It fails the test when seq yields an empty batch, a batch with an
	// error, or a value after an error.
	collectSectionBatches := func(t *testing.T, seq iter.Seq2[DataobjSectionDescriptors, error]) ([]DataobjSectionDescriptors, error) {
		t.Helper()

		var (
			batches []DataobjSectionDescriptors
			failure error
		)
		for batch, err := range seq {
			require.NoError(t, failure, "the sequence yielded after an error")
			if err != nil {
				require.Nil(t, batch, "the sequence yielded a batch with an error")
				failure = err
				continue
			}
			require.NotEmpty(t, batch, "the sequence yielded an empty batch")
			batches = append(batches, batch)
		}
		return batches, failure
	}

	// requireOnlyFirstIndexRecorded checks the metrics of a resolution that yielded one batch and then
	// failed or was canceled. Only the first-index metric and stat exist. A resolution records its
	// totals only when it ends without a failure or a cancel.
	requireOnlyFirstIndexRecorded := func(t *testing.T, reg *prometheus.Registry, capture *xcap.Capture) {
		t.Helper()

		require.Equal(t, uint64(1), gatherHistogram(t, reg, "loki_metastore_resolved_sections_first_index_duration_seconds").GetSampleCount())
		require.Positive(t, xcap.ValueFromRegion[int64](capture, RegionSections, StatMetastoreSectionsFirstIndexDuration))
		require.Zero(t, xcap.ValueFromRegion[int64](capture, RegionSections, StatMetastoreSectionsDuration))
		for _, name := range []string{
			"loki_metastore_stream_filter_sections_total",
			"loki_metastore_resolved_sections_total",
			"loki_metastore_resolved_sections_ratio",
			"loki_metastore_resolved_sections_total_duration_seconds",
		} {
			require.Zero(t, gatherHistogram(t, reg, name).GetSampleCount(), name)
		}
	}

	t.Run("yields the first batch while another index object is still blocked", func(t *testing.T) {
		inMem := objstore.NewInMemBucket()
		addIndexObject(t, inMem, sectionOf("object-fast"))
		slowIndex := addIndexObject(t, inMem, sectionOf("object-slow"))
		bucket := newGatedBucket(inMem)
		gate := bucket.hold(slowIndex, nil)
		m, _, _ := newTestMetastore(bucket)
		boundedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		next, stop := iter.Pull2(m.SectionsStream(boundedCtx, request))
		defer stop()

		batch, err, ok := next()
		require.NoError(t, boundedCtx.Err(), "the first batch waited for the blocked read")
		require.True(t, ok)
		require.NoError(t, err)
		require.Equal(t, []string{"object-fast"}, objectPathsOf(batch))
		gate.waitUntilHeld(t)

		gate.open()
		batch, err, ok = next()
		require.True(t, ok)
		require.NoError(t, err)
		require.Equal(t, []string{"object-slow"}, objectPathsOf(batch))

		_, _, ok = next()
		require.False(t, ok)
	})

	t.Run("stops the reads in flight and records only the first-index metric when the caller breaks after the first batch", func(t *testing.T) {
		inMem := objstore.NewInMemBucket()
		addIndexObject(t, inMem, sectionOf("object-fast"))
		slowIndex := addIndexObject(t, inMem, sectionOf("object-slow"))
		bucket := newGatedBucket(inMem)
		gate := bucket.hold(slowIndex, nil)
		gate.proceedOnCancel = true
		m, reg, _ := newTestMetastore(bucket)
		boundedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		captureCtx, capture := xcap.NewCapture(boundedCtx, nil)

		var batches int
		for batch, err := range m.SectionsStream(captureCtx, request) {
			require.NoError(t, err)
			require.Equal(t, []string{"object-fast"}, objectPathsOf(batch))
			gate.waitUntilHeld(t)
			batches++
			break
		}

		require.NoError(t, boundedCtx.Err(), "the loop waited for the blocked read")
		require.Equal(t, 1, batches)
		require.Zero(t, bucket.inFlight.Load(), "a read outlived the loop")
		requireOnlyFirstIndexRecorded(t, reg, capture)
	})

	t.Run("keeps reading the index objects while the caller holds the first batch and the next read waits for a free slot", func(t *testing.T) {
		inMem := objstore.NewInMemBucket()
		var indexes []string
		for _, object := range []string{"object-a", "object-b", "object-c", "object-d", "object-e"} {
			indexes = append(indexes, addIndexObject(t, inMem, sectionOf(object)))
		}
		slices.Sort(indexes)
		bucket := newGatedBucket(inMem)
		gate := bucket.hold(indexes[3], nil)
		m, _, _ := newTestMetastore(bucket)
		m.parallelism = 1
		boundedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		var batches int
		for _, err := range m.SectionsStream(boundedCtx, request) {
			require.NoError(t, err)
			gate.waitUntilHeld(t)
			batches++
			break
		}

		require.NoError(t, boundedCtx.Err(), "the loop waited for the blocked read")
		require.Equal(t, 1, batches)
		require.Zero(t, bucket.inFlight.Load(), "a read outlived the loop")
	})

	t.Run("ends the resolution and records its duration while the caller holds the first batch", func(t *testing.T) {
		inMem := objstore.NewInMemBucket()
		for _, object := range []string{"object-a", "object-b", "object-c", "object-d"} {
			addIndexObject(t, inMem, sectionOf(object))
		}
		resolved := make(chan struct{})
		var closeResolved sync.Once
		logger := log.LoggerFunc(func(keyvals ...any) error {
			for i := 0; i+1 < len(keyvals); i += 2 {
				if keyvals[i] == "msg" && keyvals[i+1] == "resolved sections" {
					closeResolved.Do(func() { close(resolved) })
				}
			}
			return nil
		})
		reg := prometheus.NewRegistry()
		m := NewObjectMetastore(inMem, Config{}, logger, NewObjectMetastoreMetrics(reg))
		boundedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		var batches int
		for _, err := range m.SectionsStream(boundedCtx, request) {
			require.NoError(t, err)

			select {
			case <-resolved:
			case <-boundedCtx.Done():
				t.Fatal("the resolution did not end while the caller held the first batch")
			}
			batches++
			break
		}

		require.Equal(t, 1, batches)
		require.Equal(t, uint64(1), gatherHistogram(t, reg, "loki_metastore_resolved_sections_total_duration_seconds").GetSampleCount())
	})

	t.Run("yields the context error and stops when the caller cancels the context after the first batch", func(t *testing.T) {
		inMem := objstore.NewInMemBucket()
		addIndexObject(t, inMem, sectionOf("object-fast"))
		slowIndex := addIndexObject(t, inMem, sectionOf("object-slow"))
		bucket := newGatedBucket(inMem)
		gate := bucket.hold(slowIndex, nil)
		m, _, _ := newTestMetastore(bucket)
		boundedCtx, cancelTimeout := context.WithTimeout(ctx, 10*time.Second)
		defer cancelTimeout()

		cancelCtx, cancel := context.WithCancel(boundedCtx)
		t.Cleanup(cancel)
		next, stop := iter.Pull2(m.SectionsStream(cancelCtx, request))
		defer stop()

		batch, err, ok := next()
		require.True(t, ok)
		require.NoError(t, err)
		require.Equal(t, []string{"object-fast"}, objectPathsOf(batch))
		gate.waitUntilHeld(t)

		cancel()
		batch, err, ok = next()
		require.True(t, ok)
		require.Nil(t, batch)
		require.ErrorIs(t, err, context.Canceled)

		_, _, ok = next()
		require.False(t, ok)

		require.Zero(t, bucket.inFlight.Load(), "a read outlived the loop")
	})

	t.Run("yields the error of the failed read, not the cancel error of another read, and records only the first-index metric", func(t *testing.T) {
		inMem := objstore.NewInMemBucket()
		addIndexObject(t, inMem, sectionOf("object-fast"))
		failingIndex := addIndexObject(t, inMem, sectionOf("object-failing"))
		slowIndex := addIndexObject(t, inMem, sectionOf("object-slow"))
		bucket := newGatedBucket(inMem)
		failingGate := bucket.hold(failingIndex, errors.New("storage unavailable"))
		slowGate := bucket.hold(slowIndex, nil)
		m, reg, _ := newTestMetastore(bucket)
		boundedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		captureCtx, capture := xcap.NewCapture(boundedCtx, nil)

		next, stop := iter.Pull2(m.SectionsStream(captureCtx, request))
		defer stop()

		batch, err, ok := next()
		require.True(t, ok)
		require.NoError(t, err)
		require.Equal(t, []string{"object-fast"}, objectPathsOf(batch))
		failingGate.waitUntilHeld(t)
		slowGate.waitUntilHeld(t)

		failingGate.open()
		_, err, ok = next()
		require.True(t, ok)
		require.ErrorContains(t, err, "storage unavailable")
		require.NotErrorIs(t, err, context.Canceled)

		_, _, ok = next()
		require.False(t, ok)

		require.NoError(t, boundedCtx.Err(), "the sequence waited for a blocked read")
		requireOnlyFirstIndexRecorded(t, reg, capture)
	})

	t.Run("yields an error that names the index object and the panic when a read panics", func(t *testing.T) {
		inMem := objstore.NewInMemBucket()
		panickingIndex := addIndexObject(t, inMem, sectionOf("object-a"))
		bucket := newGatedBucket(inMem)
		gate := bucket.hold(panickingIndex, errors.New("corrupt index object"))
		gate.panics = true
		gate.open()
		m, _, logs := newTestMetastore(bucket)

		batches, err := collectSectionBatches(t, m.SectionsStream(ctx, request))

		require.Empty(t, batches)
		require.EqualError(t, err, fmt.Sprintf("reading the sections of index object %q: corrupt index object", panickingIndex))
		require.Contains(t, logs.String(), "panic reading the sections of an index object")
	})

	t.Run("cancels the other index reads when one read fails, even while the caller handles a batch", func(t *testing.T) {
		inMem := objstore.NewInMemBucket()
		addIndexObject(t, inMem, sectionOf("object-fast"))
		failingIndex := addIndexObject(t, inMem, sectionOf("object-failing"))
		slowIndex := addIndexObject(t, inMem, sectionOf("object-slow"))
		bucket := newGatedBucket(inMem)
		failingGate := bucket.hold(failingIndex, errors.New("storage unavailable"))
		slowGate := bucket.hold(slowIndex, nil)
		m, _, _ := newTestMetastore(bucket)
		boundedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		var batches int
		for batch, err := range m.SectionsStream(boundedCtx, request) {
			require.NoError(t, err)
			require.Equal(t, []string{"object-fast"}, objectPathsOf(batch))

			failingGate.waitUntilHeld(t)
			slowGate.waitUntilHeld(t)
			failingGate.open()
			slowGate.waitUntilLeft(t)
			batches++
			break
		}

		require.NoError(t, boundedCtx.Err(), "the loop waited for a blocked read")
		require.Equal(t, 1, batches)
		require.Zero(t, bucket.inFlight.Load(), "a read outlived the loop")
	})

	t.Run("yields every section and reads as many index objects at once as the parallelism limit allows", func(t *testing.T) {
		inMem := objstore.NewInMemBucket()
		var want []string
		for i := range 12 {
			object := fmt.Sprintf("object-%d", i)
			addIndexObject(t, inMem, sectionOf(object))
			want = append(want, object)
		}
		bucket := newGatedBucket(inMem)
		bucket.latency = 2 * time.Millisecond
		m, _, _ := newTestMetastore(bucket)
		m.parallelism = 2
		boundedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		var got []string
		for batch, err := range m.SectionsStream(boundedCtx, request) {
			require.NoError(t, err)
			got = append(got, objectPathsOf(batch)...)
		}

		require.ElementsMatch(t, want, got)
		require.Equal(t, 2, bucket.peakObjects, "the index objects were not read at the limit")
	})

	t.Run("runs a new resolution each time the caller ranges over the sequence", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		addIndexObject(t, bucket, sectionOf("object-a"))
		addIndexObject(t, bucket, sectionOf("object-b"))
		m, reg, _ := newTestMetastore(bucket)
		sequence := m.SectionsStream(ctx, request)

		for range 2 {
			batches, err := collectSectionBatches(t, sequence)
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"object-a", "object-b"}, objectPathsOf(slices.Concat(batches...)))
		}

		require.Equal(t, uint64(2), gatherHistogram(t, reg, "loki_metastore_resolved_sections_total_duration_seconds").GetSampleCount())
		require.Equal(t, uint64(2), gatherHistogram(t, reg, "loki_metastore_resolved_sections_first_index_duration_seconds").GetSampleCount())
	})

	t.Run("does no work until the caller ranges over the sequence", func(t *testing.T) {
		inMem := objstore.NewInMemBucket()
		addIndexObject(t, inMem, sectionOf("object-a"))
		bucket := &getRecordingBucket{Bucket: inMem}
		m, _, _ := newTestMetastore(bucket)

		_ = m.SectionsStream(ctx, request)

		require.Empty(t, bucket.names)
	})

	t.Run("records the totals, the ratio and both durations when two index objects list the same section", func(t *testing.T) {
		otherApp := indexedLine{objectPath: "other-path", section: 1, streamID: 2, labels: `{app="bar"}`, timestamp: now}
		bucket := objstore.NewInMemBucket()
		addIndexObject(t, bucket, sectionOf("test-path"))
		addIndexObject(t, bucket, append(sectionOf("test-path"), otherApp))
		m, reg, _ := newTestMetastore(bucket)
		captureCtx, capture := xcap.NewCapture(ctx, nil)

		_, err := collectSectionBatches(t, m.SectionsStream(captureCtx, request))

		require.NoError(t, err)
		resolved := gatherHistogram(t, reg, "loki_metastore_resolved_sections_total")
		require.Equal(t, uint64(1), resolved.GetSampleCount())
		require.Equal(t, float64(1), resolved.GetSampleSum())
		require.Equal(t, float64(2), gatherHistogram(t, reg, "loki_metastore_stream_filter_sections_total").GetSampleSum())
		require.InDelta(t, 0.5, gatherHistogram(t, reg, "loki_metastore_resolved_sections_ratio").GetSampleSum(), 1e-9)
		require.Equal(t, uint64(1), gatherHistogram(t, reg, "loki_metastore_resolved_sections_first_index_duration_seconds").GetSampleCount())
		require.Equal(t, uint64(1), gatherHistogram(t, reg, "loki_metastore_resolved_sections_total_duration_seconds").GetSampleCount())
		firstIndex := xcap.ValueFromRegion[int64](capture, RegionSections, StatMetastoreSectionsFirstIndexDuration)
		total := xcap.ValueFromRegion[int64](capture, RegionSections, StatMetastoreSectionsDuration)
		require.Positive(t, firstIndex)
		require.LessOrEqual(t, firstIndex, total)
	})

	t.Run("fails once and counts one diverged duplicate when several index objects list different streams of a section", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		for streams := 1; streams <= 4; streams++ {
			var observations []postings.LabelObservation
			for streamID := 1; streamID <= streams; streamID++ {
				observations = append(observations, postings.LabelObservation{ObjectPath: "src-obj", SectionIndex: 0, ColumnName: "app", LabelValue: "foo", StreamID: int64(streamID), Timestamp: now})
			}
			createIndexObject(ctx, t, bucket, observations)
		}
		m, _, logs := newTestMetastore(bucket)
		m.parallelism = 1

		_, err := collectSectionBatches(t, m.SectionsStream(ctx, request))

		require.ErrorIs(t, err, errDifferentSectionStatistics)
		require.Equal(t, float64(1), testutil.ToFloat64(m.metrics.duplicateSectionsTotal.WithLabelValues(divergedTrue)))
		require.Equal(t, 1, strings.Count(logs.String(), "level=error"))
	})

	t.Run("yields no batch and records no first-index metric when no section resolves", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		addIndexObject(t, bucket, sectionOf("object-a"))
		m, reg, _ := newTestMetastore(bucket)
		unmatched := request
		unmatched.Matchers = []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "doesnotexist")}
		captureCtx, capture := xcap.NewCapture(ctx, nil)

		batches, err := collectSectionBatches(t, m.SectionsStream(captureCtx, unmatched))

		require.NoError(t, err)
		require.Empty(t, batches)
		require.Zero(t, gatherHistogram(t, reg, "loki_metastore_resolved_sections_first_index_duration_seconds").GetSampleCount())
		require.Zero(t, xcap.ValueFromRegion[int64](capture, RegionSections, StatMetastoreSectionsFirstIndexDuration))
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
			name: "not-equal predicate returns no label names",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "bar"),
			},
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchNotEqual, "env", "prod"),
			},
			start:      now.Add(-4 * time.Hour),
			end:        now.Add(time.Hour),
			wantCount:  1,
			wantLabels: []string{},
		},
		{
			name: "regex-match predicate returns no label names",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "bar"),
			},
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "env", "d.*"),
			},
			start:      now.Add(-4 * time.Hour),
			end:        now.Add(time.Hour),
			wantCount:  1,
			wantLabels: []string{},
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

func TestNewSectionDescriptor(t *testing.T) {
	t.Run("copies every field of the row and removes duplicate ambiguous names", func(t *testing.T) {
		start, end := now.Add(-2*time.Hour), now.Add(-1*time.Hour)

		desc := newSectionDescriptor(resolvedRow{
			objectPath:       "test-path",
			sectionIndex:     3,
			streamID:         7,
			minTimestamp:     start.UnixNano(),
			maxTimestamp:     end.UnixNano(),
			rowCount:         10,
			uncompressedSize: 250,
			ambiguousNames:   []string{"a", "a", "b"},
		})

		require.Equal(t, SectionKey{ObjectPath: "test-path", SectionIdx: 3}, desc.SectionKey)
		require.Equal(t, []int64{7}, desc.StreamIDs)
		require.Equal(t, 10, desc.RowCount)
		require.Equal(t, int64(250), desc.Size)
		require.Equal(t, start.UnixNano(), desc.Start.UnixNano())
		require.Equal(t, end.UnixNano(), desc.End.UnixNano())
		require.Equal(t, []string{"a", "b"}, desc.AmbiguousPredicates)
	})
}

func TestDataobjSectionDescriptor_Merge(t *testing.T) {
	t.Run("keeps the time range when the merged row lies inside it", func(t *testing.T) {
		desc := newSectionDescriptor(resolvedRow{streamID: 1, minTimestamp: 100, maxTimestamp: 400, rowCount: 10, uncompressedSize: 100})

		desc.merge(resolvedRow{streamID: 2, minTimestamp: 200, maxTimestamp: 300, rowCount: 5, uncompressedSize: 50})

		require.Equal(t, int64(100), desc.Start.UnixNano())
		require.Equal(t, int64(400), desc.End.UnixNano())
		require.Equal(t, []int64{1, 2}, desc.StreamIDs)
		require.Equal(t, 15, desc.RowCount)
		require.Equal(t, int64(150), desc.Size)
	})

	t.Run("adds the ambiguous names of the row when the descriptor has none", func(t *testing.T) {
		desc := newSectionDescriptor(resolvedRow{streamID: 1})

		desc.merge(resolvedRow{streamID: 2, ambiguousNames: []string{"env"}})

		require.Equal(t, []string{"env"}, desc.AmbiguousPredicates)
	})

	t.Run("adds only the ambiguous names that the descriptor lacks", func(t *testing.T) {
		desc := newSectionDescriptor(resolvedRow{ambiguousNames: []string{"a", "b"}})

		desc.merge(resolvedRow{ambiguousNames: []string{"b", "c"}})

		require.Equal(t, []string{"a", "b", "c"}, desc.AmbiguousPredicates)
	})
}

func newTestDataBuilder(t testing.TB) *testDataBuilder {
	bucket := objstore.NewInMemBucket()

	builder, err := logsobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,      // 1MB
		TargetObjectSize:        10 * 1024 * 1024, // 10MB
		TargetSectionSize:       1024 * 1024,      // 1MB
		BufferSize:              1024 * 1024,      // 1MB
		SectionStripeMergeLimit: 2,
	}, nil, logsobj.NewBuilderMetrics(), log.NewNopLogger(), logsobj.NoTenantOverrides{})
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
	obj, closer := buildIndexObject(t, tenantID, []postings.LabelObservation{
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
		indexPath := createIndexObject(ctx, t, bucket, []postings.LabelObservation{
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
		indexPath := createIndexObject(ctx, t, bucket, []postings.LabelObservation{
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

// newTestMetastore returns a metastore over bucket, the registry of its metrics and the buffer that
// receives its logs. Several goroutines of one resolution can log, so the buffer sits behind a lock.
// Read the buffer after the resolution ends.
func newTestMetastore(bucket objstore.Bucket) (*ObjectMetastore, *prometheus.Registry, *bytes.Buffer) {
	reg := prometheus.NewRegistry()
	logs := bytes.NewBuffer(nil)
	logger := log.NewLogfmtLogger(log.NewSyncWriter(logs))
	return NewObjectMetastore(bucket, Config{}, logger, NewObjectMetastoreMetrics(reg)), reg, logs
}

// createIndexObject uploads an index object with one postings section and one stats section for
// each of layouts, adds it to the ToC, and returns its path. With no layouts, the index object has
// no stats section.
func createIndexObject(ctx context.Context, t testing.TB, bucket objstore.Bucket, observations []postings.LabelObservation, layouts ...streams.Layout) string {
	t.Helper()

	obj, closer := buildIndexObject(t, tenantID, observations, layouts...)
	t.Cleanup(closer)
	return uploadAndRegisterIndexObject(ctx, t, bucket, obj)
}

// uploadAndRegisterIndexObject uploads obj to bucket, adds it to the ToC, and returns its path.
func uploadAndRegisterIndexObject(ctx context.Context, t testing.TB, bucket objstore.Bucket, obj *dataobj.Object) string {
	t.Helper()

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

// buildIndexObject builds an index object that holds a tenant postings section with the given label
// postings and one tenant stats section for each of layouts. A stats section records its layout, as
// the index builder writes it. With no layouts, the object has no stats section. observations must
// not be empty when layouts is not.
func buildIndexObject(t testing.TB, tenant string, observations []postings.LabelObservation, layouts ...streams.Layout) (*dataobj.Object, func()) {
	t.Helper()

	postingsBuilder := postings.NewBuilder(nil, 0, 0, 1<<20)
	postingsBuilder.SetTenant(tenant)
	for _, observation := range observations {
		postingsBuilder.ObserveLabelPosting(observation)
	}

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(postingsBuilder))
	for i, layout := range layouts {
		statsBuilder := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(2048, 1000), layout)
		statsBuilder.SetTenant(tenant)
		statsBuilder.Append(stats.Stat{ObjectPath: observations[0].ObjectPath, SectionIndex: observations[0].SectionIndex + int64(i)})
		require.NoError(t, objBuilder.Append(statsBuilder))
	}
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	return obj, func() { _ = closer.Close() }
}

// hashRangeOfBuckets returns the hash range that covers exactly the buckets of bucketRange in
// layout. It returns nil if bucketRange is nil.
func hashRangeOfBuckets(layout streams.ShardLayout, bucketRange *postings.ShardBucketRange) *streams.HashRange {
	if bucketRange == nil {
		return nil
	}
	width := uint(64 - layout.Bits())
	return &streams.HashRange{
		From: uint64(bucketRange.From) << width,
		To:   uint64(bucketRange.To)<<width | (1<<width - 1),
	}
}

func objectPathsOf(sections []*DataobjSectionDescriptor) []string {
	paths := make([]string, 0, len(sections))
	for _, section := range sections {
		paths = append(paths, section.ObjectPath)
	}
	return paths
}

// gatherHistogram returns the histogram that reg holds under name.
func gatherHistogram(t *testing.T, reg *prometheus.Registry, name string) *dto.Histogram {
	t.Helper()

	families, err := dskitmetrics.NewMetricFamilyMapFromGatherer(reg)
	require.NoError(t, err)
	histogram, err := dskitmetrics.FindHistogramWithNameAndLabels(families, name)
	require.NoError(t, err)
	return histogram
}

// sectionOf returns the lines of an index object that lists one section of the log object at
// objectPath.
func sectionOf(objectPath string) []indexedLine {
	return []indexedLine{{objectPath: objectPath, section: 1, streamID: 1, labels: `{app="foo", env="prod"}`, timestamp: now}}
}

// gatedBucket holds back or fails the reads of chosen objects. It counts the reads in flight and the
// most objects read at once.
type gatedBucket struct {
	objstore.Bucket

	// latency delays every read, so that concurrent reads overlap. Set it before the first read.
	latency time.Duration

	mtx     sync.Mutex
	gates   map[string]*readGate
	reading map[string]int

	// peakObjects is the most objects with a read in flight at once. Read it after the reads end.
	peakObjects int

	inFlight atomic.Int64
}

func newGatedBucket(bucket objstore.Bucket) *gatedBucket {
	return &gatedBucket{Bucket: bucket, gates: make(map[string]*readGate), reading: make(map[string]int)}
}

// hold makes every read of the object at path wait. A read ends when the gate opens or its context
// ends. Once the gate opens, the reads fail with err, or go through when err is nil.
func (b *gatedBucket) hold(path string, err error) *readGate {
	gate := &readGate{entered: make(chan struct{}), left: make(chan struct{}), release: make(chan struct{}), err: err}

	b.mtx.Lock()
	defer b.mtx.Unlock()
	b.gates[path] = gate
	return gate
}

// enter counts a read in flight and holds it at the gate of its object. It returns the error that
// the read must fail with. The caller must call leave when the read ends, also when err is set.
func (b *gatedBucket) enter(ctx context.Context, name string) (leave func(), err error) {
	b.inFlight.Add(1)
	b.mtx.Lock()
	b.reading[name]++
	b.peakObjects = max(b.peakObjects, len(b.reading))
	b.mtx.Unlock()
	leave = func() {
		b.inFlight.Add(-1)

		b.mtx.Lock()
		defer b.mtx.Unlock()
		if b.reading[name]--; b.reading[name] == 0 {
			delete(b.reading, name)
		}
	}

	if b.latency > 0 {
		select {
		case <-time.After(b.latency):
		case <-ctx.Done():
			return leave, ctx.Err()
		}
	}

	b.mtx.Lock()
	gate := b.gates[name]
	b.mtx.Unlock()
	if gate == nil {
		return leave, nil
	}

	gate.enteredOnce.Do(func() { close(gate.entered) })
	defer gate.leftOnce.Do(func() { close(gate.left) })
	select {
	case <-gate.release:
		if gate.panics {
			panic(gate.err)
		}
		return leave, gate.err
	case <-ctx.Done():
		if gate.proceedOnCancel {
			return leave, nil
		}
		return leave, ctx.Err()
	}
}

func (b *gatedBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	leave, err := b.enter(ctx, name)
	defer leave()
	if err != nil {
		return nil, err
	}
	return b.Bucket.Get(ctx, name)
}

func (b *gatedBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	leave, err := b.enter(ctx, name)
	defer leave()
	if err != nil {
		return nil, err
	}
	return b.Bucket.GetRange(ctx, name, off, length)
}

func (b *gatedBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	leave, err := b.enter(ctx, name)
	defer leave()
	if err != nil {
		return objstore.ObjectAttributes{}, err
	}
	return b.Bucket.Attributes(ctx, name)
}

// readGate holds the reads of one object until the test opens it.
type readGate struct {
	entered     chan struct{}
	enteredOnce sync.Once
	left        chan struct{}
	leftOnce    sync.Once
	release     chan struct{}
	err         error

	// proceedOnCancel lets a held read go through, instead of fail, when its context ends. It models
	// a read that ends without an error after the cancel. Set it before the first read.
	proceedOnCancel bool

	// panics makes a read panic with err when the gate opens. Set it before the first read.
	panics bool
}

// waitUntilHeld blocks until a read of the object reaches the gate.
func (g *readGate) waitUntilHeld(t *testing.T) {
	t.Helper()

	select {
	case <-g.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("no read reached the gate")
	}
}

// waitUntilLeft blocks until a held read leaves the gate, whether the gate opened or not.
func (g *readGate) waitUntilLeft(t *testing.T) {
	t.Helper()

	select {
	case <-g.left:
	case <-time.After(10 * time.Second):
		t.Fatal("no read left the gate")
	}
}

func (g *readGate) open() {
	close(g.release)
}

// newTestMetastoreWithCache returns a metastore that caches index objects in the returned in-memory
// backend.
func newTestMetastoreWithCache(bucket objstore.Bucket) (*ObjectMetastore, cache.MockCache) {
	memoryCache := cache.NewMockCache()
	metadataCache := metadatacache.NewCache(memoryCache, 0, metadatacache.KindIndex, metadatacache.NewMetrics(nil), nil)
	return NewObjectMetastore(bucket, Config{}, log.NewNopLogger(), NewObjectMetastoreMetrics(prometheus.NewRegistry()), WithIndexMetadataCache(metadataCache)), memoryCache
}
