package queryrange

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/stats"
)

func testSampleStreams() []queryrange.SampleStream {
	return []queryrange.SampleStream{
		{
			Labels: []client.LabelAdapter{{Name: "foo", Value: "bar"}},
			Samples: []client.Sample{
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
			Labels: []client.LabelAdapter{{Name: "bazz", Value: "buzz"}},
			Samples: []client.Sample{
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
			Points: []promql.Point{
				{
					V: 0,
					T: 0,
				},
				{
					V: 1,
					T: 1,
				},
				{
					V: 2,
					T: 2,
				},
			},
		},
		{
			Metric: labels.FromMap(map[string]string{
				"bazz": "buzz",
			}),
			Points: []promql.Point{
				{
					V: 4,
					T: 4,
				},
				{
					V: 5,
					T: 5,
				},
				{
					V: 6,
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
		input    queryrange.Response
		err      bool
		expected logql.Result
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
					Summary: stats.Summary{ExecTime: 1},
				},
			},
			expected: logql.Result{
				Statistics: stats.Result{
					Summary: stats.Summary{ExecTime: 1},
				},
				Data: logql.Streams{{
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
					Summary: stats.Summary{ExecTime: 1},
				},
				Response: &queryrange.PrometheusResponse{
					Data: queryrange.PrometheusData{
						Result: testSampleStreams(),
					},
				},
			},
			expected: logql.Result{
				Statistics: stats.Result{
					Summary: stats.Summary{ExecTime: 1},
				},
				Data: sampleStreamToMatrix(testSampleStreams()),
			},
		},
		{
			desc: "LokiPromResponseError",
			input: &LokiPromResponse{
				Response: &queryrange.PrometheusResponse{
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
	h := DownstreamHandler{nil}
	in := h.Downstreamer().(*instance)
	require.Equal(t, DefaultDownstreamConcurrency, in.parallelism)
	require.NotNil(t, in.locks)
	ensureParallelism(t, in, in.parallelism)
}

// Consumes the locks in an instance, making sure they're all available. Does not replace them and thus instance is unusable after. This is a cleanup test to ensure internal state
func ensureParallelism(t *testing.T, in *instance, n int) {
	for i := 0; i < n; i++ {
		select {
		case <-in.locks:
		case <-time.After(time.Millisecond):
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
	mkIn := func() *instance { return DownstreamHandler{nil}.Downstreamer().(*instance) }
	in := mkIn()

	queries := make([]logql.DownstreamQuery, in.parallelism+1)
	var mtx sync.Mutex
	var ct int

	// ensure we can execute queries that number more than the parallelism parameter
	_, err := in.For(context.TODO(), queries, func(_ logql.DownstreamQuery) (logql.Result, error) {
		mtx.Lock()
		defer mtx.Unlock()
		ct++
		return logql.Result{}, nil
	})
	require.Nil(t, err)
	require.Equal(t, len(queries), ct)
	ensureParallelism(t, in, in.parallelism)

	// ensure an early error abandons the other queues queries
	in = mkIn()
	ct = 0
	_, err = in.For(context.TODO(), queries, func(_ logql.DownstreamQuery) (logql.Result, error) {
		mtx.Lock()
		defer mtx.Unlock()
		ct++
		return logql.Result{}, errors.New("testerr")
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
				Shards: logql.Shards{
					{Shard: 0, Of: 2},
				},
			},
			{
				Shards: logql.Shards{
					{Shard: 1, Of: 2},
				},
			},
		},
		func(qry logql.DownstreamQuery) (logql.Result, error) {
			return logql.Result{
				Data: logql.Streams{{
					Labels: qry.Shards[0].String(),
				}},
			}, nil
		},
	)
	require.Nil(t, err)
	require.Equal(
		t,
		[]logql.Result{
			{
				Data: logql.Streams{{Labels: "0_of_2"}},
			},
			{
				Data: logql.Streams{{Labels: "1_of_2"}},
			},
		},
		results,
	)
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
	expr, err := logql.ParseExpr(`{foo="bar"}`)
	require.Nil(t, err)

	expectedResp := func() *LokiResponse {
		return &LokiResponse{
			Data: LokiData{
				Result: []logproto.Stream{{
					Labels: `{foo="bar"}`,
				}},
			},
			Statistics: stats.Result{
				Summary: stats.Summary{ExecTime: 1},
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

	var got queryrange.Request
	var want queryrange.Request
	handler := queryrange.HandlerFunc(
		func(_ context.Context, req queryrange.Request) (queryrange.Response, error) {
			// for some reason these seemingly can't be checked in their own goroutines,
			// so we assign them to scoped variables for later comparison.
			got = req
			want = ParamsToLokiRequest(params).WithShards(logql.Shards{{Shard: 0, Of: 2}}).WithQuery(expr.String())

			return expectedResp(), nil
		},
	)

	expected, err := ResponseToResult(expectedResp())
	require.Nil(t, err)

	results, err := DownstreamHandler{handler}.Downstreamer().Downstream(context.Background(), queries)

	require.Equal(t, want, got)

	require.Nil(t, err)
	require.Equal(t, []logql.Result{expected}, results)
}
