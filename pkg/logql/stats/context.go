/*
Package stats provides primitives for recording metrics across the query path.
Statistics are passed through the query context.
To start a new query statistics context use:

	ctx := stats.NewContext(ctx)

Then you can update statistics by mutating data by using:

	stats.GetChunkData(ctx)
	stats.GetIngesterData(ctx)
	stats.GetStoreData

Finally to get a snapshot of the current query statistic use

	stats.Snapshot(ctx, time.Since(start))

Ingester statistics are sent across the GRPC stream using Trailers
see https://github.com/grpc/grpc-go/blob/master/Documentation/grpc-metadata.md
*/
package stats

import (
	"context"
	"errors"
	fmt "fmt"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/kit/log"
)

type ctxKeyType string

const (
	trailersKey ctxKeyType = "trailers"
	chunksKey   ctxKeyType = "chunks"
	ingesterKey ctxKeyType = "ingester"
	storeKey    ctxKeyType = "store"
	resultKey   ctxKeyType = "result" // key for pre-computed results to be merged in  `Snapshot`
	lockKey     ctxKeyType = "lock"   // key for locking a context when stats is used concurrently
)

// Log logs a query statistics result.
func (r Result) Log(log log.Logger) {
	_ = log.Log(
		"Ingester.TotalReached", r.Ingester.TotalReached,
		"Ingester.TotalChunksMatched", r.Ingester.TotalChunksMatched,
		"Ingester.TotalBatches", r.Ingester.TotalBatches,
		"Ingester.TotalLinesSent", r.Ingester.TotalLinesSent,

		"Ingester.HeadChunkBytes", humanize.Bytes(uint64(r.Ingester.HeadChunkBytes)),
		"Ingester.HeadChunkLines", r.Ingester.HeadChunkLines,
		"Ingester.DecompressedBytes", humanize.Bytes(uint64(r.Ingester.DecompressedBytes)),
		"Ingester.DecompressedLines", r.Ingester.DecompressedLines,
		"Ingester.CompressedBytes", humanize.Bytes(uint64(r.Ingester.CompressedBytes)),
		"Ingester.TotalDuplicates", r.Ingester.TotalDuplicates,

		"Store.TotalChunksRef", r.Store.TotalChunksRef,
		"Store.TotalChunksDownloaded", r.Store.TotalChunksDownloaded,
		"Store.ChunksDownloadTime", time.Duration(int64(r.Store.ChunksDownloadTime*float64(time.Second))),

		"Store.HeadChunkBytes", humanize.Bytes(uint64(r.Store.HeadChunkBytes)),
		"Store.HeadChunkLines", r.Store.HeadChunkLines,
		"Store.DecompressedBytes", humanize.Bytes(uint64(r.Store.DecompressedBytes)),
		"Store.DecompressedLines", r.Store.DecompressedLines,
		"Store.CompressedBytes", humanize.Bytes(uint64(r.Store.CompressedBytes)),
		"Store.TotalDuplicates", r.Store.TotalDuplicates,
	)
	r.Summary.Log(log)
}

func (s Summary) Log(log log.Logger) {
	_ = log.Log(
		"Summary.BytesProcessedPerSecond", humanize.Bytes(uint64(s.BytesProcessedPerSecond)),
		"Summary.LinesProcessedPerSecond", s.LinesProcessedPerSecond,
		"Summary.TotalBytesProcessed", humanize.Bytes(uint64(s.TotalBytesProcessed)),
		"Summary.TotalLinesProcessed", s.TotalLinesProcessed,
		"Summary.ExecTime", time.Duration(int64(s.ExecTime*float64(time.Second))),
	)
}

// NewContext creates a new statistics context
func NewContext(ctx context.Context) context.Context {
	ctx = injectTrailerCollector(ctx)
	ctx = context.WithValue(ctx, storeKey, &StoreData{})
	ctx = context.WithValue(ctx, chunksKey, &ChunkData{})
	ctx = context.WithValue(ctx, ingesterKey, &IngesterData{})
	ctx = context.WithValue(ctx, resultKey, &Result{})
	ctx = context.WithValue(ctx, lockKey, &sync.Mutex{})
	return ctx
}

// ChunkData contains chunks specific statistics.
type ChunkData struct {
	HeadChunkBytes    int64 `json:"headChunkBytes"`    // Total bytes processed but was already in memory. (found in the headchunk)
	HeadChunkLines    int64 `json:"headChunkLines"`    // Total lines processed but was already in memory. (found in the headchunk)
	DecompressedBytes int64 `json:"decompressedBytes"` // Total bytes decompressed and processed from chunks.
	DecompressedLines int64 `json:"decompressedLines"` // Total lines decompressed and processed from chunks.
	CompressedBytes   int64 `json:"compressedBytes"`   // Total bytes of compressed chunks (blocks) processed.
	TotalDuplicates   int64 `json:"totalDuplicates"`   // Total duplicates found while processing.
}

// GetChunkData returns the chunks statistics data from the current context.
func GetChunkData(ctx context.Context) *ChunkData {
	res, ok := ctx.Value(chunksKey).(*ChunkData)
	if !ok {
		return &ChunkData{}
	}
	return res
}

// IngesterData contains ingester specific statistics.
type IngesterData struct {
	TotalChunksMatched int64 `json:"totalChunksMatched"` // Total of chunks matched by the query from ingesters
	TotalBatches       int64 `json:"totalBatches"`       // Total of batches sent from ingesters.
	TotalLinesSent     int64 `json:"totalLinesSent"`     // Total lines sent by ingesters.
}

// GetIngesterData returns the ingester statistics data from the current context.
func GetIngesterData(ctx context.Context) *IngesterData {
	res, ok := ctx.Value(ingesterKey).(*IngesterData)
	if !ok {
		return &IngesterData{}
	}
	return res
}

// StoreData contains store specific statistics.
type StoreData struct {
	TotalChunksRef        int64         // The total of chunk reference fetched from index.
	TotalChunksDownloaded int64         // Total number of chunks fetched.
	ChunksDownloadTime    time.Duration // Time spent fetching chunks.
}

// GetStoreData returns the store statistics data from the current context.
func GetStoreData(ctx context.Context) *StoreData {
	res, ok := ctx.Value(storeKey).(*StoreData)
	if !ok {
		return &StoreData{}
	}
	return res
}

// Snapshot compute query statistics from a context using the total exec time.
func Snapshot(ctx context.Context, execTime time.Duration) Result {
	// ingester data is decoded from grpc trailers.
	res := decodeTrailers(ctx)
	// collect data from store.
	s, ok := ctx.Value(storeKey).(*StoreData)
	if ok {
		res.Store.TotalChunksRef = s.TotalChunksRef
		res.Store.TotalChunksDownloaded = s.TotalChunksDownloaded
		res.Store.ChunksDownloadTime = s.ChunksDownloadTime.Seconds()
	}
	// collect data from chunks iteration.
	c, ok := ctx.Value(chunksKey).(*ChunkData)
	if ok {
		res.Store.HeadChunkBytes = c.HeadChunkBytes
		res.Store.HeadChunkLines = c.HeadChunkLines
		res.Store.DecompressedBytes = c.DecompressedBytes
		res.Store.DecompressedLines = c.DecompressedLines
		res.Store.CompressedBytes = c.CompressedBytes
		res.Store.TotalDuplicates = c.TotalDuplicates
	}

	existing, err := GetResult(ctx)
	if err != nil {
		res.ComputeSummary(execTime)
		return res
	}

	existing.Merge(res)
	existing.ComputeSummary(execTime)
	return *existing

}

// ComputeSummary calculates the summary based on store and ingester data.
func (r *Result) ComputeSummary(execTime time.Duration) {
	// calculate the summary
	r.Summary.TotalBytesProcessed = r.Store.DecompressedBytes + r.Store.HeadChunkBytes +
		r.Ingester.DecompressedBytes + r.Ingester.HeadChunkBytes
	r.Summary.TotalLinesProcessed = r.Store.DecompressedLines + r.Store.HeadChunkLines +
		r.Ingester.DecompressedLines + r.Ingester.HeadChunkLines
	r.Summary.ExecTime = execTime.Seconds()
	if execTime != 0 {
		r.Summary.BytesProcessedPerSecond =
			int64(float64(r.Summary.TotalBytesProcessed) /
				execTime.Seconds())
		r.Summary.LinesProcessedPerSecond =
			int64(float64(r.Summary.TotalLinesProcessed) /
				execTime.Seconds())
	}
}

func (r *Result) Merge(m Result) {

	r.Store.TotalChunksRef += m.Store.TotalChunksRef
	r.Store.TotalChunksDownloaded += m.Store.TotalChunksDownloaded
	r.Store.ChunksDownloadTime += m.Store.ChunksDownloadTime
	r.Store.HeadChunkBytes += m.Store.HeadChunkBytes
	r.Store.HeadChunkLines += m.Store.HeadChunkLines
	r.Store.DecompressedBytes += m.Store.DecompressedBytes
	r.Store.DecompressedLines += m.Store.DecompressedLines
	r.Store.CompressedBytes += m.Store.CompressedBytes
	r.Store.TotalDuplicates += m.Store.TotalDuplicates

	r.Ingester.TotalReached += m.Ingester.TotalReached
	r.Ingester.TotalChunksMatched += m.Ingester.TotalChunksMatched
	r.Ingester.TotalBatches += m.Ingester.TotalBatches
	r.Ingester.TotalLinesSent += m.Ingester.TotalLinesSent
	r.Ingester.HeadChunkBytes += m.Ingester.HeadChunkBytes
	r.Ingester.HeadChunkLines += m.Ingester.HeadChunkLines
	r.Ingester.DecompressedBytes += m.Ingester.DecompressedBytes
	r.Ingester.DecompressedLines += m.Ingester.DecompressedLines
	r.Ingester.CompressedBytes += m.Ingester.CompressedBytes
	r.Ingester.TotalDuplicates += m.Ingester.TotalDuplicates

	r.ComputeSummary(time.Duration(int64((r.Summary.ExecTime + m.Summary.ExecTime) * float64(time.Second))))
}

// JoinResults merges a Result with the embedded Result in a context in a concurrency-safe manner.
func JoinResults(ctx context.Context, res Result) error {
	mtx, err := GetMutex(ctx)
	if err != nil {
		return err
	}
	mtx.Lock()
	defer mtx.Unlock()

	v, err := GetResult(ctx)
	if err != nil {
		return err
	}
	v.Merge(res)
	return nil
}

func GetResult(ctx context.Context) (*Result, error) {
	v, ok := ctx.Value(resultKey).(*Result)
	if !ok {
		return nil, errors.New("unpopulated Results key")
	}
	return v, nil
}

// GetChunkData returns the chunks statistics data from the current context.
func GetMutex(ctx context.Context) (*sync.Mutex, error) {
	res, ok := ctx.Value(lockKey).(*sync.Mutex)
	if !ok {
		return nil, fmt.Errorf("no mutex available under %s", string(lockKey))
	}
	return res, nil
}
