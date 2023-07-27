package queryrange

import (
	"container/heap"
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/sketch"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase/definitions"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

const (
	DefaultDownstreamConcurrency = 128
)

type DownstreamHandler struct {
	limits Limits
	next   queryrangebase.Handler
}

func ParamsToLokiRequest(probabilistic bool, params logql.Params, shards logql.Shards) queryrangebase.Request {
	if probabilistic {
		return &LokiProbabilisticRequest{
			Query:    params.Query(),
			Limit:    params.Limit(),
			Step:     params.Step().Milliseconds(),
			Interval: params.Interval().Milliseconds(),
			StartTs:  params.Start(),
			EndTs:    params.End(),
			Path:     "/loki/api/v1/probabilistic_query",
			Shards:   shards.Encode(),
		}
	}
	if logql.GetRangeType(params) == logql.InstantType {
		return &LokiInstantRequest{
			Query:     params.Query(),
			Limit:     params.Limit(),
			TimeTs:    params.Start(),
			Direction: params.Direction(),
			Path:      "/loki/api/v1/query", // TODO(owen-d): make this derivable
			Shards:    shards.Encode(),
		}
	}
	return &LokiRequest{
		Query:     params.Query(),
		Limit:     params.Limit(),
		Step:      params.Step().Milliseconds(),
		Interval:  params.Interval().Milliseconds(),
		StartTs:   params.Start(),
		EndTs:     params.End(),
		Direction: params.Direction(),
		Path:      "/loki/api/v1/query_range", // TODO(owen-d): make this derivable
		Shards:    shards.Encode(),
	}
}

// Note: After the introduction of the LimitedRoundTripper,
// bounding concurrency in the downstreamer is mostly redundant
// The reason we don't remove it is to prevent malicious queries
// from creating an unreasonably large number of goroutines, such as
// the case of a query like `a / a / a / a / a ..etc`, which could try
// to shard each leg, quickly dispatching an unreasonable number of goroutines.
// In the future, it's probably better to replace this with a channel based API
// so we don't have to do all this ugly edge case handling/accounting
func (h DownstreamHandler) Downstreamer(ctx context.Context) logql.Downstreamer {
	p := DefaultDownstreamConcurrency

	// We may increase parallelism above the default,
	// ensure we don't end up bottlenecking here.
	if user, err := tenant.TenantID(ctx); err == nil {
		if x := h.limits.MaxQueryParallelism(ctx, user); x > 0 {
			p = x
		}
	}

	locks := make(chan struct{}, p)
	for i := 0; i < p; i++ {
		locks <- struct{}{}
	}
	return &instance{
		parallelism: p,
		locks:       locks,
		handler:     h.next,
	}
}

// instance is an intermediate struct for controlling concurrency across a single query
type instance struct {
	parallelism   int
	locks         chan struct{}
	handler       queryrangebase.Handler
	probabilistic bool
}

func (in instance) Downstream(ctx context.Context, queries []logql.DownstreamQuery) ([]logqlmodel.Result, error) {
	return in.For(ctx, queries, func(qry logql.DownstreamQuery) (logqlmodel.Result, error) {
		probExpr, prob := qry.Expr.(logql.DownstreamTopkSampleExpr)
		expr := qry.Expr
		if prob {
			expr = probExpr.TopkSampleExpr
		}
		req := ParamsToLokiRequest(prob, qry.Params, qry.Shards).WithQuery(expr.String())
		sp, ctx := opentracing.StartSpanFromContext(ctx, "DownstreamHandler.instance")
		defer sp.Finish()
		logger := spanlogger.FromContext(ctx)
		defer logger.Finish()
		level.Debug(logger).Log("shards", fmt.Sprintf("%+v", qry.Shards), "query", req.GetQuery(), "step", req.GetStep(), "handler", reflect.TypeOf(in.handler))

		res, err := in.handler.Do(ctx, req)
		if err != nil {
			return logqlmodel.Result{}, err
		}
		return ResponseToResult(res)
	})
}

// For runs a function against a list of queries, collecting the results or returning an error. The indices are preserved such that input[i] maps to output[i].
func (in instance) For(
	ctx context.Context,
	queries []logql.DownstreamQuery,
	fn func(logql.DownstreamQuery) (logqlmodel.Result, error),
) ([]logqlmodel.Result, error) {
	type resp struct {
		i   int
		res logqlmodel.Result
		err error
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch := make(chan resp)

	// Make one goroutine to dispatch the other goroutines, bounded by instance parallelism
	go func() {
		for i := 0; i < len(queries); i++ {
			select {
			case <-ctx.Done():
				break
			case <-in.locks:
				go func(i int) {
					// release lock back into pool
					defer func() {
						in.locks <- struct{}{}
					}()

					res, err := fn(queries[i])
					response := resp{
						i:   i,
						res: res,
						err: err,
					}

					// Feed the result into the channel unless the work has completed.
					select {
					case <-ctx.Done():
					case ch <- response:
					}
				}(i)
			}
		}
	}()

	acc := newDownstreamAccumulator(queries[0].Params, len(queries))
	for i := 0; i < len(queries); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case resp := <-ch:
			if resp.err != nil {
				return nil, resp.err
			}
			if err := acc.Accumulate(ctx, resp.i, resp.res); err != nil {
				return nil, err
			}
		}
	}
	return acc.Result(), nil
}

// convert to matrix
func sampleStreamToMatrix(streams []queryrangebase.SampleStream) parser.Value {
	xs := make(promql.Matrix, 0, len(streams))
	for _, stream := range streams {
		x := promql.Series{}
		x.Metric = make(labels.Labels, 0, len(stream.Labels))
		for _, l := range stream.Labels {
			x.Metric = append(x.Metric, labels.Label(l))
		}

		x.Floats = make([]promql.FPoint, 0, len(stream.Samples))
		for _, sample := range stream.Samples {
			x.Floats = append(x.Floats, promql.FPoint{
				T: sample.TimestampMs,
				F: sample.Value,
			})
		}

		xs = append(xs, x)
	}
	return xs
}

func sampleStreamToVector(streams []queryrangebase.SampleStream) parser.Value {
	xs := make(promql.Vector, 0, len(streams))
	for _, stream := range streams {
		x := promql.Sample{}
		x.Metric = make(labels.Labels, 0, len(stream.Labels))
		for _, l := range stream.Labels {
			x.Metric = append(x.Metric, labels.Label(l))
		}

		x.T = stream.Samples[0].TimestampMs
		x.F = stream.Samples[0].Value

		xs = append(xs, x)
	}
	return xs
}

func ResponseToResult(resp queryrangebase.Response) (logqlmodel.Result, error) {
	switch r := resp.(type) {
	case *LokiResponse:
		if r.Error != "" {
			return logqlmodel.Result{}, fmt.Errorf("%s: %s", r.ErrorType, r.Error)
		}

		streams := make(logqlmodel.Streams, 0, len(r.Data.Result))

		for _, stream := range r.Data.Result {
			streams = append(streams, stream)
		}

		return logqlmodel.Result{
			Statistics: r.Statistics,
			Data:       streams,
			Headers:    resp.GetHeaders(),
		}, nil

	case *LokiPromResponse:
		if r.Response.Error != "" {
			return logqlmodel.Result{}, fmt.Errorf("%s: %s", r.Response.ErrorType, r.Response.Error)
		}
		if r.Response.Data.ResultType == loghttp.ResultTypeVector {
			return logqlmodel.Result{
				Statistics: r.Statistics,
				Data:       sampleStreamToVector(r.Response.Data.Result),
				Headers:    resp.GetHeaders(),
			}, nil
		}
		return logqlmodel.Result{
			Statistics: r.Statistics,
			Data:       sampleStreamToMatrix(r.Response.Data.Result),
			Headers:    resp.GetHeaders(),
		}, nil
	case *TopKSketchesResponse:
		matrix, err := sketch.TopKMatrixFromProto(r.Response)
		if err != nil {
			return logqlmodel.Result{}, fmt.Errorf("cannot decode topk sketch: %w", err)
		}

		return logqlmodel.Result{
			Data:    matrix,
			Headers: resp.GetHeaders(),
		}, nil
	default:
		return logqlmodel.Result{}, fmt.Errorf("cannot decode (%T)", resp)
	}
}

// downstreamAccumulator is one of two variants:
// a logsAccumulator or a bufferedAccumulator.
// Which variant is detected on the first call to Accumulate.
// Metric queries, which are generally small payloads, are buffered
// since the memory overhead is negligible.
// Log queries, sharded thousands of times and each returning <limit>
// results, can be _considerably_ larger.	In this case, we eagerly
// accumulate the results into a logsAccumulator, discarding values
// over the limit to keep memory pressure down while other subqueries
// are executing.
type downstreamAccumulator struct {
	acc    resultAccumulator
	params logql.Params
	n      int // number of queries, used to build slice size
}

type resultAccumulator interface {
	Accumulate(logqlmodel.Result, int) error
	Result() []logqlmodel.Result
}

func newDownstreamAccumulator(params logql.Params, nQueries int) *downstreamAccumulator {
	return &downstreamAccumulator{params: params, n: nQueries}
}

func (a *downstreamAccumulator) build(acc logqlmodel.Result) {
	if acc.Data.Type() == logqlmodel.ValueTypeStreams {

		// the stream accumulator stores a heap with reversed order
		// from the results we expect, so we need to reverse the direction
		direction := logproto.FORWARD
		if a.params.Direction() == logproto.FORWARD {
			direction = logproto.BACKWARD
		}

		a.acc = newStreamAccumulator(direction, int(a.params.Limit()))

	} else {
		a.acc = &bufferedAccumulator{
			results: make([]logqlmodel.Result, a.n),
		}

	}
}

func (a *downstreamAccumulator) Accumulate(_ context.Context, index int, acc logqlmodel.Result) error {
	// on first pass, determine which accumulator to use
	if a.acc == nil {
		a.build(acc)
	}

	return a.acc.Accumulate(acc, index)
}

func (a *downstreamAccumulator) Result() []logqlmodel.Result {
	if a.acc == nil {
		return nil
	}
	return a.acc.Result()

}

type bufferedAccumulator struct {
	results []logqlmodel.Result
}

func (a *bufferedAccumulator) Accumulate(acc logqlmodel.Result, i int) error {
	a.results[i] = acc
	return nil
}

func (a *bufferedAccumulator) Result() []logqlmodel.Result {
	return a.results
}

// heap impl for keeping only the top n results across m streams
// importantly, accumulatedStreams is _bounded_, so it will only
// store the top `limit` results across all streams.
// To implement this, we use a min-heap when looking
// for the max values (logproto.FORWARD)
// and vice versa for logproto.BACKWARD.
// This allows us to easily find the 'worst' value
// and replace it with a better one.
// Once we've fully processed all log lines,
// we return the heap in opposite order and then reverse it
// to get the correct order.
// Heap implements container/heap.Interface
// solely to use heap.Interface as a library.
// It is not intended for the heap pkg functions
// to otherwise call this type.
type accumulatedStreams struct {
	count, limit int
	labelmap     map[string]int
	streams      []*logproto.Stream
	order        logproto.Direction

	stats   stats.Result        // for accumulating statistics from downstream requests
	headers map[string][]string // for accumulating headers from downstream requests
}

func newStreamAccumulator(order logproto.Direction, limit int) *accumulatedStreams {
	return &accumulatedStreams{
		labelmap: make(map[string]int),
		order:    order,
		limit:    limit,

		headers: make(map[string][]string),
	}
}

// returns the top priority
func (acc *accumulatedStreams) top() (time.Time, bool) {
	if len(acc.streams) > 0 && len(acc.streams[0].Entries) > 0 {
		return acc.streams[0].Entries[len(acc.streams[0].Entries)-1].Timestamp, true
	}
	return time.Time{}, false
}

func (acc *accumulatedStreams) Find(labels string) (int, bool) {
	i, ok := acc.labelmap[labels]
	return i, ok
}

// number of streams
func (acc *accumulatedStreams) Len() int { return len(acc.streams) }

func (acc *accumulatedStreams) Swap(i, j int) {
	// for i=0, j=1

	// {'a': 0, 'b': 1}
	// [a, b]
	acc.streams[i], acc.streams[j] = acc.streams[j], acc.streams[i]
	// {'a': 0, 'b': 1}
	// [b, a]
	acc.labelmap[acc.streams[i].Labels] = i
	acc.labelmap[acc.streams[j].Labels] = j
	// {'a': 1, 'b': 0}
	// [b, a]
}

// first order by timestamp, then by labels
func (acc *accumulatedStreams) Less(i, j int) bool {
	// order by the 'oldest' entry in the stream
	if a, b := acc.streams[i].Entries[len(acc.streams[i].Entries)-1].Timestamp, acc.streams[j].Entries[len(acc.streams[j].Entries)-1].Timestamp; !a.Equal(b) {
		return acc.less(a, b)
	}
	return acc.streams[i].Labels <= acc.streams[j].Labels
}

func (acc *accumulatedStreams) less(a, b time.Time) bool {
	// use after for stable sort
	if acc.order == logproto.FORWARD {
		return !a.After(b)
	}
	return !b.After(a)
}

func (acc *accumulatedStreams) Push(x any) {
	s := x.(*logproto.Stream)
	if len(s.Entries) == 0 {
		return
	}

	if room := acc.limit - acc.count; room >= len(s.Entries) {
		if i, ok := acc.Find(s.Labels); ok {
			// stream already exists, append entries

			// these are already guaranteed to be sorted
			// Reasoning: we shard subrequests so each stream exists on only one
			// shard. Therefore, the only time a stream should already exist
			// is in successive splits, which are already guaranteed to be ordered
			// and we can just append.
			acc.appendTo(acc.streams[i], s)

			return
		}

		// new stream
		acc.addStream(s)
		return
	}

	// there's not enough room for all the entries,
	// so we need to
	acc.push(s)
}

// there's not enough room for all the entries.
// since we store them in a reverse heap relative to what we _want_
// (i.e. the max value for FORWARD, the min value for BACKWARD),
// we test if the new entry is better than the worst entry,
// swapping them if so.
func (acc *accumulatedStreams) push(s *logproto.Stream) {
	worst, ok := acc.top()
	room := min(acc.limit-acc.count, len(s.Entries))

	if !ok {
		if room == 0 {
			// special case: limit must be zero since there's no room and no worst entry
			return
		}
		s.Entries = s.Entries[:room]
		// special case: there are no entries in the heap. Push entries up to the limit
		acc.addStream(s)
		return
	}

	// since entries are sorted by timestamp from best -> worst,
	// we can discard the entire stream if the incoming best entry
	// is worse than the worst entry in the heap.
	cutoff := sort.Search(len(s.Entries), func(i int) bool {
		// TODO(refactor label comparison -- should be in another fn)
		if worst.Equal(s.Entries[i].Timestamp) {
			return acc.streams[0].Labels < s.Labels
		}
		return acc.less(s.Entries[i].Timestamp, worst)
	})
	s.Entries = s.Entries[:cutoff]

	for i := 0; i < len(s.Entries) && acc.less(worst, s.Entries[i].Timestamp); i++ {

		// push one entry at a time
		room = acc.limit - acc.count
		// pop if there's no room to make the heap small enough for an append;
		// in the short path of Push() we know that there's room for at least one entry
		if room == 0 {
			acc.Pop()
		}

		cpy := *s
		cpy.Entries = []logproto.Entry{s.Entries[i]}
		acc.Push(&cpy)

		// update worst
		worst, _ = acc.top()
	}
}

func (acc *accumulatedStreams) addStream(s *logproto.Stream) {
	// ensure entries conform to order we expect
	// TODO(owen-d): remove? should be unnecessary since we insert in appropriate order
	// but it's nice to have the safeguard
	sort.Slice(s.Entries, func(i, j int) bool {
		return acc.less(s.Entries[j].Timestamp, s.Entries[i].Timestamp)
	})

	acc.streams = append(acc.streams, s)
	i := len(acc.streams) - 1
	acc.labelmap[s.Labels] = i
	acc.count += len(s.Entries)
	heap.Fix(acc, i)
}

// dst must already exist in acc
func (acc *accumulatedStreams) appendTo(dst, src *logproto.Stream) {
	// these are already guaranteed to be sorted
	// Reasoning: we shard subrequests so each stream exists on only one
	// shard. Therefore, the only time a stream should already exist
	// is in successive splits, which are already guaranteed to be ordered
	// and we can just append.

	var needsSort bool
	for _, e := range src.Entries {
		// sort if order has broken
		if len(dst.Entries) > 0 && acc.less(dst.Entries[len(dst.Entries)-1].Timestamp, e.Timestamp) {
			needsSort = true
		}
		dst.Entries = append(dst.Entries, e)
	}

	if needsSort {
		sort.Slice(dst.Entries, func(i, j int) bool {
			// store in reverse order so we can more reliably insert without sorting and pop from end
			return acc.less(dst.Entries[j].Timestamp, dst.Entries[i].Timestamp)
		})
	}

	acc.count += len(src.Entries)
	heap.Fix(acc, acc.labelmap[dst.Labels])

}

// Pop returns a stream with one entry. It pops the first entry of the first stream
func (acc *accumulatedStreams) Pop() any {
	n := acc.Len()
	if n == 0 {
		return nil
	}

	stream := acc.streams[0]
	cpy := *stream
	cpy.Entries = []logproto.Entry{cpy.Entries[len(stream.Entries)-1]}
	stream.Entries = stream.Entries[:len(stream.Entries)-1]

	acc.count--

	if len(stream.Entries) == 0 {
		// remove stream
		acc.Swap(0, n-1)
		acc.streams[n-1] = nil // avoid leaking reference
		delete(acc.labelmap, stream.Labels)
		acc.streams = acc.streams[:n-1]

	}

	if acc.Len() > 0 {
		heap.Fix(acc, 0)
	}

	return &cpy
}

// Note: can only be called once as it will alter stream ordreing.
func (acc *accumulatedStreams) Result() []logqlmodel.Result {
	// sort streams by label
	sort.Slice(acc.streams, func(i, j int) bool {
		return acc.streams[i].Labels < acc.streams[j].Labels
	})

	streams := make(logqlmodel.Streams, 0, len(acc.streams))

	for _, s := range acc.streams {
		// sort entries by timestamp, inversely based on direction
		sort.Slice(s.Entries, func(i, j int) bool {
			return acc.less(s.Entries[j].Timestamp, s.Entries[i].Timestamp)
		})
		streams = append(streams, *s)
	}

	res := logqlmodel.Result{
		// stats & headers are already aggregated in the context
		Data:       streams,
		Statistics: acc.stats,
		Headers:    make([]*definitions.PrometheusResponseHeader, 0, len(acc.headers)),
	}

	for name, vals := range acc.headers {
		res.Headers = append(
			res.Headers,
			&definitions.PrometheusResponseHeader{
				Name:   name,
				Values: vals,
			},
		)
	}

	return []logqlmodel.Result{res}
}

func (acc *accumulatedStreams) Accumulate(x logqlmodel.Result, _ int) error {
	// TODO(owen-d/ewelch): Shard counts should be set by the querier
	// so we don't have to do it in tricky ways in multiple places.
	// See pkg/logql/downstream.go:DownstreamEvaluator.Downstream
	// for another example.
	if x.Statistics.Summary.Shards == 0 {
		x.Statistics.Summary.Shards = 1
	}
	acc.stats.Merge(x.Statistics)
	metadata.ExtendHeaders(acc.headers, x.Headers)

	switch got := x.Data.(type) {
	case logqlmodel.Streams:
		for i := range got {
			acc.Push(&got[i])
		}
	default:
		return fmt.Errorf("unexpected response type during response result accumulation. Got (%T), wanted %s", got, logqlmodel.ValueTypeStreams)
	}
	return nil
}
