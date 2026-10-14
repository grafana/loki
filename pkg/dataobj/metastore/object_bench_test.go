package metastore

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
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

				newIdx, err := builder.AppendStream(streams.Stream{
					ID:               globalStreamID,
					Labels:           lbls,
					MinTimestamp:     ts.Entries[0].Timestamp,
					MaxTimestamp:     ts.Entries[0].Timestamp,
					UncompressedSize: 0,
				})
				require.NoError(b, err)

				observeLabelPostings(builder, "test-path", int64(fileIdx+1), newIdx, lbls, ts.Entries[0].Timestamp, int64(len(ts.Entries[0].Line)))

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

func BenchmarkSectionsForPredicateMatchers(b *testing.B) {
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

			_, err = builder.AppendStream(streams.Stream{
				ID:               1,
				Labels:           lbls,
				MinTimestamp:     now.Add(-3 * time.Hour),
				MaxTimestamp:     now.Add(-2 * time.Hour),
				UncompressedSize: 5,
			})
			require.NoError(b, err)

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
