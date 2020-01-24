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

	stats.Snapshot(ctx,time.Since(start))

Ingester statistics are sent across the GRPC stream using Trailers
see https://github.com/grpc/grpc-go/blob/master/Documentation/grpc-metadata.md
*/
package stats

import (
	"context"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

type ctxKeyType string

const (
	trailersKey ctxKeyType = "trailers"
	chunksKey   ctxKeyType = "chunks"
	ingesterKey ctxKeyType = "ingester"
	storeKey    ctxKeyType = "store"
)

// Result contains LogQL query statistics.
type Result struct {
	Ingester Ingester
	Store    Store
	Summary  Summary
}

// Log logs a query statistics result.
func Log(log log.Logger, r Result) {
	level.Debug(log).Log(
		"Ingester.TotalReached", r.Ingester.TotalReached,
		"Ingester.TotalChunksMatched", r.Ingester.TotalChunksMatched,
		"Ingester.TotalBatches", r.Ingester.TotalBatches,
		"Ingester.TotalLinesSent", r.Ingester.TotalLinesSent,

		"Ingester.BytesUncompressed", humanize.Bytes(uint64(r.Ingester.BytesUncompressed)),
		"Ingester.LinesUncompressed", r.Ingester.LinesUncompressed,
		"Ingester.BytesDecompressed", humanize.Bytes(uint64(r.Ingester.BytesDecompressed)),
		"Ingester.LinesDecompressed", r.Ingester.LinesDecompressed,
		"Ingester.BytesCompressed", humanize.Bytes(uint64(r.Ingester.BytesCompressed)),
		"Ingester.TotalDuplicates", r.Ingester.TotalDuplicates,

		"Store.TotalChunksRef", r.Store.TotalChunksRef,
		"Store.TotalDownloadedChunks", r.Store.TotalDownloadedChunks,
		"Store.TimeDownloadingChunks", r.Store.TimeDownloadingChunks,

		"Store.BytesUncompressed", humanize.Bytes(uint64(r.Store.BytesUncompressed)),
		"Store.LinesUncompressed", r.Store.LinesUncompressed,
		"Store.BytesDecompressed", humanize.Bytes(uint64(r.Store.BytesDecompressed)),
		"Store.LinesDecompressed", r.Store.LinesDecompressed,
		"Store.BytesCompressed", humanize.Bytes(uint64(r.Store.BytesCompressed)),
		"Store.TotalDuplicates", r.Store.TotalDuplicates,

		"Summary.BytesProcessedPerSeconds", humanize.Bytes(uint64(r.Summary.BytesProcessedPerSeconds)),
		"Summary.LinesProcessedPerSeconds", r.Summary.LinesProcessedPerSeconds,
		"Summary.TotalBytesProcessed", humanize.Bytes(uint64(r.Summary.TotalBytesProcessed)),
		"Summary.TotalLinesProcessed", r.Summary.TotalLinesProcessed,
		"Summary.ExecTime", r.Summary.ExecTime,
	)
}

// Summary is the summary of a query statistics.
type Summary struct {
	BytesProcessedPerSeconds int64
	LinesProcessedPerSeconds int64
	TotalBytesProcessed      int64
	TotalLinesProcessed      int64
	ExecTime                 time.Duration
}

// Ingester is the statistics result for ingesters queries.
type Ingester struct {
	IngesterData
	ChunkData
	TotalReached int
}

// Store is the statistics result of the store.
type Store struct {
	StoreData
	ChunkData
}

// NewContext creates a new statistics context
func NewContext(ctx context.Context) context.Context {
	ctx = injectTrailerCollector(ctx)
	ctx = context.WithValue(ctx, storeKey, &StoreData{})
	ctx = context.WithValue(ctx, chunksKey, &ChunkData{})
	ctx = context.WithValue(ctx, ingesterKey, &IngesterData{})
	return ctx
}

// ChunkData contains chunks specific statistics.
type ChunkData struct {
	BytesUncompressed int64
	LinesUncompressed int64
	BytesDecompressed int64
	LinesDecompressed int64
	BytesCompressed   int64
	TotalDuplicates   int64
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
	TotalChunksMatched int64
	TotalBatches       int64
	TotalLinesSent     int64
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
	TotalDownloadedChunks int64         // Total number of chunks fetched.
	TimeDownloadingChunks time.Duration // Time spent fetching chunks.
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
	var res Result
	// ingester data is decoded from grpc trailers.
	res.Ingester = decodeTrailers(ctx)
	// collect data from store.
	s, ok := ctx.Value(storeKey).(*StoreData)
	if ok {
		res.Store.StoreData = *s
	}
	// collect data from chunks iteration.
	c, ok := ctx.Value(chunksKey).(*ChunkData)
	if ok {
		res.Store.ChunkData = *c
	}

	// calculate the summary
	res.Summary.TotalBytesProcessed = res.Store.BytesDecompressed + res.Store.BytesUncompressed +
		res.Ingester.BytesDecompressed + res.Ingester.BytesUncompressed
	res.Summary.BytesProcessedPerSeconds =
		int64(float64(res.Summary.TotalBytesProcessed) /
			execTime.Seconds())
	res.Summary.TotalLinesProcessed = res.Store.LinesDecompressed + res.Store.LinesUncompressed +
		res.Ingester.LinesDecompressed + res.Ingester.LinesUncompressed
	res.Summary.LinesProcessedPerSeconds =
		int64(float64(res.Summary.TotalLinesProcessed) /
			execTime.Seconds())
	res.Summary.ExecTime = execTime
	return res
}
