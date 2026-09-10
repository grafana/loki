package queryrange

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

type fanoutContextKey struct{}
type fanoutResultOwnerKey struct{}

type fanoutRecorder struct {
	mu sync.Mutex

	parent trace.Span
	start  time.Time

	activated bool
	finished  bool

	plannedRequests      int64
	observedRequests     int64
	splits               int64
	shards               int64
	chunkRefs            int64
	chunkDownloads       int64
	chunkFetchFailures   int64
	cacheRequests        int64
	cacheHits            int64
	cacheMisses          int64
	cacheBytes           int64
	cacheDuration        int64
	requestErrors        int64
	totalRequestDuration int64
	maxRequestDuration   int64
}

func startFanout(ctx context.Context, count int) (context.Context, *fanoutRecorder, bool) {
	if recorder, ok := ctx.Value(fanoutContextKey{}).(*fanoutRecorder); ok {
		if recorder.reserve(count) {
			return suppressedFanoutContext(ctx), recorder, false
		}
		return ctx, recorder, false
	}

	parent := trace.SpanFromContext(ctx)
	spanContext := parent.SpanContext()
	if !spanContext.IsValid() {
		return ctx, nil, false
	}

	recorder := &fanoutRecorder{parent: parent, start: time.Now()}
	ctx = context.WithValue(ctx, fanoutContextKey{}, recorder)
	if recorder.reserve(count) {
		return suppressedFanoutContext(ctx), recorder, true
	}
	return ctx, recorder, true
}

func suppressedFanoutContext(ctx context.Context) context.Context {
	// Loki's production sampler is parent-based, so clearing this flag suppresses descendants.
	spanContext := trace.SpanFromContext(ctx).SpanContext()
	spanContext = trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    spanContext.TraceID(),
		SpanID:     spanContext.SpanID(),
		TraceState: spanContext.TraceState(),
		TraceFlags: spanContext.TraceFlags() &^ trace.FlagsSampled,
		Remote:     spanContext.IsRemote(),
	})
	return trace.ContextWithSpanContext(ctx, spanContext)
}

func (r *fanoutRecorder) reserve(count int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return r.activated
	}
	r.plannedRequests += int64(count)
	if !r.activated && r.plannedRequests > DefaultDownstreamConcurrency {
		r.activated = true
	}
	return r.activated
}

func (r *fanoutRecorder) addSplits(splits int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.finished {
		r.mu.Unlock()
		return
	}
	r.splits += splits
	r.mu.Unlock()
}

func (r *fanoutRecorder) addShards(count int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.finished {
		r.mu.Unlock()
		return
	}
	r.shards += int64(count)
	r.mu.Unlock()
}

func (r *fanoutRecorder) addRequest(duration time.Duration, err error, result *stats.Result) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}

	r.observedRequests++
	d := int64(duration)
	r.totalRequestDuration += d
	if d > r.maxRequestDuration {
		r.maxRequestDuration = d
	}
	if err != nil {
		r.requestErrors++
	}
	if result == nil {
		return
	}

	r.chunkRefs += result.TotalChunksRef()
	r.chunkDownloads += result.TotalChunksDownloaded()
	r.chunkFetchFailures += result.TotalChunkFetchFailures()
	for _, cache := range []stats.Cache{
		result.Caches.Chunk,
		result.Caches.Index,
		result.Caches.Result,
		result.Caches.StatsResult,
		result.Caches.VolumeResult,
		result.Caches.SeriesResult,
		result.Caches.LabelResult,
		result.Caches.InstantMetricResult,
		result.Caches.LogResult,
		result.Caches.TaskResult,
	} {
		r.cacheRequests += int64(cache.Requests)
		r.cacheHits += int64(cache.EntriesFound)
		misses := int64(cache.EntriesRequested) - int64(cache.EntriesFound)
		if misses < 0 {
			misses = 0
		}
		r.cacheMisses += misses
		r.cacheBytes += cache.BytesReceived + cache.BytesSent
		r.cacheDuration += cache.DownloadTime
	}
}

func (r *fanoutRecorder) finish() {
	if r == nil {
		return
	}

	r.mu.Lock()
	if r.finished {
		r.mu.Unlock()
		return
	}
	r.finished = true
	if !r.activated {
		r.mu.Unlock()
		return
	}
	attrs := []attribute.KeyValue{
		attribute.Bool("fanout.aggregate_only", true),
		attribute.Int("fanout.threshold", DefaultDownstreamConcurrency),
		attribute.Int64("fanout.planned_downstream_requests", r.plannedRequests),
		attribute.Int64("fanout.observed_downstream_requests", r.observedRequests),
		attribute.Int64("fanout.split_count", r.splits),
		attribute.Int64("fanout.shard_count", r.shards),
		attribute.Int64("fanout.chunk_refs", r.chunkRefs),
		attribute.Int64("fanout.chunk_downloads", r.chunkDownloads),
		attribute.Int64("fanout.chunk_fetch_failures", r.chunkFetchFailures),
		attribute.Int64("fanout.cache_requests", r.cacheRequests),
		attribute.Int64("fanout.cache_hits", r.cacheHits),
		attribute.Int64("fanout.cache_misses", r.cacheMisses),
		attribute.Int64("fanout.cache_bytes", r.cacheBytes),
		attribute.Int64("fanout.cache_duration_ns", r.cacheDuration),
		attribute.Int64("fanout.request_errors", r.requestErrors),
		attribute.Int64("fanout.total_subrequest_duration_ns", r.totalRequestDuration),
		attribute.Int64("fanout.max_subrequest_duration_ns", r.maxRequestDuration),
		attribute.Int64("fanout.duration_ns", time.Since(r.start).Nanoseconds()),
	}
	parent := r.parent
	r.mu.Unlock()
	parent.SetAttributes(attrs...)
}
