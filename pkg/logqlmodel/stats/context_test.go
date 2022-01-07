package stats

import (
	"context"
	"testing"
	"time"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/stretchr/testify/require"
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

	fakeIngesterQuery(ctx)
	fakeIngesterQuery(ctx)

	res := stats.Result(2*time.Second, 2*time.Nanosecond)
	res.Log(util_log.Logger)
	expected := Result{
		Ingester: Ingester{
			TotalChunksMatched: 200,
			TotalBatches:       50,
			TotalLinesSent:     60,
			TotalReached:       2,
			Store: Store{
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
				TotalChunksRef:        50,
				TotalChunksDownloaded: 60,
				ChunksDownloadTime:    time.Second.Nanoseconds(),
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
				TotalChunksRef:        50,
				TotalChunksDownloaded: 60,
				ChunksDownloadTime:    time.Second.Nanoseconds(),
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
		},
	}

	JoinResults(ctx, expected)
	res := statsCtx.Result(2*time.Second, 2*time.Nanosecond)
	require.Equal(t, expected, res)
}

func fakeIngesterQuery(ctx context.Context) {
	FromContext(ctx).AddIngesterReached(1)
	JoinIngesters(ctx, Ingester{
		TotalChunksMatched: 100,
		TotalBatches:       25,
		TotalLinesSent:     30,
		Store: Store{
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
				TotalChunksRef:        50,
				TotalChunksDownloaded: 60,
				ChunksDownloadTime:    time.Second.Nanoseconds(),
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
				TotalChunksRef:        2 * 50,
				TotalChunksDownloaded: 2 * 60,
				ChunksDownloadTime:    2 * time.Second.Nanoseconds(),
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
	res := statsCtx.Result(2*time.Second, 2*time.Millisecond)
	require.NotEmpty(t, res)
	statsCtx.Reset()
	res = statsCtx.Result(0, 0)
	require.Empty(t, res)
}

func TestIngester(t *testing.T) {
	statsCtx, ctx := NewContext(context.Background())
	fakeIngesterQuery(ctx)
	statsCtx.AddCompressedBytes(100)
	statsCtx.AddDuplicates(10)
	statsCtx.AddHeadChunkBytes(200)
	require.Equal(t, Ingester{
		TotalReached:       1,
		TotalChunksMatched: 100,
		TotalBatches:       25,
		TotalLinesSent:     30,
		Store: Store{
			Chunk: Chunk{
				HeadChunkBytes:  200,
				CompressedBytes: 100,
				TotalDuplicates: 10,
			},
		},
	}, statsCtx.Ingester())
}
