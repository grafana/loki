package metastore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v4/pkg/dataobj"
	"github.com/grafana/loki/v4/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v4/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v4/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v4/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v4/pkg/dataobj/uploader"
	"github.com/grafana/loki/v4/pkg/logql/syntax"
)

type readSectionsBenchmarkParams struct {
	name          string
	indexFilesNum int
}

func BenchmarkReadSections(b *testing.B) {
	benchmarks := []readSectionsBenchmarkParams{
		{
			name:          "single index file",
			indexFilesNum: 1,
		},
		{
			name:          "multiple index files",
			indexFilesNum: 200,
		},
	}
	for _, bm := range benchmarks {
		benchmarkReadSections(b, bm)
	}
}

func benchmarkReadSections(b *testing.B, bm readSectionsBenchmarkParams) {
	b.Run(bm.name, func(b *testing.B) {
		ctx := context.Background()
		bucket := objstore.NewInMemBucket()

		objUploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
		require.NoError(b, objUploader.RegisterMetrics(prometheus.DefaultRegisterer))

		metastoreTocWriter := NewTableOfContentsWriter(bucket, DefaultTocWriterBackoffConfig, DefaultTocBuilderConfig, log.NewNopLogger(), NewTocWriterMetrics(nil), indexobj.NewBuilderMetrics(nil))

		// Calculate how many streams per index file
		streamsPerIndex := len(testStreams) / bm.indexFilesNum
		if streamsPerIndex == 0 {
			streamsPerIndex = 1
		}

		// Track global stream ID counter across all index files
		globalStreamID := int64(0)

		// Create multiple index files
		for fileIdx := 0; fileIdx < bm.indexFilesNum; fileIdx++ {
			// Create index builder for this file
			builder, err := indexobj.NewBuilder(tenantID, logsobj.BuilderBaseConfig{
				TargetPageSize:          1024 * 1024,
				TargetObjectSize:        10 * 1024 * 1024,
				TargetSectionSize:       128,
				BufferSize:              1024 * 1024,
				SectionStripeMergeLimit: 2,
			}, nil, indexobj.NewBuilderMetrics(nil))
			require.NoError(b, err)

			// Determine which streams to add to this index file
			// Use modulo to cycle through testStreams if we need more entries than available
			startIdx := fileIdx * streamsPerIndex
			endIdx := startIdx + streamsPerIndex
			if fileIdx == bm.indexFilesNum-1 {
				// Last file gets all remaining streams needed to reach the desired count
				endIdx = startIdx + streamsPerIndex + (len(testStreams)-endIdx%len(testStreams))%len(testStreams)
			}

			// Add test streams to this index file, cycling through testStreams if necessary
			for i := startIdx; i < endIdx; i++ {
				streamIdx := i % len(testStreams)
				ts := testStreams[streamIdx]
				lbls, err := syntax.ParseLabels(ts.Labels)
				require.NoError(b, err)

				observeLabelPostings(builder, "test-path", int64(fileIdx+1), globalStreamID, lbls, ts.Entries[0].Timestamp, int64(len(ts.Entries[0].Line)))

				globalStreamID++
			}

			// Build and store the index object
			timeRanges := []dataobj.TimeRange{builder.TimeRange()}
			obj, closer, err := builder.Flush()
			require.NoError(b, err)
			b.Cleanup(func() { _ = closer.Close() })

			path, err := objUploader.Upload(context.Background(), obj)
			require.NoError(b, err)

			err = writeTimeRanges(context.Background(), metastoreTocWriter, path, timeRanges)
			require.NoError(b, err)
		}

		// Create the metastore instance
		mstore := newTestObjectMetastore(bucket)

		// Prepare benchmark parameters
		benchCtx := user.InjectOrgID(ctx, tenantID)
		start := now.Add(-5 * time.Hour)
		end := now.Add(5 * time.Hour)
		matchers := []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
		}

		b.ResetTimer()
		b.ReportAllocs()

		// Run the benchmark
		for range b.N {
			sectionsResp, err := mstore.Sections(benchCtx, SectionsRequest{
				Start:    start,
				End:      end,
				Matchers: matchers,
			})
			require.NoError(b, err)
			require.NotEmpty(b, sectionsResp.Sections)
		}

		// Stop timer before cleanup
		b.StopTimer()
	})
}

func BenchmarkSections_PredicateMatchers(b *testing.B) {
	cases := []struct {
		name       string
		predicates []*labels.Matcher
		wantCount  int
	}{
		{
			name: "single predicate hit",
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "traceID", "abcd"),
			},
			wantCount: 1,
		},
		{
			name: "multiple predicate hit",
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "traceID", "abcd"),
				labels.MustNewMatcher(labels.MatchEqual, "spanID", "wxyz"),
			},
			wantCount: 1,
		},
		{
			name: "predicate miss",
			predicates: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "traceID", "cdef"),
			},
			wantCount: 0,
		},
	}

	for _, tt := range cases {
		b.Run(tt.name, func(b *testing.B) {
			ctx := user.InjectOrgID(context.Background(), tenantID)

			builder, err := indexobj.NewBuilder(tenantID, logsobj.BuilderBaseConfig{
				TargetPageSize:          1024 * 1024,
				TargetObjectSize:        10 * 1024 * 1024,
				TargetSectionSize:       128,
				BufferSize:              1024 * 1024,
				SectionStripeMergeLimit: 2,
			}, nil, indexobj.NewBuilderMetrics(nil))
			require.NoError(b, err)

			lbls := labels.New(labels.Label{Name: "app", Value: "foo"})

			observeLabelPostings(builder, "test-path", 0, 1, lbls, now.Add(-3*time.Hour), 5)
			observeLabelPostings(builder, "test-path", 0, 1, lbls, now.Add(-2*time.Hour), 0)
			observeBloomPostings(b, builder, "test-path", 0, 1, now.Add(-3*time.Hour), "traceID", "abcd", "1234")
			observeBloomPostings(b, builder, "test-path", 0, 1, now.Add(-3*time.Hour), "spanID", "wxyz")

			timeRanges := []dataobj.TimeRange{builder.TimeRange()}
			require.Len(b, timeRanges, 1)

			obj, closer, err := builder.Flush()
			require.NoError(b, err)
			b.Cleanup(func() { _ = closer.Close() })

			bucket := objstore.NewInMemBucket()

			objUploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())
			require.NoError(b, objUploader.RegisterMetrics(prometheus.DefaultRegisterer))

			path, err := objUploader.Upload(context.Background(), obj)
			require.NoError(b, err)

			metastoreTocWriter := NewTableOfContentsWriter(bucket, DefaultTocWriterBackoffConfig, DefaultTocBuilderConfig, log.NewNopLogger(), NewTocWriterMetrics(nil), indexobj.NewBuilderMetrics(nil))
			err = writeTimeRanges(context.Background(), metastoreTocWriter, path, timeRanges)
			require.NoError(b, err)

			mstore := newTestObjectMetastore(bucket)

			matchers := []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "app", "foo"),
			}

			start := now.Add(-3 * time.Hour)
			end := now.Add(time.Hour)

			b.ResetTimer()
			b.ReportAllocs()

			for range b.N {
				sectionsResp, err := mstore.Sections(ctx, SectionsRequest{
					Start:      start,
					End:        end,
					Matchers:   matchers,
					Predicates: tt.predicates,
				})
				require.NoError(b, err)
				require.Len(b, sectionsResp.Sections, tt.wantCount)
			}

			b.StopTimer()
		})
	}
}

// BenchmarkSections_ShardBucketRange resolves sections with and without a stream hash range. Each
// log object holds two adjacent buckets. The index records its layout in a stats section, so a
// resolution with a range also reads that layout.
func BenchmarkSections_ShardBucketRange(b *testing.B) {
	const (
		objects           = 256
		sectionsPerObject = 4
		streamsPerSection = 8
	)

	ctx := user.InjectOrgID(b.Context(), tenantID)
	bucket := objstore.NewInMemBucket()
	b.Cleanup(func() { require.NoError(b, bucket.Close()) })

	var observations []postings.LabelObservation
	for object := range objects {
		firstBucket := uint32(object) % (streams.DefaultShardLayout().Buckets() - 1)
		for section := range sectionsPerObject {
			for stream := range streamsPerSection {
				streamLabels := []labels.Label{
					{Name: "app", Value: "foo"},
					{Name: "pod", Value: fmt.Sprintf("pod-%d-%d-%d", object, section, stream)},
				}
				for _, label := range streamLabels {
					observations = append(observations, postings.LabelObservation{
						ObjectPath:   fmt.Sprintf("object-%d", object),
						SectionIndex: int64(section),
						ColumnName:   label.Name,
						LabelValue:   label.Value,
						StreamID:     int64(stream + 1),
						Timestamp:    now,
						ShardBucket:  firstBucket + uint32(stream%2),
					})
				}
			}
		}
	}
	createIndexObject(ctx, b, bucket, observations, streams.NewLayout(streams.DefaultShardLayout(), nil))
	m := newTestObjectMetastore(bucket)

	for _, test := range []struct {
		name        string
		bucketRange *postings.ShardBucketRange
	}{
		{name: "no range", bucketRange: nil},
		{name: "a range of one bucket", bucketRange: &postings.ShardBucketRange{From: 5, To: 5}},
		{name: "a range of a quarter of the buckets", bucketRange: &postings.ShardBucketRange{From: 0, To: streams.DefaultShardLayout().Buckets()/4 - 1}},
	} {
		b.Run(test.name, func(b *testing.B) {
			request := SectionsRequest{
				Start:           now.Add(-time.Hour),
				End:             now.Add(time.Hour),
				Matchers:        []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")},
				StreamHashRange: hashRangeOfBuckets(streams.DefaultShardLayout(), test.bucketRange),
			}

			b.ReportAllocs()
			b.ResetTimer()

			var sections int
			for range b.N {
				resp, err := m.Sections(ctx, request)
				require.NoError(b, err)
				require.NotEmpty(b, resp.Sections)
				sections = len(resp.Sections)
			}
			b.ReportMetric(float64(sections), "sections")
		})
	}
}
