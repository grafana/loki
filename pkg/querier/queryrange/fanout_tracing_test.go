package queryrange

import (
	"bytes"
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func TestFanoutTracingSuppressesChildSpans(t *testing.T) {
	for _, count := range []int{DefaultDownstreamConcurrency + 1, DefaultDownstreamConcurrency * 4} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			tp := trace.NewTracerProvider(
				trace.WithSampler(trace.ParentBased(trace.AlwaysSample())),
				trace.WithSpanProcessor(recorder),
			)
			oldTracer := tracer
			tracer = tp.Tracer("test")
			t.Cleanup(func() {
				tracer = oldTracer
				require.NoError(t, tp.Shutdown(context.Background()))
			})

			ctx, root := tracer.Start(context.Background(), "root")
			suppressed, fanout, owner := startFanout(ctx, count)
			require.True(t, owner)
			require.NotNil(t, fanout)
			require.Equal(t, root.SpanContext().TraceID(), oteltrace.SpanFromContext(suppressed).SpanContext().TraceID())
			require.False(t, oteltrace.SpanFromContext(suppressed).SpanContext().IsSampled())
			require.Equal(t, root.SpanContext().SpanID(), oteltrace.SpanFromContext(suppressed).SpanContext().SpanID())

			fanout.addSplits(int64(count))
			fanout.addRequest(time.Millisecond, nil, &stats.Result{})
			fanout.finish()
			root.End()

			require.Len(t, recorder.Ended(), 1)
			attrs := recorder.Ended()[0].Attributes()
			require.Equal(t, true, attributeValue(attrs, "fanout.aggregate_only"))
			require.Equal(t, int64(count), attributeValue(attrs, "fanout.planned_downstream_requests"))
		})
	}
}

type fanoutTestSplitter struct{ requests []queryrangebase.Request }

type fanoutCompletionHandler struct {
	next queryrangebase.Handler
	done func(queryrangebase.Request)
}

func (h fanoutCompletionHandler) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	resp, err := h.next.Do(ctx, req)
	h.done(req)
	return resp, err
}

func (s fanoutTestSplitter) split(time.Time, []string, queryrangebase.Request, time.Duration) []queryrangebase.Request {
	return s.requests
}

func TestFanoutTracingThreshold(t *testing.T) {
	for _, tc := range []struct {
		name       string
		count      int
		suppressed bool
	}{
		{name: "below", count: DefaultDownstreamConcurrency, suppressed: false},
		{name: "above", count: DefaultDownstreamConcurrency + 1, suppressed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			tp := trace.NewTracerProvider(trace.WithSampler(trace.ParentBased(trace.AlwaysSample())), trace.WithSpanProcessor(recorder))
			oldTracer := tracer
			tracer = tp.Tracer("test")
			t.Cleanup(func() { tracer = oldTracer; require.NoError(t, tp.Shutdown(context.Background())) })

			ctx, root := tracer.Start(context.Background(), "root")
			childCtx, child := tracer.Start(startFanoutContext(ctx, tc.count), "child")
			child.End()
			root.End()
			ended := recorder.Ended()
			if tc.suppressed {
				require.Len(t, ended, 1)
				require.Equal(t, true, attributeValue(ended[0].Attributes(), "fanout.aggregate_only"))
			} else {
				require.Len(t, ended, 2)
				require.Nil(t, attributeValue(ended[1].Attributes(), "fanout.aggregate_only"))
			}
			require.Equal(t, tc.suppressed, !oteltrace.SpanFromContext(childCtx).SpanContext().IsSampled())
		})
	}
}

func TestFanoutTracingDormantRecorderFinalization(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSampler(trace.ParentBased(trace.AlwaysSample())), trace.WithSpanProcessor(recorder))
	oldTracer := tracer
	tracer = tp.Tracer("test")
	t.Cleanup(func() { tracer = oldTracer; require.NoError(t, tp.Shutdown(context.Background())) })

	ctx, root := tracer.Start(context.Background(), "root")
	_, fanout, owner := startFanout(ctx, DefaultDownstreamConcurrency)
	require.True(t, owner)
	require.False(t, fanout.activated)
	fanout.finish()
	require.True(t, fanout.finished)
	require.False(t, fanout.reserve(DefaultDownstreamConcurrency+1))
	fanout.addSplits(DefaultDownstreamConcurrency + 1)
	fanout.addShards(DefaultDownstreamConcurrency + 1)
	fanout.addRequest(time.Millisecond, context.Canceled, nil)
	fanout.finish()
	root.End()

	require.Len(t, recorder.Ended(), 1)
	require.Nil(t, attributeValue(recorder.Ended()[0].Attributes(), "fanout.aggregate_only"))
}

func startFanoutContext(ctx context.Context, count int) context.Context {
	ctx, recorder, _ := startFanout(ctx, count)
	if recorder != nil {
		defer recorder.finish()
	}
	return ctx
}

func TestComposedFanoutTracingBudget(t *testing.T) {
	for _, counts := range []struct{ splits, downstream int }{{16, 16}, {32, 32}} {
		t.Run(strconv.Itoa(counts.splits*counts.downstream), func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			tp := trace.NewTracerProvider(trace.WithSampler(trace.ParentBased(trace.AlwaysSample())), trace.WithSpanProcessor(recorder))
			oldTracer := tracer
			tracer = tp.Tracer("test")
			t.Cleanup(func() { tracer = oldTracer; require.NoError(t, tp.Shutdown(context.Background())) })

			params, err := logql.NewLiteralParams(`{app="test"}`, time.Now(), time.Now(), 0, 0, logproto.BACKWARD, 0, nil, nil)
			require.NoError(t, err)
			downstreamQueries := make([]logql.DownstreamQuery, counts.downstream)
			for i := range downstreamQueries {
				downstreamQueries[i] = logql.DownstreamQuery{Params: params}
			}
			downstreamHandler := queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				_, child := tracer.Start(ctx, "handler-child")
				child.AddEvent("handler-event")
				child.End()
				return &LokiResponse{}, nil
			})
			instance := (&DownstreamHandler{next: downstreamHandler}).Downstreamer(context.Background()).(*instance)
			handler := queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				_, err := instance.Downstream(ctx, downstreamQueries, logql.NewBufferedAccumulator(len(downstreamQueries)))
				return &LokiResponse{}, err
			})
			request := &LokiRequest{StartTs: time.Now().Add(-time.Hour), EndTs: time.Now()}
			intervals := make([]queryrangebase.Request, counts.splits)
			for i := range intervals {
				intervals[i] = request
			}
			wrapped := SplitByIntervalMiddleware(nil, fakeLimits{splitDuration: map[string]time.Duration{"test": time.Minute}}, DefaultCodec, fanoutTestSplitter{requests: intervals}, nil).Wrap(handler)
			ctx := user.InjectOrgID(context.Background(), "test")
			ctx, root := tracer.Start(ctx, "root")
			_, err = wrapped.Do(ctx, request)
			require.NoError(t, err)
			root.End()

			ended := recorder.Ended()
			require.LessOrEqual(t, len(ended), 2*DefaultDownstreamConcurrency+1)
			totalEvents := 0
			for _, span := range ended {
				totalEvents += len(span.Events())
			}
			require.LessOrEqual(t, totalEvents, 2*DefaultDownstreamConcurrency)
			rootSpan := ended[len(ended)-1]
			require.Equal(t, true, attributeValue(rootSpan.Attributes(), "fanout.aggregate_only"))
			require.Equal(t, int64(counts.splits+counts.splits*counts.downstream), attributeValue(rootSpan.Attributes(), "fanout.planned_downstream_requests"))
		})
	}
}

func TestSplitFanoutTracingEarlyLimitIgnoresCanceledSibling(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSampler(trace.ParentBased(trace.AlwaysSample())), trace.WithSpanProcessor(recorder))
	testTracer := tp.Tracer("test")
	t.Cleanup(func() { require.NoError(t, tp.Shutdown(context.Background())) })

	secondStarted := make(chan struct{})
	var secondOnce sync.Once
	siblingDone := make(chan struct{})
	var siblingDoneOnce sync.Once
	firstStart := time.Now().Add(-time.Hour)
	handler := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		if req.GetStart().Equal(firstStart) {
			<-secondStarted
			return &LokiResponse{
				Data:       LokiData{Result: []logproto.Stream{{Entries: []logproto.Entry{{Line: "accepted"}}}}},
				Statistics: stats.Result{Querier: stats.Querier{Store: stats.Store{TotalChunksRef: 7}}},
			}, nil
		}
		secondOnce.Do(func() { close(secondStarted) })
		<-ctx.Done()
		return nil, ctx.Err()
	})
	request := &LokiRequest{StartTs: firstStart, EndTs: time.Now(), Limit: 1}
	intervals := make([]queryrangebase.Request, DefaultDownstreamConcurrency+1)
	for i := range intervals {
		intervals[i] = &LokiRequest{StartTs: firstStart.Add(time.Duration(i)), EndTs: request.EndTs, Limit: 1}
	}
	completedHandler := fanoutCompletionHandler{next: handler, done: func(req queryrangebase.Request) {
		if !req.GetStart().Equal(firstStart) {
			siblingDoneOnce.Do(func() { close(siblingDone) })
		}
	}}
	wrapped := SplitByIntervalMiddleware(testSchemas, fakeLimits{
		maxQueryParallelism: 2,
		splitDuration:       map[string]time.Duration{"test": time.Minute},
	}, DefaultCodec, fanoutTestSplitter{requests: intervals}, nil).Wrap(completedHandler)
	ctx := user.InjectOrgID(context.Background(), "test")
	ctx, root := testTracer.Start(ctx, "root")
	_, err := wrapped.Do(ctx, request)
	require.NoError(t, err)
	<-siblingDone
	root.End()

	require.Len(t, recorder.Ended(), 1)
	attrs := recorder.Ended()[0].Attributes()
	require.Equal(t, int64(0), attributeValue(attrs, "fanout.request_errors"))
	require.Equal(t, int64(1), attributeValue(attrs, "fanout.observed_downstream_requests"))
	require.Equal(t, int64(7), attributeValue(attrs, "fanout.chunk_refs"))
}

func TestDownstreamFanoutTracingSuppressesHandlerSpans(t *testing.T) {
	for _, count := range []int{DefaultDownstreamConcurrency + 1, DefaultDownstreamConcurrency * 4} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			tp := trace.NewTracerProvider(trace.WithSampler(trace.ParentBased(trace.AlwaysSample())), trace.WithSpanProcessor(recorder))
			oldTracer := tracer
			tracer = tp.Tracer("test")
			t.Cleanup(func() { tracer = oldTracer; require.NoError(t, tp.Shutdown(context.Background())) })

			params, err := logql.NewLiteralParams(`{app="test"}`, time.Now(), time.Now(), 0, 0, logproto.BACKWARD, 0, nil, nil)
			require.NoError(t, err)
			queries := make([]logql.DownstreamQuery, count)
			for i := range queries {
				queries[i] = logql.DownstreamQuery{Params: params}
			}
			handler := queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				_, child := tracer.Start(ctx, "handler-child")
				child.AddEvent("handler-event")
				child.End()
				return &LokiResponse{}, nil
			})
			instance := (&DownstreamHandler{next: handler}).Downstreamer(context.Background()).(*instance)
			ctx, root := tracer.Start(context.Background(), "root")
			_, err = instance.Downstream(ctx, queries, logql.NewBufferedAccumulator(len(queries)))
			require.NoError(t, err)
			root.End()
			require.Len(t, recorder.Ended(), 1)
			require.Empty(t, recorder.Ended()[0].Events())
			attrs := recorder.Ended()[0].Attributes()
			require.Equal(t, true, attributeValue(attrs, "fanout.aggregate_only"))
			require.Equal(t, int64(count), attributeValue(attrs, "fanout.planned_downstream_requests"))
		})
	}
}

func TestSplitFanoutTracingSuppressesIntervalSpans(t *testing.T) {
	for _, count := range []int{DefaultDownstreamConcurrency + 1, DefaultDownstreamConcurrency * 4} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			tp := trace.NewTracerProvider(trace.WithSampler(trace.ParentBased(trace.AlwaysSample())), trace.WithSpanProcessor(recorder))
			oldTracer := tracer
			tracer = tp.Tracer("test")
			t.Cleanup(func() { tracer = oldTracer; require.NoError(t, tp.Shutdown(context.Background())) })

			request := &LokiRequest{StartTs: time.Now().Add(-time.Hour), EndTs: time.Now()}
			intervals := make([]queryrangebase.Request, count)
			for i := range intervals {
				intervals[i] = request
			}
			handler := queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				_, child := tracer.Start(ctx, "handler-child")
				child.AddEvent("handler-event")
				child.End()
				return &LokiResponse{}, nil
			})
			middleware := SplitByIntervalMiddleware(nil, fakeLimits{splitDuration: map[string]time.Duration{"test": time.Minute}}, DefaultCodec, fanoutTestSplitter{requests: intervals}, nil)
			wrapped := middleware.Wrap(handler)
			ctx := user.InjectOrgID(context.Background(), "test")
			ctx, root := tracer.Start(ctx, "root")
			_, err := wrapped.Do(ctx, request)
			require.NoError(t, err)
			root.End()
			ended := recorder.Ended()
			require.Len(t, ended, 1)
			require.Empty(t, ended[0].Events())
			attrs := ended[0].Attributes()
			require.Equal(t, true, attributeValue(attrs, "fanout.aggregate_only"))
			require.Equal(t, int64(count), attributeValue(attrs, "fanout.planned_downstream_requests"))
			require.Equal(t, int64(count), attributeValue(attrs, "fanout.split_count"))
		})
	}
}

func TestShardResolverFanoutTracingSuppressesHandlerSpans(t *testing.T) {
	for _, count := range []int{DefaultDownstreamConcurrency + 1, DefaultDownstreamConcurrency * 4} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			tp := trace.NewTracerProvider(trace.WithSampler(trace.ParentBased(trace.AlwaysSample())), trace.WithSpanProcessor(recorder))
			oldTracer := tracer
			tracer = tp.Tracer("test")
			t.Cleanup(func() { tracer = oldTracer; require.NoError(t, tp.Shutdown(context.Background())) })

			matcherGroups := make([]syntax.MatcherRange, count)
			handler := queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				_, child := tracer.Start(ctx, "handler-child")
				child.AddEvent("handler-event")
				child.End()
				return &IndexStatsResponse{Response: &logproto.IndexStatsResponse{}}, nil
			})
			ctx, root := tracer.Start(context.Background(), "root")
			_, err := getStatsForMatchers(ctx, log.NewNopLogger(), handler, model.Time(0), model.Time(1), matcherGroups, 16, time.Minute)
			require.NoError(t, err)
			root.End()
			require.Len(t, recorder.Ended(), 1)
			require.Empty(t, recorder.Ended()[0].Events())
			attrs := recorder.Ended()[0].Attributes()
			require.Equal(t, int64(count), attributeValue(attrs, "fanout.planned_downstream_requests"))
		})
	}
}

func TestShardResolverFanoutTracingCountsResponseTypeErrors(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSampler(trace.ParentBased(trace.AlwaysSample())), trace.WithSpanProcessor(recorder))
	oldTracer := tracer
	tracer = tp.Tracer("test")
	t.Cleanup(func() { tracer = oldTracer; require.NoError(t, tp.Shutdown(context.Background())) })

	matcherGroups := make([]syntax.MatcherRange, DefaultDownstreamConcurrency+1)
	handler := queryrangebase.HandlerFunc(func(context.Context, queryrangebase.Request) (queryrangebase.Response, error) {
		return &LokiResponse{}, nil
	})
	ctx, root := tracer.Start(context.Background(), "root")
	_, err := getStatsForMatchers(ctx, log.NewNopLogger(), handler, model.Time(0), model.Time(1), matcherGroups, 1, time.Minute)
	require.Error(t, err)
	root.End()
	ended := recorder.Ended()
	require.Len(t, ended, 1)
	require.Equal(t, int64(1), attributeValue(ended[0].Attributes(), "fanout.request_errors"))
}

func TestFanoutTracingAggregatesStatsAndPreservesLogTraceID(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(
		trace.WithSampler(trace.ParentBased(trace.AlwaysSample())),
		trace.WithSpanProcessor(spans),
	)
	oldTracer := tracer
	tracer = tp.Tracer("test")
	t.Cleanup(func() {
		tracer = oldTracer
		require.NoError(t, tp.Shutdown(context.Background()))
	})

	ctx, root := tracer.Start(context.Background(), "root")
	suppressed, fanout, owner := startFanout(ctx, DefaultDownstreamConcurrency+1)
	require.True(t, owner)
	require.False(t, oteltrace.SpanFromContext(suppressed).SpanContext().IsSampled())
	result := &stats.Result{}
	result.Querier.Store.TotalChunksRef = 3
	result.Querier.Store.TotalChunksDownloaded = 2
	result.Querier.Store.ChunkFetchFailures = 1
	result.Caches.Chunk.Requests = 4
	result.Caches.Chunk.EntriesFound = 3
	result.Caches.Chunk.EntriesRequested = 5
	result.Caches.Chunk.BytesReceived = 10
	result.Caches.Chunk.BytesSent = 20
	result.Caches.Chunk.DownloadTime = int64(7)
	fanout.addSplits(3)
	fanout.addShards(4)
	fanout.addRequest(2*time.Millisecond, nil, result)
	fanout.addRequest(3*time.Millisecond, context.Canceled, nil)

	var output bytes.Buffer
	log.Logger(util_log.WithContext(suppressed, log.NewLogfmtLogger(&output))).Log("msg", "correlated")
	require.Contains(t, output.String(), root.SpanContext().TraceID().String())

	fanout.finish()
	root.End()
	require.Len(t, spans.Ended(), 1)
	attrs := spans.Ended()[0].Attributes()
	require.Equal(t, int64(DefaultDownstreamConcurrency+1), attributeValue(attrs, "fanout.planned_downstream_requests"))
	require.Equal(t, int64(3), attributeValue(attrs, "fanout.split_count"))
	require.Equal(t, int64(4), attributeValue(attrs, "fanout.shard_count"))
	require.Equal(t, int64(3), attributeValue(attrs, "fanout.chunk_refs"))
	require.Equal(t, int64(2), attributeValue(attrs, "fanout.chunk_downloads"))
	require.Equal(t, int64(1), attributeValue(attrs, "fanout.chunk_fetch_failures"))
	require.Equal(t, int64(4), attributeValue(attrs, "fanout.cache_requests"))
	require.Equal(t, int64(3), attributeValue(attrs, "fanout.cache_hits"))
	require.Equal(t, int64(2), attributeValue(attrs, "fanout.cache_misses"))
	require.Equal(t, int64(30), attributeValue(attrs, "fanout.cache_bytes"))
	require.Equal(t, int64(7), attributeValue(attrs, "fanout.cache_duration_ns"))
	require.Equal(t, int64(1), attributeValue(attrs, "fanout.request_errors"))
}

func attributeValue(attrs []attribute.KeyValue, key string) any {
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value.AsInterface()
		}
	}
	return nil
}
