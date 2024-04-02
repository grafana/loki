package stats

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func TestResult(t *testing.T) {
	stats, ctx := NewContext(context.Background())

	stats.AddHeadChunkBytes(10)
	stats.AddHeadChunkLines(20)
	stats.AddDecompressedBytes(40)
	stats.AddDecompressedLines(20)
	stats.AddCompressedBytes(30)
	stats.AddDuplicates(10)
	stats.AddChunksRef(50)
	stats.AddChunksDownloaded(60)
	stats.AddChunksDownloadTime(time.Second)
	stats.AddCacheRequest(ChunkCache, 3)
	stats.AddCacheRequest(IndexCache, 4)
	stats.AddCacheRequest(ResultCache, 1)
	stats.SetQueryReferencedStructuredMetadata()
	stats.AddPipelineWrapperFilterdLines(1)

	fakeIngesterQuery(ctx)
	fakeIngesterQuery(ctx)

	res := stats.Result(2*time.Second, 2*time.Nanosecond, 10)
	res.Log(util_log.Logger)
	expected := Result{
		Ingester: Ingester{
			TotalChunksMatched: 200,
			TotalBatches:       50,
			TotalLinesSent:     60,
			TotalReached:       2,
			Store: Store{
				PipelineWrapperFilteredLines: 2,
				Chunk: Chunk{
					HeadChunkBytes:    10,
					HeadChunkLines:    20,
					DecompressedBytes: 24,
					DecompressedLines: 40,
					CompressedBytes:   60,
					TotalDuplicates:   2,
				},
			},
		},
		Querier: Querier{
			Store: Store{
				TotalChunksRef:               50,
				TotalChunksDownloaded:        60,
				ChunksDownloadTime:           time.Second.Nanoseconds(),
				QueryReferencedStructured:    true,
				PipelineWrapperFilteredLines: 1,
				Chunk: Chunk{
					HeadChunkBytes:    10,
					HeadChunkLines:    20,
					DecompressedBytes: 40,
					DecompressedLines: 20,
					CompressedBytes:   30,
					TotalDuplicates:   10,
				},
			},
		},
		Caches: Caches{
			Chunk: Cache{
				Requests: 3,
			},
			Index: Cache{
				Requests: 4,
			},
			Result: Cache{
				Requests: 1,
			},
		},
		Summary: Summary{
			ExecTime:                2 * time.Second.Seconds(),
			QueueTime:               2 * time.Nanosecond.Seconds(),
			BytesProcessedPerSecond: int64(42),
			LinesProcessedPerSecond: int64(50),
			TotalBytesProcessed:     int64(84),
			TotalLinesProcessed:     int64(100),
			TotalEntriesReturned:    int64(10),
		},
	}
	require.Equal(t, expected, res)
}

func TestSnapshot_JoinResults(t *testing.T) {
	statsCtx, ctx := NewContext(context.Background())
	expected := Result{
		Ingester: Ingester{
			TotalChunksMatched: 200,
			TotalBatches:       50,
			TotalLinesSent:     60,
			TotalReached:       2,
			Store: Store{
				QueryReferencedStructured: true,
				Chunk: Chunk{
					HeadChunkBytes:    10,
					HeadChunkLines:    20,
					DecompressedBytes: 24,
					DecompressedLines: 40,
					CompressedBytes:   60,
					TotalDuplicates:   2,
				},
			},
		},
		Querier: Querier{
			Store: Store{
				TotalChunksRef:            50,
				TotalChunksDownloaded:     60,
				ChunksDownloadTime:        time.Second.Nanoseconds(),
				QueryReferencedStructured: true,
				Chunk: Chunk{
					HeadChunkBytes:    10,
					HeadChunkLines:    20,
					DecompressedBytes: 40,
					DecompressedLines: 20,
					CompressedBytes:   30,
					TotalDuplicates:   10,
				},
			},
		},
		Summary: Summary{
			ExecTime:                2 * time.Second.Seconds(),
			QueueTime:               2 * time.Nanosecond.Seconds(),
			BytesProcessedPerSecond: int64(42),
			LinesProcessedPerSecond: int64(50),
			TotalBytesProcessed:     int64(84),
			TotalLinesProcessed:     int64(100),
			TotalEntriesReturned:    int64(10),
		},
	}

	JoinResults(ctx, expected)
	res := statsCtx.Result(2*time.Second, 2*time.Nanosecond, 10)
	require.Equal(t, expected, res)
}

func fakeIngesterQuery(ctx context.Context) {
	FromContext(ctx).AddIngesterReached(1)
	JoinIngesters(ctx, Ingester{
		TotalChunksMatched: 100,
		TotalBatches:       25,
		TotalLinesSent:     30,
		Store: Store{
			PipelineWrapperFilteredLines: 1,
			Chunk: Chunk{
				HeadChunkBytes:    5,
				HeadChunkLines:    10,
				DecompressedBytes: 12,
				DecompressedLines: 20,
				CompressedBytes:   30,
				TotalDuplicates:   1,
			},
		},
	})
}

func TestResult_Merge(t *testing.T) {
	var res Result

	res.Merge(res) // testing zero.
	require.Equal(t, res, res)

	toMerge := Result{
		Ingester: Ingester{
			TotalChunksMatched: 200,
			TotalBatches:       50,
			TotalLinesSent:     60,
			TotalReached:       2,
			Store: Store{
				PipelineWrapperFilteredLines: 4,
				Chunk: Chunk{
					HeadChunkBytes:    10,
					HeadChunkLines:    20,
					DecompressedBytes: 24,
					DecompressedLines: 40,
					CompressedBytes:   60,
					TotalDuplicates:   2,
				},
			},
		},
		Querier: Querier{
			Store: Store{
				TotalChunksRef:               50,
				TotalChunksDownloaded:        60,
				ChunksDownloadTime:           time.Second.Nanoseconds(),
				QueryReferencedStructured:    true,
				PipelineWrapperFilteredLines: 2,
				Chunk: Chunk{
					HeadChunkBytes:    10,
					HeadChunkLines:    20,
					DecompressedBytes: 40,
					DecompressedLines: 20,
					CompressedBytes:   30,
					TotalDuplicates:   10,
				},
			},
		},
		Caches: Caches{
			Chunk: Cache{
				Requests:      5,
				BytesReceived: 1024,
				BytesSent:     512,
			},
			Index: Cache{
				EntriesRequested: 22,
				EntriesFound:     2,
			},
			Result: Cache{
				EntriesStored:     3,
				QueryLengthServed: int64(3 * time.Hour),
			},
		},
		Summary: Summary{
			ExecTime:                2 * time.Second.Seconds(),
			QueueTime:               2 * time.Nanosecond.Seconds(),
			BytesProcessedPerSecond: int64(42),
			LinesProcessedPerSecond: int64(50),
			TotalBytesProcessed:     int64(84),
			TotalLinesProcessed:     int64(100),
		},
	}

	res.Merge(toMerge)
	require.Equal(t, toMerge, res)

	// merge again
	res.Merge(toMerge)
	require.Equal(t, Result{
		Ingester: Ingester{
			TotalChunksMatched: 2 * 200,
			TotalBatches:       2 * 50,
			TotalLinesSent:     2 * 60,
			Store: Store{
				PipelineWrapperFilteredLines: 8,
				Chunk: Chunk{
					HeadChunkBytes:    2 * 10,
					HeadChunkLines:    2 * 20,
					DecompressedBytes: 2 * 24,
					DecompressedLines: 2 * 40,
					CompressedBytes:   2 * 60,
					TotalDuplicates:   2 * 2,
				},
			},
			TotalReached: 2 * 2,
		},
		Querier: Querier{
			Store: Store{
				TotalChunksRef:               2 * 50,
				TotalChunksDownloaded:        2 * 60,
				ChunksDownloadTime:           2 * time.Second.Nanoseconds(),
				QueryReferencedStructured:    true,
				PipelineWrapperFilteredLines: 4,
				Chunk: Chunk{
					HeadChunkBytes:    2 * 10,
					HeadChunkLines:    2 * 20,
					DecompressedBytes: 2 * 40,
					DecompressedLines: 2 * 20,
					CompressedBytes:   2 * 30,
					TotalDuplicates:   2 * 10,
				},
			},
		},
		Caches: Caches{
			Chunk: Cache{
				Requests:      2 * 5,
				BytesReceived: 2 * 1024,
				BytesSent:     2 * 512,
			},
			Index: Cache{
				EntriesRequested: 2 * 22,
				EntriesFound:     2 * 2,
			},
			Result: Cache{
				EntriesStored:     2 * 3,
				QueryLengthServed: int64(2 * 3 * time.Hour),
			},
		},
		Summary: Summary{
			ExecTime:                2 * 2 * time.Second.Seconds(),
			QueueTime:               2 * 2 * time.Nanosecond.Seconds(),
			BytesProcessedPerSecond: int64(42), // 2 requests at the same pace should give the same bytes/lines per sec
			LinesProcessedPerSecond: int64(50),
			TotalBytesProcessed:     2 * int64(84),
			TotalLinesProcessed:     2 * int64(100),
		},
	}, res)
}

func TestReset(t *testing.T) {
	statsCtx, ctx := NewContext(context.Background())
	fakeIngesterQuery(ctx)
	res := statsCtx.Result(2*time.Second, 2*time.Millisecond, 10)
	require.NotEmpty(t, res)
	statsCtx.Reset()
	res = statsCtx.Result(0, 0, 0)
	res.Summary.Subqueries = 0
	require.Empty(t, res)
}

func TestIngester(t *testing.T) {
	statsCtx, ctx := NewContext(context.Background())
	fakeIngesterQuery(ctx)
	statsCtx.AddCompressedBytes(100)
	statsCtx.AddDuplicates(10)
	statsCtx.AddHeadChunkBytes(200)
	statsCtx.SetQueryReferencedStructuredMetadata()
	statsCtx.AddPipelineWrapperFilterdLines(1)
	require.Equal(t, Ingester{
		TotalReached:       1,
		TotalChunksMatched: 100,
		TotalBatches:       25,
		TotalLinesSent:     30,
		Store: Store{
			QueryReferencedStructured:    true,
			PipelineWrapperFilteredLines: 1,
			Chunk: Chunk{
				HeadChunkBytes:  200,
				CompressedBytes: 100,
				TotalDuplicates: 10,
			},
		},
	}, statsCtx.Ingester())
}

func TestCaches(t *testing.T) {
	statsCtx, _ := NewContext(context.Background())

	statsCtx.AddCacheRequest(ChunkCache, 5)
	statsCtx.AddCacheEntriesStored(ResultCache, 3)
	statsCtx.AddCacheQueryLengthServed(ResultCache, 3*time.Hour)
	statsCtx.AddCacheEntriesRequested(IndexCache, 22)
	statsCtx.AddCacheBytesRetrieved(ChunkCache, 1024)
	statsCtx.AddCacheBytesSent(ChunkCache, 512)
	statsCtx.AddCacheEntriesFound(IndexCache, 2)

	require.Equal(t, Caches{
		Chunk: Cache{
			Requests:      5,
			BytesReceived: 1024,
			BytesSent:     512,
		},
		Index: Cache{
			EntriesRequested: 22,
			EntriesFound:     2,
		},
		Result: Cache{
			EntriesStored:     3,
			QueryLengthServed: int64(time.Hour * 3),
		},
	}, statsCtx.Caches())
}
