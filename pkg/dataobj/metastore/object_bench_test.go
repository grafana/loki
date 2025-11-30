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

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
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
		require.NoError(b, objUploader.RegisterMetrics(prometheus.NewPedanticRegistry()))

		metastoreTocWriter := NewTableOfContentsWriter(bucket, log.NewNopLogger())

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
			builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
				TargetPageSize:          1024 * 1024,
				TargetObjectSize:        10 * 1024 * 1024,
				TargetSectionSize:       128,
				BufferSize:              1024 * 1024,
				SectionStripeMergeLimit: 2,
			}, nil)
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

				newIdx, err := builder.AppendStream(tenantID, streams.Stream{
					ID:               globalStreamID,
					Labels:           lbls,
					MinTimestamp:     ts.Entries[0].Timestamp,
					MaxTimestamp:     ts.Entries[0].Timestamp,
					UncompressedSize: 0,
				})
				require.NoError(b, err)

				err = builder.ObserveLogLine(tenantID, "test-path", int64(fileIdx+1), newIdx, globalStreamID, ts.Entries[0].Timestamp, int64(len(ts.Entries[0].Line)))
				require.NoError(b, err)

				globalStreamID++
			}

			// Build and store the index object
			timeRanges := builder.TimeRanges()
			obj, closer, err := builder.Flush()
			require.NoError(b, err)
			b.Cleanup(func() { _ = closer.Close() })

			path, err := objUploader.Upload(context.Background(), obj)
			require.NoError(b, err)

			err = metastoreTocWriter.WriteEntry(context.Background(), path, timeRanges)
			require.NoError(b, err)
		}

		// Create the metastore instance
		mstore := NewObjectMetastore(bucket, log.NewNopLogger(), nil)

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
			sections, err := mstore.Sections(benchCtx, start, end, matchers, nil)
			require.NoError(b, err)
			require.NotEmpty(b, sections)
		}

		// Stop timer before cleanup
		b.StopTimer()
	})
}
