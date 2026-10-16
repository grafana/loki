package queryrange

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/querier/testutil"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
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
		params, err := logql.NewLiteralParams(
			`{app="foo"}`,
			time.Now(),
			time.Now(),
			0,
			0,
			logproto.BACKWARD,
			1000,
			nil,
			nil,
		)
		require.NoError(t, err)
		return params
	}

	var queries []logql.DownstreamQuery
	for i := 0; i < in.parallelism+1; i++ {
		queries = append(queries, logql.DownstreamQuery{
			Params: newParams(),
		})
	}
	var mtx sync.Mutex
	var ct int

	acc := logql.NewBufferedAccumulator(len(queries))

	// ensure we can execute queries that number more than the parallelism parameter
	_, err := in.For(context.TODO(), queries, acc, func(_ logql.DownstreamQuery) (logqlmodel.Result, error) {
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
	_, err = in.For(context.TODO(), queries, acc, func(_ logql.DownstreamQuery) (logqlmodel.Result, error) {
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
				Params: logql.ParamsWithShardsOverride{
					Params: newParams(),
					ShardsOverride: logql.Shards{
						logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: 0, Of: 2}),
					}.Encode(),
				},
			},
			{
				Params: logql.ParamsWithShardsOverride{
					Params: newParams(),
					ShardsOverride: logql.Shards{
						logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: 1, Of: 2}),
					}.Encode(),
				},
			},
		},
		logql.NewBufferedAccumulator(2),
		func(qry logql.DownstreamQuery) (logqlmodel.Result, error) {
			// Decode shard
			s := strings.Split(qry.Params.Shards()[0], "_")
			shard, err := strconv.Atoi(s[0])
			if err != nil {
				return logqlmodel.Result{}, err
			}
			return logqlmodel.Result{
				Data: promql.Scalar{
					V: float64(shard),
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

func TestParamsToLokiRequest(t *testing.T) {
	// Usually, queryrangebase.Request converted into Params and passed to downstream engine
	// And converted back to queryrangebase.Request from the params before executing those queries.
	// This test makes sure, we don't loose `CachingOption` during this transformation.

	ts := time.Now()
	qs := `sum(rate({foo="bar"}[2h] offset 1h))`

	cases := []struct {
		name    string
		caching resultscache.CachingOptions
		expReq  queryrangebase.Request
	}{
		{
			"instant-query-cache-enabled",
			resultscache.CachingOptions{
				Disabled: false,
			},
			&LokiInstantRequest{
				Query:       qs,
				Limit:       1000,
				TimeTs:      ts,
				Direction:   logproto.BACKWARD,
				Path:        "/loki/api/v1/query",
				Shards:      nil,
				StoreChunks: nil,
				Plan:        testutil.MustPlan(qs),
				CachingOptions: resultscache.CachingOptions{
					Disabled: false,
				},
			},
		},
		{
			"instant-query-cache-disabled",
			resultscache.CachingOptions{
				Disabled: true,
			},
			&LokiInstantRequest{
				Query:       qs,
				Limit:       1000,
				TimeTs:      ts,
				Direction:   logproto.BACKWARD,
				Path:        "/loki/api/v1/query",
				Shards:      nil,
				StoreChunks: nil,
				Plan:        testutil.MustPlan(qs),
				CachingOptions: resultscache.CachingOptions{
					Disabled: true,
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params, err := logql.NewLiteralParamsWithCaching(
				`sum(rate({foo="bar"}[2h] offset 1h))`,
				ts,
				ts,
				0,
				0,
				logproto.BACKWARD,
				1000,
				nil,
				nil,
				tc.caching,
			)
			require.NoError(t, err)
			req := ParamsToLokiRequest(params)
			require.Equal(t, tc.expReq, req)
		})
	}
}

func TestParamsToLokiRequestPreservesHintRanges(t *testing.T) {
	start := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	hintRanges := []logproto.HintTimeRange{{
		Start: start.Add(5 * time.Minute),
		End:   start.Add(10 * time.Minute),
	}}
	req := &LokiRequest{
		Query:      `{foo="bar"} |= "error"`,
		StartTs:    start,
		EndTs:      start.Add(time.Hour),
		Plan:       testutil.MustPlan(`{foo="bar"} |= "error"`),
		HintRanges: hintRanges,
	}

	params, err := ParamsFromRequest(req)
	require.NoError(t, err)

	got := ParamsToLokiRequest(params).(*LokiRequest)
	require.Equal(t, hintRanges, got.HintRanges)
}

func TestInstanceDownstream(t *testing.T) {
	t.Run("Downstream simple query", func(t *testing.T) {
		ts := time.Unix(1, 0)

		params, err := logql.NewLiteralParams(
			`{foo="bar"}`,
			ts,
			ts,
			0,
			0,
			logproto.BACKWARD,
			1000,
			nil,
			nil,
		)
		require.NoError(t, err)
		expr, err := syntax.ParseExpr(`{foo="bar"}`)
		require.NoError(t, err)

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
				Params: logql.ParamsWithShardsOverride{
					Params: logql.ParamsWithExpressionOverride{Params: params, ExpressionOverride: expr},
					ShardsOverride: logql.Shards{
						logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: 0, Of: 2}),
					}.Encode(),
				},
			},
		}

		var got queryrangebase.Request
		var want queryrangebase.Request
		handler := queryrangebase.HandlerFunc(
			func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
				// for some reason these seemingly can't be checked in their own goroutines,
				// so we assign them to scoped variables for later comparison.
				got = req
				want = ParamsToLokiRequest(queries[0].Params).WithQuery(expr.String())

				return expectedResp(), nil
			},
		)

		expected, err := ResponseToResult(expectedResp())
		require.Nil(t, err)

		results, err := DownstreamHandler{
			limits: fakeLimits{},
			next:   handler,
		}.Downstreamer(context.Background()).Downstream(context.Background(), queries, logql.NewBufferedAccumulator(len(queries)))

		fmt.Println("want", want.GetEnd(), want.GetStart(), "got", got.GetEnd(), got.GetStart())
		require.Equal(t, want, got)
		require.Nil(t, err)
		require.Equal(t, 1, len(results))
		require.Equal(t, expected.Data, results[0].Data)
	})

	t.Run("Downstream with offset removed", func(t *testing.T) {
		ts := time.Unix(1, 0)

		params, err := logql.NewLiteralParams(
			`sum(rate({foo="bar"}[2h] offset 1h))`,
			ts,
			ts,
			0,
			0,
			logproto.BACKWARD,
			1000,
			nil,
			nil,
		)
		require.NoError(t, err)

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
				Params: params,
			},
		}

		var got queryrangebase.Request
		var want queryrangebase.Request
		handler := queryrangebase.HandlerFunc(
			func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
				// for some reason these seemingly can't be checked in their own goroutines,
				// so we assign them to scoped variables for later comparison.
				got = req
				want = ParamsToLokiRequest(params).WithQuery(`sum(rate({foo="bar"}[2h]))`).WithStartEnd(ts.Add(-1*time.Hour), ts.Add(-1*time.Hour)) // without offset and start, end adjusted for instant query

				return expectedResp(), nil
			},
		)

		expected, err := ResponseToResult(expectedResp())
		require.NoError(t, err)

		results, err := DownstreamHandler{
			limits:     fakeLimits{},
			next:       handler,
			splitAlign: true,
		}.Downstreamer(context.Background()).Downstream(context.Background(), queries, logql.NewBufferedAccumulator(len(queries)))

		assert.Equal(t, want, got)

		require.Nil(t, err)
		require.Equal(t, 1, len(results))
		require.Equal(t, expected.Data, results[0].Data)

	})
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
	acc := logql.NewBufferedAccumulator(len(queries))

	ctx, cancel := context.WithCancel(context.Background())

	// Launch the For call in a goroutine because it blocks and we need to be able to cancel the context
	// to prove it will exit when the context is canceled.
	b := atomic.NewBool(false)
	go func() {
		_, _ = in.For(ctx, queries, acc, func(_ logql.DownstreamQuery) (logqlmodel.Result, error) {
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

func TestCancelDoesNotLeakGoroutines(t *testing.T) {
	mkIn := func() *instance {
		return DownstreamHandler{
			limits: fakeLimits{},
			next:   nil,
		}.Downstreamer(context.Background()).(*instance)
	}
	in := mkIn()

	params, err := logql.NewLiteralParams(
		`{app="foo"}`,
		time.Now(),
		time.Now(),
		0,
		0,
		logproto.BACKWARD,
		1000,
		nil,
		nil,
	)
	require.NoError(t, err)

	queries := make([]logql.DownstreamQuery, in.parallelism+1)
	for i := range queries {
		queries[i] = logql.DownstreamQuery{Params: params}
	}

	// Record baseline goroutine count.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	// Run multiple iterations of canceled queries to amplify any leak.
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		acc := logql.NewBufferedAccumulator(len(queries))

		started := atomic.NewInt32(0)
		done := make(chan struct{})
		go func() {
			_, _ = in.For(ctx, queries, acc, func(_ logql.DownstreamQuery) (logqlmodel.Result, error) {
				started.Inc()
				// Block until context is canceled.
				<-ctx.Done()
				return logqlmodel.Result{}, ctx.Err()
			})
			close(done)
		}()

		// Wait for at least one job to start, then cancel.
		require.Eventually(t, func() bool {
			return started.Load() > 0
		}, 5*time.Second, time.Millisecond)

		cancel()
		<-done
	}

	// Allow goroutines to settle.
	runtime.GC()
	require.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= baseline+10
	}, 10*time.Second, 100*time.Millisecond,
		"goroutine count did not return to baseline; likely leak in For on context cancellation (baseline=%d, current=%d)",
		baseline, runtime.NumGoroutine(),
	)
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

// gatedShardAccumulator holds the collector while other shards finish execution.
type gatedShardAccumulator struct {
	logql.Accumulator
	firstResultReceived chan struct{}
	releaseFirstResult  <-chan struct{}
}

func (a *gatedShardAccumulator) Accumulate(ctx context.Context, result logqlmodel.Result, index int) error {
	if index == 0 {
		// Pause before the wrapped accumulator can accept or reject shard 0.
		// Tests can then complete shard 1 while its delivery is blocked.
		close(a.firstResultReceived)
		<-a.releaseFirstResult
	}
	return a.Accumulator.Accumulate(ctx, result, index)
}

type controlledShardDownstreamer struct {
	instance
	query func(logql.DownstreamQuery) (logqlmodel.Result, error)
}

func (d controlledShardDownstreamer) Downstream(ctx context.Context, queries []logql.DownstreamQuery, acc logql.Accumulator) ([]logqlmodel.Result, error) {
	return d.For(ctx, queries, acc, d.query)
}

func TestDownstreamPreservesCompletedShardUsage(t *testing.T) {
	workerErr := errors.New("shard execution failed")
	for _, tc := range []struct {
		name                 string
		queryCount           int
		limit                uint32
		workerErr            error
		cancelQuery          bool
		returnScalar         bool
		useSketchAccumulator bool
		wantErr              error
		wantErrorContains    string
	}{
		{name: "success", queryCount: 2, limit: 2},
		// Both shards reach the accumulator, but only one entry is returned.
		{name: "line limit", queryCount: 2, limit: 1},
		{
			name: "worker failure", queryCount: 3, limit: 2, workerErr: workerErr,
			wantErr: workerErr,
		},
		{
			name: "cancellation", queryCount: 4, limit: 2, cancelQuery: true,
			wantErr: context.Canceled,
		},
		{
			name: "stream rejection", queryCount: 2, limit: 2, returnScalar: true,
			wantErrorContains: "unexpected",
		},
		{
			name: "sketch rejection", queryCount: 2, limit: 2, returnScalar: true, useSketchAccumulator: true,
			wantErrorContains: "unexpected",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				// Check normal and partial statistics independently so bytes recorded
				// in both contexts cannot pass the assertions.
				parent, cancel := context.WithCancel(context.Background())
				defer cancel()
				partial, ctx := stats.NewPartialContext(parent)
				queryStats, ctx := stats.NewContext(ctx)

				// Create downstream jobs representing shard queries; execution is simulated
				// below. Jobs 0 and 1 return results while collection is paused in the first
				// Accumulate call. Failure and cancellation cases add jobs that fail, wait
				// for cancellation, or finish after usage has been finalized.
				queries := make([]logql.DownstreamQuery, tc.queryCount)
				for i := range queries {
					params, err := logql.NewLiteralParams(`{app="test"}`, time.Unix(int64(i), 0), time.Unix(int64(i+1), 0), 0, 0, logproto.FORWARD, tc.limit, nil, nil)
					require.NoError(t, err)
					queries[i].Params = params
				}

				// Choose the accumulator and hold its first call while sibling shards finish.
				var accumulator logql.Accumulator = logql.NewStreamAccumulator(queries[0].Params)
				if tc.useSketchAccumulator {
					accumulator = &logql.QuantileSketchAccumulator{}
				}

				firstReceived := make(chan struct{})
				releaseFirst := make(chan struct{})
				defer close(releaseFirst)
				releaseFailure := make(chan struct{})
				defer close(releaseFailure)
				releaseLate := make(chan struct{})
				defer close(releaseLate)

				acc := &gatedShardAccumulator{
					Accumulator:         accumulator,
					firstResultReceived: firstReceived,
					releaseFirstResult:  releaseFirst,
				}

				// Use the real fan-out with controlled worker completion and result types.
				var calls atomic.Int64
				downstream := controlledShardDownstreamer{instance: instance{parallelism: tc.queryCount}}
				downstream.query = func(query logql.DownstreamQuery) (logqlmodel.Result, error) {
					calls.Add(1)
					index := int(query.Params.Start().Unix())
					switch index {
					case 1:
						<-firstReceived
					case 2:
						if tc.workerErr != nil {
							<-releaseFailure
							return logqlmodel.Result{}, tc.workerErr
						}
						<-ctx.Done()
						return logqlmodel.Result{}, ctx.Err()
					case 3:
						<-releaseLate // A worker that ignores cancellation and finishes after finalization.
					}

					result, err := ResponseToResult(logResponseWithScanUsage(int64(100 * (index + 1))))
					if index == 0 && tc.returnScalar {
						// Neither accumulator accepts scalars. Reject shard 0 while
						// preserving its usage and shard 1's pending usage separately.
						result.Data = promql.Scalar{}
					}
					return result, err
				}

				// Run collection concurrently so the test can control failure and cancellation.
				evaluator := logql.NewDownstreamEvaluator(downstream)
				var results []logqlmodel.Result
				var queryErr error
				done := make(chan struct{})
				go func() {
					defer close(done)
					results, queryErr = evaluator.Downstream(ctx, queries, acc)
				}()

				// The collector is inside Accumulate for shard 0. Shard 1 has
				// completed and is blocked on the real, unbuffered result channel.
				synctest.Wait()
				require.Equal(t, int64(tc.queryCount), calls.Load())

				// For failure and cancellation, interrupt delivery before allowing
				// the accumulator to finish processing shard 0.
				if tc.workerErr != nil {
					releaseFailure <- struct{}{}
					synctest.Wait()
				}
				if tc.cancelQuery {
					cancel()
					synctest.Wait()
				}

				releaseFirst <- struct{}{}
				<-done

				// Finish the late worker only after finalization to verify the cutoff.
				if tc.cancelQuery {
					releaseLate <- struct{}{}
				}
				synctest.Wait()

				// On success, expect 300 bytes in normal stats and zero in partial
				// stats. On failure, expect the reverse: each shard must be counted once.
				if tc.wantErr == nil && tc.wantErrorContains == "" {
					require.NoError(t, queryErr)
					require.Len(t, results, 1)
					require.Equal(t, int64(tc.limit), results[0].Data.(logqlmodel.Streams).Lines())
					require.Equal(t, int64(300), queryStats.Result(time.Second, 0, 0).Summary.TotalBytesProcessed)
					require.Equal(t, int64(2), queryStats.Result(time.Second, 0, 0).Summary.Shards)
					require.Zero(t, partial.Result().Summary.TotalBytesProcessed)
				} else {
					require.Error(t, queryErr)
					if tc.wantErr != nil {
						require.ErrorIs(t, queryErr, tc.wantErr)
					}
					if tc.wantErrorContains != "" {
						require.ErrorContains(t, queryErr, tc.wantErrorContains)
					}

					require.Nil(t, results)
					require.Equal(t, int64(300), partial.Result().Summary.TotalBytesProcessed, "100 accumulated + 200 completed but discarded; late work excluded")
					require.Equal(t, int64(2), partial.Result().Summary.Shards)
					require.Zero(t, queryStats.Result(time.Second, 0, 0).Summary.TotalBytesProcessed)
				}
			})
		})
	}
}
