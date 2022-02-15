/*
Package stats provides primitives for recording metrics across the query path.
Statistics are passed through the query context.
To start a new query statistics context use:

	statsCtx, ctx := stats.NewContext(ctx)

Then you can update statistics by mutating data by using:

	statsCtx.Add...(1)

To get the  statistic from the current context you can use:

	statsCtx := stats.FromContext(ctx)

Finally to get a snapshot of the current query statistic use

	statsCtx.Result(time.Since(start))

*/
package stats

import (
	"context"
	"sync"
	"sync/atomic" //lint:ignore faillint we can't use go.uber.org/atomic with a protobuf struct without wrapping it.
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
)

type (
	ctxKeyType string
	Component  int64
)

const (
	statsKey ctxKeyType = "stats"
)

// Context is the statistics context. It is passed through the query path and accumulates statistics.
type Context struct {
	querier  Querier
	ingester Ingester

	// store is the store statistics collected across the query path
	store Store
	// result accumulates results for JoinResult.
	result Result

	mtx sync.Mutex
}

// NewContext creates a new statistics context
func NewContext(ctx context.Context) (*Context, context.Context) {
	contextData := &Context{}
	ctx = context.WithValue(ctx, statsKey, contextData)
	return contextData, ctx
}

// FromContext returns the statistics context.
func FromContext(ctx context.Context) *Context {
	v, ok := ctx.Value(statsKey).(*Context)
	if !ok {
		return &Context{}
	}
	return v
}

// Ingester returns the ingester statistics accumulated so far.
func (c *Context) Ingester() Ingester {
	return Ingester{
		TotalReached:       c.ingester.TotalReached,
		TotalChunksMatched: c.ingester.TotalChunksMatched,
		TotalBatches:       c.ingester.TotalBatches,
		TotalLinesSent:     c.ingester.TotalLinesSent,
		Store:              c.store,
	}
}

// Reset clears the statistics.
func (c *Context) Reset() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	c.store.Reset()
	c.querier.Reset()
	c.ingester.Reset()
	c.result.Reset()
}

// Result calculates the summary based on store and ingester data.
func (c *Context) Result(execTime time.Duration, queueTime time.Duration) Result {
	r := c.result

	r.Merge(Result{
		Querier: Querier{
			Store: c.store,
		},
		Ingester: c.ingester,
	})

	r.ComputeSummary(execTime, queueTime)

	return r
}

// JoinResults merges a Result with the embedded Result in a context in a concurrency-safe manner.
func JoinResults(ctx context.Context, res Result) {
	stats := FromContext(ctx)
	stats.mtx.Lock()
	defer stats.mtx.Unlock()

	stats.result.Merge(res)
}

// JoinIngesterResult joins the ingester result statistics in a concurrency-safe manner.
func JoinIngesters(ctx context.Context, inc Ingester) {
	stats := FromContext(ctx)
	stats.mtx.Lock()
	defer stats.mtx.Unlock()

	stats.ingester.Merge(inc)
}

// ComputeSummary compute the summary of the statistics.
func (r *Result) ComputeSummary(execTime time.Duration, queueTime time.Duration) {
	r.Summary.TotalBytesProcessed = r.Querier.Store.Chunk.DecompressedBytes + r.Querier.Store.Chunk.HeadChunkBytes +
		r.Ingester.Store.Chunk.DecompressedBytes + r.Ingester.Store.Chunk.HeadChunkBytes
	r.Summary.TotalLinesProcessed = r.Querier.Store.Chunk.DecompressedLines + r.Querier.Store.Chunk.HeadChunkLines +
		r.Ingester.Store.Chunk.DecompressedLines + r.Ingester.Store.Chunk.HeadChunkLines
	r.Summary.ExecTime = execTime.Seconds()
	if execTime != 0 {
		r.Summary.BytesProcessedPerSecond = int64(float64(r.Summary.TotalBytesProcessed) /
			execTime.Seconds())
		r.Summary.LinesProcessedPerSecond = int64(float64(r.Summary.TotalLinesProcessed) /
			execTime.Seconds())
	}
	if queueTime != 0 {
		r.Summary.QueueTime = queueTime.Seconds()
	}
}

func (s *Store) Merge(m Store) {
	s.TotalChunksRef += m.TotalChunksRef
	s.TotalChunksDownloaded += m.TotalChunksDownloaded
	s.ChunksDownloadTime += m.ChunksDownloadTime
	s.Chunk.HeadChunkBytes += m.Chunk.HeadChunkBytes
	s.Chunk.HeadChunkLines += m.Chunk.HeadChunkLines
	s.Chunk.DecompressedBytes += m.Chunk.DecompressedBytes
	s.Chunk.DecompressedLines += m.Chunk.DecompressedLines
	s.Chunk.CompressedBytes += m.Chunk.CompressedBytes
	s.Chunk.TotalDuplicates += m.Chunk.TotalDuplicates
}

func (q *Querier) Merge(m Querier) {
	q.Store.Merge(m.Store)
}

func (i *Ingester) Merge(m Ingester) {
	i.Store.Merge(m.Store)
	i.TotalBatches += m.TotalBatches
	i.TotalLinesSent += m.TotalLinesSent
	i.TotalChunksMatched += m.TotalChunksMatched
	i.TotalReached += m.TotalReached
}

// Merge merges two results of statistics.
// This will increase the total number of Subqueries.
func (r *Result) Merge(m Result) {
	r.Summary.Subqueries++
	r.Querier.Merge(m.Querier)
	r.Ingester.Merge(m.Ingester)
	r.ComputeSummary(ConvertSecondsToNanoseconds(r.Summary.ExecTime+m.Summary.ExecTime),
		ConvertSecondsToNanoseconds(r.Summary.QueueTime+m.Summary.QueueTime))
}

// ConvertSecondsToNanoseconds converts time.Duration representation of seconds (float64)
// into time.Duration representation of nanoseconds (int64)
func ConvertSecondsToNanoseconds(seconds float64) time.Duration {
	return time.Duration(int64(seconds * float64(time.Second)))
}

func (r Result) ChunksDownloadTime() time.Duration {
	return time.Duration(r.Querier.Store.ChunksDownloadTime + r.Ingester.Store.ChunksDownloadTime)
}

func (r Result) TotalDuplicates() int64 {
	return r.Querier.Store.Chunk.TotalDuplicates + r.Ingester.Store.Chunk.TotalDuplicates
}

func (r Result) TotalChunksDownloaded() int64 {
	return r.Querier.Store.TotalChunksDownloaded + r.Ingester.Store.TotalChunksDownloaded
}

func (r Result) TotalChunksRef() int64 {
	return r.Querier.Store.TotalChunksRef + r.Ingester.Store.TotalChunksRef
}

func (r Result) TotalDecompressedBytes() int64 {
	return r.Querier.Store.Chunk.DecompressedBytes + r.Ingester.Store.Chunk.DecompressedBytes
}

func (r Result) TotalDecompressedLines() int64 {
	return r.Querier.Store.Chunk.DecompressedLines + r.Ingester.Store.Chunk.DecompressedLines
}

func (c *Context) AddIngesterBatch(size int64) {
	atomic.AddInt64(&c.ingester.TotalBatches, 1)
	atomic.AddInt64(&c.ingester.TotalLinesSent, size)
}

func (c *Context) AddIngesterTotalChunkMatched(i int64) {
	atomic.AddInt64(&c.ingester.TotalChunksMatched, i)
}

func (c *Context) AddIngesterReached(i int32) {
	atomic.AddInt32(&c.ingester.TotalReached, i)
}

func (c *Context) AddHeadChunkLines(i int64) {
	atomic.AddInt64(&c.store.Chunk.HeadChunkLines, i)
}

func (c *Context) AddHeadChunkBytes(i int64) {
	atomic.AddInt64(&c.store.Chunk.HeadChunkBytes, i)
}

func (c *Context) AddCompressedBytes(i int64) {
	atomic.AddInt64(&c.store.Chunk.CompressedBytes, i)
}

func (c *Context) AddDecompressedBytes(i int64) {
	atomic.AddInt64(&c.store.Chunk.DecompressedBytes, i)
}

func (c *Context) AddDecompressedLines(i int64) {
	atomic.AddInt64(&c.store.Chunk.DecompressedLines, i)
}

func (c *Context) AddDuplicates(i int64) {
	atomic.AddInt64(&c.store.Chunk.TotalDuplicates, i)
}

func (c *Context) AddChunksDownloadTime(i time.Duration) {
	atomic.AddInt64(&c.store.ChunksDownloadTime, int64(i))
}

func (c *Context) AddChunksDownloaded(i int64) {
	atomic.AddInt64(&c.store.TotalChunksDownloaded, i)
}

func (c *Context) AddChunksRef(i int64) {
	atomic.AddInt64(&c.store.TotalChunksRef, i)
}

// Log logs a query statistics result.
func (r Result) Log(log log.Logger) {
	_ = log.Log(
		"Ingester.TotalReached", r.Ingester.TotalReached,
		"Ingester.TotalChunksMatched", r.Ingester.TotalChunksMatched,
		"Ingester.TotalBatches", r.Ingester.TotalBatches,
		"Ingester.TotalLinesSent", r.Ingester.TotalLinesSent,
		"Ingester.TotalChunksRef", r.Ingester.Store.TotalChunksRef,
		"Ingester.TotalChunksDownloaded", r.Ingester.Store.TotalChunksDownloaded,
		"Ingester.ChunksDownloadTime", time.Duration(r.Ingester.Store.ChunksDownloadTime),
		"Ingester.HeadChunkBytes", humanize.Bytes(uint64(r.Ingester.Store.Chunk.HeadChunkBytes)),
		"Ingester.HeadChunkLines", r.Ingester.Store.Chunk.HeadChunkLines,
		"Ingester.DecompressedBytes", humanize.Bytes(uint64(r.Ingester.Store.Chunk.DecompressedBytes)),
		"Ingester.DecompressedLines", r.Ingester.Store.Chunk.DecompressedLines,
		"Ingester.CompressedBytes", humanize.Bytes(uint64(r.Ingester.Store.Chunk.CompressedBytes)),
		"Ingester.TotalDuplicates", r.Ingester.Store.Chunk.TotalDuplicates,

		"Querier.TotalChunksRef", r.Querier.Store.TotalChunksRef,
		"Querier.TotalChunksDownloaded", r.Querier.Store.TotalChunksDownloaded,
		"Querier.ChunksDownloadTime", time.Duration(r.Querier.Store.ChunksDownloadTime),
		"Querier.HeadChunkBytes", humanize.Bytes(uint64(r.Querier.Store.Chunk.HeadChunkBytes)),
		"Querier.HeadChunkLines", r.Querier.Store.Chunk.HeadChunkLines,
		"Querier.DecompressedBytes", humanize.Bytes(uint64(r.Querier.Store.Chunk.DecompressedBytes)),
		"Querier.DecompressedLines", r.Querier.Store.Chunk.DecompressedLines,
		"Querier.CompressedBytes", humanize.Bytes(uint64(r.Querier.Store.Chunk.CompressedBytes)),
		"Querier.TotalDuplicates", r.Querier.Store.Chunk.TotalDuplicates,
	)
	r.Summary.Log(log)
}

func (s Summary) Log(log log.Logger) {
	_ = log.Log(
		"Summary.BytesProcessedPerSecond", humanize.Bytes(uint64(s.BytesProcessedPerSecond)),
		"Summary.LinesProcessedPerSecond", s.LinesProcessedPerSecond,
		"Summary.TotalBytesProcessed", humanize.Bytes(uint64(s.TotalBytesProcessed)),
		"Summary.TotalLinesProcessed", s.TotalLinesProcessed,
		"Summary.ExecTime", ConvertSecondsToNanoseconds(s.ExecTime),
		"Summary.QueueTime", ConvertSecondsToNanoseconds(s.QueueTime),
	)
}
