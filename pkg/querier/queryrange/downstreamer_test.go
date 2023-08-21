package queryrange

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/axiomhq/hyperloglog"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

func testSampleStreams() []queryrangebase.SampleStream {
	return []queryrangebase.SampleStream{
		{
			Labels: []logproto.LabelAdapter{{Name: "foo", Value: "bar"}},
			Samples: []logproto.LegacySample{
				{
					Value:       0,
					TimestampMs: 0,
				},
				{
					Value:       1,
					TimestampMs: 1,
				},
				{
					Value:       2,
					TimestampMs: 2,
				},
			},
		},
		{
			Labels: []logproto.LabelAdapter{{Name: "bazz", Value: "buzz"}},
			Samples: []logproto.LegacySample{
				{
					Value:       4,
					TimestampMs: 4,
				},
				{
					Value:       5,
					TimestampMs: 5,
				},
				{
					Value:       6,
					TimestampMs: 6,
				},
			},
		},
	}
}

func TestSampleStreamToMatrix(t *testing.T) {
	input := testSampleStreams()
	expected := promql.Matrix{
		{
			Metric: labels.FromMap(map[string]string{
				"foo": "bar",
			}),
			Floats: []promql.FPoint{
				{
					F: 0,
					T: 0,
				},
				{
					F: 1,
					T: 1,
				},
				{
					F: 2,
					T: 2,
				},
			},
		},
		{
			Metric: labels.FromMap(map[string]string{
				"bazz": "buzz",
			}),
			Floats: []promql.FPoint{
				{
					F: 4,
					T: 4,
				},
				{
					F: 5,
					T: 5,
				},
				{
					F: 6,
					T: 6,
				},
			},
		},
	}
	require.Equal(t, expected, sampleStreamToMatrix(input))
}

func TestResponseToResult(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		input    queryrangebase.Response
		err      bool
		expected logqlmodel.Result
	}{
		{
			desc: "LokiResponse",
			input: &LokiResponse{
				Data: LokiData{
					Result: []logproto.Stream{{
						Labels: `{foo="bar"}`,
					}},
				},
				Statistics: stats.Result{
					Summary: stats.Summary{QueueTime: 1, ExecTime: 2},
				},
			},
			expected: logqlmodel.Result{
				Statistics: stats.Result{
					Summary: stats.Summary{QueueTime: 1, ExecTime: 2},
				},
				Data: logqlmodel.Streams{{
					Labels: `{foo="bar"}`,
				}},
			},
		},
		{
			desc: "LokiResponseError",
			input: &LokiResponse{
				Error:     "foo",
				ErrorType: "bar",
			},
			err: true,
		},
		{
			desc: "LokiPromResponse",
			input: &LokiPromResponse{
				Statistics: stats.Result{
					Summary: stats.Summary{QueueTime: 1, ExecTime: 2},
				},
				Response: &queryrangebase.PrometheusResponse{
					Data: queryrangebase.PrometheusData{
						Result: testSampleStreams(),
					},
				},
			},
			expected: logqlmodel.Result{
				Statistics: stats.Result{
					Summary: stats.Summary{QueueTime: 1, ExecTime: 2},
				},
				Data: sampleStreamToMatrix(testSampleStreams()),
			},
		},
		{
			desc: "LokiPromResponseError",
			input: &LokiPromResponse{
				Response: &queryrangebase.PrometheusResponse{
					Error:     "foo",
					ErrorType: "bar",
				},
			},
			err: true,
		},
		{
			desc:  "UnexpectedTypeError",
			input: nil,
			err:   true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			out, err := ResponseToResult(tc.input)
			if tc.err {
				require.NotNil(t, err)
			}
			require.Equal(t, tc.expected, out)
		})
	}
}

func TestDownstreamHandler(t *testing.T) {
	// Pretty poor test, but this is just a passthrough struct, so ensure we create locks
	// and can consume them
	h := DownstreamHandler{limits: fakeLimits{}, next: nil}
	in := h.Downstreamer(context.Background()).(*instance)
	require.Equal(t, DefaultDownstreamConcurrency, in.parallelism)
	require.NotNil(t, in.locks)
	ensureParallelism(t, in, in.parallelism)
}

// Consumes the locks in an instance, making sure they're all available. Does not replace them and thus instance is unusable after. This is a cleanup test to ensure internal state
func ensureParallelism(t *testing.T, in *instance, n int) {
	for i := 0; i < n; i++ {
		select {
		case <-in.locks:
		case <-time.After(time.Second):
			require.FailNow(t, "lock couldn't be acquired")
		}
	}
	// ensure no more locks available
	select {
	case <-in.locks:
		require.FailNow(t, "unexpected lock acquisition")
	default:
	}
}

func TestInstanceFor(t *testing.T) {
	mkIn := func() *instance {
		return DownstreamHandler{
			limits: fakeLimits{},
			next:   nil,
		}.Downstreamer(context.Background()).(*instance)
	}
	in := mkIn()
	newParams := func() logql.Params {
		return logql.NewLiteralParams(
			"",
			time.Now(),
			time.Now(),
			0,
			0,
			logproto.BACKWARD,
			1000,
			nil,
		)
	}

	var queries []logql.DownstreamQuery
	for i := 0; i < in.parallelism+1; i++ {
		queries = append(queries, logql.DownstreamQuery{
			Params: newParams(),
		})
	}
	var mtx sync.Mutex
	var ct int

	// ensure we can execute queries that number more than the parallelism parameter
	_, err := in.For(context.TODO(), queries, func(_ logql.DownstreamQuery) (logqlmodel.Result, error) {
		mtx.Lock()
		defer mtx.Unlock()
		ct++
		return logqlmodel.Result{
			Data: promql.Scalar{},
		}, nil
	})
	require.Nil(t, err)
	require.Equal(t, len(queries), ct)
	ensureParallelism(t, in, in.parallelism)

	// ensure an early error abandons the other queues queries
	in = mkIn()
	ct = 0
	_, err = in.For(context.TODO(), queries, func(_ logql.DownstreamQuery) (logqlmodel.Result, error) {
		mtx.Lock()
		defer mtx.Unlock()
		ct++
		return logqlmodel.Result{}, errors.New("testerr")
	})
	require.NotNil(t, err)
	mtx.Lock()
	ctRes := ct
	mtx.Unlock()

	// Ensure no more than the initial batch was parallelized. (One extra instance can be started though.)
	require.LessOrEqual(t, ctRes, in.parallelism+1)
	ensureParallelism(t, in, in.parallelism)

	in = mkIn()
	results, err := in.For(
		context.TODO(),
		[]logql.DownstreamQuery{
			{
				Params: newParams(),
				Shards: logql.Shards{
					{Shard: 0, Of: 2},
				},
			},
			{
				Params: newParams(),
				Shards: logql.Shards{
					{Shard: 1, Of: 2},
				},
			},
		},
		func(qry logql.DownstreamQuery) (logqlmodel.Result, error) {
			return logqlmodel.Result{
				Data: promql.Scalar{
					V: float64(qry.Shards[0].Shard),
				},
			}, nil
		},
	)
	require.Nil(t, err)
	require.Equal(t, 2, len(results))
	for i := range results {
		require.Equal(t, float64(i), results[i].Data.(promql.Scalar).V)
	}
	ensureParallelism(t, in, in.parallelism)
}

func TestInstanceDownstream(t *testing.T) {
	params := logql.NewLiteralParams(
		"",
		time.Now(),
		time.Now(),
		0,
		0,
		logproto.BACKWARD,
		1000,
		nil,
	)
	expr, err := syntax.ParseExpr(`{foo="bar"}`)
	require.Nil(t, err)

	expectedResp := func() *LokiResponse {
		return &LokiResponse{
			Data: LokiData{
				Result: []logproto.Stream{{
					Labels: `{foo="bar"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(0, 0), Line: "foo"},
					},
				}},
			},
			Statistics: stats.Result{
				Summary: stats.Summary{QueueTime: 1, ExecTime: 2},
			},
		}
	}

	queries := []logql.DownstreamQuery{
		{
			Expr:   expr,
			Params: params,
			Shards: logql.Shards{{Shard: 0, Of: 2}},
		},
	}

	var got queryrangebase.Request
	var want queryrangebase.Request
	handler := queryrangebase.HandlerFunc(
		func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
			// for some reason these seemingly can't be checked in their own goroutines,
			// so we assign them to scoped variables for later comparison.
			got = req
			// TODO does this still need to take the probabilistic param
			want = ParamsToLokiRequest(false, params, queries[0].Shards).WithQuery(expr.String())

			return expectedResp(), nil
		},
	)

	expected, err := ResponseToResult(expectedResp())
	require.Nil(t, err)

	results, err := DownstreamHandler{
		limits: fakeLimits{},
		next:   handler,
	}.Downstreamer(context.Background()).Downstream(context.Background(), queries)

	require.Equal(t, want, got)

	require.Nil(t, err)
	require.Equal(t, 1, len(results))
	require.Equal(t, expected.Data, results[0].Data)
}

func TestInstanceDownstreamProbabilistic(t *testing.T) {
	params := logql.NewLiteralParams(
		"topk(10, count_over_time({ foo = \"bar\" }[5h]))",
		time.Now(),
		time.Now(),
		0,
		0,
		logproto.BACKWARD,
		1000,
		nil,
	)
	expr, err := syntax.ParseExpr(`topk(10, count_over_time({ foo = "bar" }[5h]))`)
	require.Nil(t, err)

	expectedResp := func() *TopKSketchesResponse {
		hll := hyperloglog.New16()
		hll.Insert([]byte("asdf"))
		hllBytes, err := hll.MarshalBinary()
		if err != nil {
			return nil
		}
		return &TopKSketchesResponse{
			Response: &logproto.TopKMatrix{
				Values: []*logproto.TopKMatrix_Vector{
					{
						Topk: &logproto.TopK{
							Cms: &logproto.CountMinSketch{Depth: 2, Width: 12, Counters: make([]float64, 2*12)},
							Results: []*logproto.TopK_Result{
								{
									Key: "",
									List: []*logproto.TopK_Pair{
										{
											Event: "asdf",
											Count: 100,
										},
									},
								},
							},
							Hyperloglog: hllBytes,
						},
						TimestampMs: 0,
					},
				},
			},
		}
	}

	queries := []logql.DownstreamQuery{
		{
			Expr: logql.DownstreamTopkSampleExpr{
				TopkSampleExpr: expr.(syntax.TopkSampleExpr),
			},
			Params: params,
			Shards: logql.Shards{{Shard: 0, Of: 2}},
		},
	}

	var got queryrangebase.Request
	var want queryrangebase.Request
	handler := queryrangebase.HandlerFunc(
		func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
			// for some reason these seemingly can't be checked in their own goroutines,
			// so we assign them to scoped variables for later comparison.
			got = req
			// todo does this still need to take probabilistic param
			want = ParamsToLokiRequest(true, params, queries[0].Shards).WithQuery(expr.String())

			return expectedResp(), nil
		},
	)

	expected, err := ResponseToResult(expectedResp())
	require.Nil(t, err)

	results, err := DownstreamHandler{
		limits: fakeLimits{},
		next:   handler,
	}.Downstreamer(context.Background()).Downstream(context.Background(), queries)

	require.Equal(t, want, got)

	require.IsType(t, &LokiProbabilisticRequest{}, got)
	require.Nil(t, err)
	require.Equal(t, 1, len(results))
	require.Equal(t, expected.Data, results[0].Data)
}

func TestCancelWhileWaitingResponse(t *testing.T) {
	mkIn := func() *instance {
		return DownstreamHandler{
			limits: fakeLimits{},
			next:   nil,
		}.Downstreamer(context.Background()).(*instance)
	}
	in := mkIn()

	queries := make([]logql.DownstreamQuery, in.parallelism+1)

	ctx, cancel := context.WithCancel(context.Background())

	// Launch the For call in a goroutine because it blocks and we need to be able to cancel the context
	// to prove it will exit when the context is canceled.
	b := atomic.NewBool(false)
	go func() {
		_, _ = in.For(ctx, queries, func(_ logql.DownstreamQuery) (logqlmodel.Result, error) {
			// Intended to keep the For method from returning unless the context is canceled.
			time.Sleep(100 * time.Second)
			return logqlmodel.Result{}, nil
		})
		// Should only reach here if the For method returns after the context is canceled.
		b.Store(true)
	}()

	// Cancel the parent call
	cancel()
	require.Eventually(t, func() bool {
		return b.Load()
	}, 5*time.Second, 10*time.Millisecond,
		"The parent context calling the Downstreamer For method was canceled "+
			"but the For method did not return as expected.")
}

func TestDownstreamerUsesCorrectParallelism(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "fake")
	l := fakeLimits{maxQueryParallelism: 4}
	d := DownstreamHandler{
		limits: l,
		next:   nil,
	}.Downstreamer(ctx)

	i := d.(*instance)
	close(i.locks)
	var ct int
	for range i.locks {
		ct++
	}
	require.Equal(t, l.maxQueryParallelism, ct)
}

func newStream(start, end time.Time, delta time.Duration, ls string, direction logproto.Direction) *logproto.Stream {
	s := &logproto.Stream{
		Labels: ls,
	}
	for t := start; t.Before(end); t = t.Add(delta) {
		s.Entries = append(s.Entries, logproto.Entry{
			Timestamp: t,
			Line:      fmt.Sprintf("%d", t.Unix()),
		})
	}
	if direction == logproto.BACKWARD {
		// simulate data coming in reverse order (logproto.BACKWARD)
		for i, j := 0, len(s.Entries)-1; i < j; i, j = i+1, j-1 {
			s.Entries[i], s.Entries[j] = s.Entries[j], s.Entries[i]
		}
	}
	return s
}

func newStreams(start, end time.Time, delta time.Duration, n int, direction logproto.Direction) (res []*logproto.Stream) {
	for i := 0; i < n; i++ {
		res = append(res, newStream(start, end, delta, fmt.Sprintf(`{n="%d"}`, i), direction))
	}
	return res
}

func TestAccumulatedStreams(t *testing.T) {
	lim := 30
	nStreams := 10
	start, end := 0, 10
	// for a logproto.BACKWARD query, we use a min heap based on FORWARD
	// to store the _earliest_ timestamp of the _latest_ entries, up to `limit`
	xs := newStreams(time.Unix(int64(start), 0), time.Unix(int64(end), 0), time.Second, nStreams, logproto.BACKWARD)
	acc := newStreamAccumulator(logproto.FORWARD, lim)
	for _, x := range xs {
		acc.Push(x)
	}

	for i := 0; i < lim; i++ {
		got := acc.Pop().(*logproto.Stream)
		require.Equal(t, fmt.Sprintf(`{n="%d"}`, i%nStreams), got.Labels)
		exp := (nStreams*(end-start) - lim + i) / nStreams
		require.Equal(t, time.Unix(int64(exp), 0), got.Entries[0].Timestamp)
	}

}

func TestDownstreamAccumulatorSimple(t *testing.T) {
	lim := 30
	start, end := 0, 10
	direction := logproto.BACKWARD

	streams := newStreams(time.Unix(int64(start), 0), time.Unix(int64(end), 0), time.Second, 10, direction)
	x := make(logqlmodel.Streams, 0, len(streams))
	for _, s := range streams {
		x = append(x, *s)
	}
	// dummy params. Only need to populate direction & limit
	params := logql.NewLiteralParams(
		"", time.Time{}, time.Time{}, 0, 0, direction, uint32(lim), nil,
	)

	acc := newDownstreamAccumulator(params, 1)
	result := logqlmodel.Result{
		Data: x,
	}

	require.Nil(t, acc.Accumulate(context.Background(), 0, result))

	res := acc.Result()[0]
	got, ok := res.Data.(logqlmodel.Streams)
	require.Equal(t, true, ok)
	require.Equal(t, 10, len(got), "correct number of streams")

	// each stream should have the top 3 entries
	for i := 0; i < 10; i++ {
		require.Equal(t, 3, len(got[i].Entries), "correct number of entries in stream")
		for j := 0; j < 3; j++ {
			require.Equal(t, time.Unix(int64(9-j), 0), got[i].Entries[j].Timestamp, "correct timestamp")
		}
	}
}

// TestDownstreamAccumulatorMultiMerge simulates merging multiple
// sub-results from different queries.
func TestDownstreamAccumulatorMultiMerge(t *testing.T) {
	for _, direction := range []logproto.Direction{logproto.BACKWARD, logproto.FORWARD} {
		t.Run(direction.String(), func(t *testing.T) {
			nQueries := 10
			delta := 10 // 10 entries per stream, 1s apart
			streamsPerQuery := 10
			lim := 30

			payloads := make([]logqlmodel.Streams, 0, nQueries)
			for i := 0; i < nQueries; i++ {
				start := i * delta
				end := start + delta
				streams := newStreams(time.Unix(int64(start), 0), time.Unix(int64(end), 0), time.Second, streamsPerQuery, direction)
				var res logqlmodel.Streams
				for i := range streams {
					res = append(res, *streams[i])
				}
				payloads = append(payloads, res)

			}

			// queries are always dispatched in the correct order.
			// oldest time ranges first in the case of logproto.FORWARD
			// and newest time ranges first in the case of logproto.BACKWARD
			if direction == logproto.BACKWARD {
				for i, j := 0, len(payloads)-1; i < j; i, j = i+1, j-1 {
					payloads[i], payloads[j] = payloads[j], payloads[i]
				}
			}

			// dummy params. Only need to populate direction & limit
			params := logql.NewLiteralParams(
				"", time.Time{}, time.Time{}, 0, 0, direction, uint32(lim), nil,
			)

			acc := newDownstreamAccumulator(params, 1)
			for i := 0; i < nQueries; i++ {
				err := acc.Accumulate(context.Background(), i, logqlmodel.Result{
					Data: payloads[i],
				})
				require.Nil(t, err)
			}

			got, ok := acc.Result()[0].Data.(logqlmodel.Streams)
			require.Equal(t, true, ok)
			require.Equal(t, int64(nQueries), acc.Result()[0].Statistics.Summary.Shards)

			// each stream should have the top 3 entries
			for i := 0; i < streamsPerQuery; i++ {
				stream := got[i]
				require.Equal(t, fmt.Sprintf(`{n="%d"}`, i), stream.Labels, "correct labels")
				ln := lim / streamsPerQuery
				require.Equal(t, ln, len(stream.Entries), "correct number of entries in stream")
				switch direction {
				case logproto.BACKWARD:
					for i := 0; i < ln; i++ {
						offset := delta*nQueries - 1 - i
						require.Equal(t, time.Unix(int64(offset), 0), stream.Entries[i].Timestamp, "correct timestamp")
					}
				default:
					for i := 0; i < ln; i++ {
						offset := i
						require.Equal(t, time.Unix(int64(offset), 0), stream.Entries[i].Timestamp, "correct timestamp")
					}
				}
			}
		})
	}
}
