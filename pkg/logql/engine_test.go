package logql

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase/definitions"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	json "github.com/json-iterator/go"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

var (
	testSize        = int64(300)
	ErrMock         = errors.New("error")
	ErrMockMultiple = util.MultiError{ErrMock, ErrMock}
)

func TestEngine_checkIntervalLimit(t *testing.T) {
	q := &query{}
	for _, tc := range []struct {
		query  string
		expErr string
	}{
		{query: `rate({app="foo"} [1m])`, expErr: ""},
		{query: `rate({app="foo"} [10m])`, expErr: ""},
		{query: `max(rate({app="foo"} [5m])) - max(rate({app="bar"} [10m]))`, expErr: ""},
		{query: `rate({app="foo"} [5m]) - rate({app="bar"} [15m])`, expErr: "[15m] > [10m]"},
		{query: `rate({app="foo"} [1h])`, expErr: "[1h] > [10m]"},
		{query: `sum(rate({app="foo"} [1h]))`, expErr: "[1h] > [10m]"},
		{query: `sum_over_time({app="foo"} |= "foo" | json | unwrap bar [1h])`, expErr: "[1h] > [10m]"},
		{query: `variants(rate({app="foo"}[5m])) of ({app="foo"}[5m])`, expErr: ""},
		{query: `variants(rate({app="foo"}[1h])) of ({app="foo"}[1h])`, expErr: "[1h] > [10m]"},
	} {
		for _, downstream := range []bool{true, false} {
			t.Run(fmt.Sprintf("%v/downstream=%v", tc.query, downstream), func(t *testing.T) {
				expr := syntax.MustParseExpr(tc.query).(syntax.SampleExpr)
				if downstream {
					// Simulate downstream expression
					expr = &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard:      nil,
							SampleExpr: expr,
						},
						next: nil,
					}
				}
				err := q.checkIntervalLimit(expr, 10*time.Minute)
				if tc.expErr != "" {
					require.ErrorContains(t, err, tc.expErr)
				} else {
					require.NoError(t, err)
				}
			})
		}
	}
}

func TestEngine_LogsRateUnwrap(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		qs        string
		ts        time.Time
		direction logproto.Direction
		limit     uint32

		// an array of data per params will be returned by the querier.
		// This is to cover logql that requires multiple queries.
		data   interface{}
		params interface{}

		expected interface{}
	}{
		{
			`rate({app="foo"} | unwrap foo [30s])`,
			time.Unix(60, 0),
			logproto.FORWARD,
			10,
			// create a stream {app="foo"} with 300 samples starting at 46s and ending at 345s with a constant value of 1
			[][]logproto.Series{
				// 30s range the lower bound of the range is not inclusive only 15 samples will make it 60 included
				{newSeries(testSize, offset(46, constantValue(1)), `{app="foo"}`)},
			},
			[]SelectSampleParams{
				{
					&logproto.SampleQueryRequest{
						Start:    time.Unix(30, 0),
						End:      time.Unix(60, 0),
						Selector: `rate({app="foo"} | unwrap foo[30s])`,
						Plan: &plan.QueryPlan{
							AST: syntax.MustParseExpr(`rate({app="foo"} | unwrap foo[30s])`),
						},
					},
				},
			},
			// there are 15 samples (from 47 to 61) matched from the generated series
			// SUM(n=47, 61, 1) = 15
			// 15 / 30 = 0.5
			promql.Vector{promql.Sample{T: 60 * 1000, F: 0.5, Metric: labels.FromStrings("app", "foo")}},
		},
		{
			`rate({app="foo"} | unwrap foo [30s])`,
			time.Unix(60, 0),
			logproto.FORWARD,
			10,
			// create a stream {app="foo"} with 300 samples starting at 46s and ending at 345s with an increasing value by 1
			[][]logproto.Series{
				// 30s range the lower bound of the range is not inclusive only 15 samples will make it 60 included
				{newSeries(testSize, offset(46, incValue(1)), `{app="foo"}`)},
			},
			[]SelectSampleParams{
				{&logproto.SampleQueryRequest{
					Start:    time.Unix(30, 0),
					End:      time.Unix(60, 0),
					Selector: `rate({app="foo"} | unwrap foo[30s])`,
					Plan: &plan.QueryPlan{
						AST: syntax.MustParseExpr(`rate({app="foo"} | unwrap foo[30s])`),
					},
				}},
			},
			// there are 15 samples (from 47 to 61) matched from the generated series
			// SUM(n=47, 61, n) = (47+48+...+61) = 810
			// 810 / 30 = 27
			promql.Vector{promql.Sample{T: 60 * 1000, F: 27, Metric: labels.FromStrings("app", "foo")}},
		},
		{
			`rate_counter({app="foo"} | unwrap foo [30s])`,
			time.Unix(60, 0),
			logproto.FORWARD,
			10,
			// create a stream {app="foo"} with 300 samples starting at 46s and ending at 345s with a constant value of 1
			[][]logproto.Series{
				// 30s range the lower bound of the range is not inclusive only 15 samples will make it 60 included
				{newSeries(testSize, offset(46, constantValue(1)), `{app="foo"}`)},
			},
			[]SelectSampleParams{
				{&logproto.SampleQueryRequest{
					Start:    time.Unix(30, 0),
					End:      time.Unix(60, 0),
					Selector: `rate_counter({app="foo"} | unwrap foo[30s])`,
					Plan: &plan.QueryPlan{
						AST: syntax.MustParseExpr(`rate_counter({app="foo"} | unwrap foo[30s])`),
					},
				}},
			},
			// there are 15 samples (from 47 to 61) matched from the generated series
			// (1 - 1) / 30 = 0
			promql.Vector{promql.Sample{T: 60 * 1000, F: 0, Metric: labels.FromStrings("app", "foo")}},
		},
		{
			`rate_counter({app="foo"} | unwrap foo [30s])`,
			time.Unix(60, 0),
			logproto.FORWARD,
			10,
			// create a stream {app="foo"} with 300 samples starting at 46s and ending at 345s with an increasing value by 1
			[][]logproto.Series{
				// 30s range the lower bound of the range is not inclusive only 15 samples will make it 60 included
				{newSeries(testSize, offset(46, incValue(1)), `{app="foo"}`)},
			},
			[]SelectSampleParams{
				{&logproto.SampleQueryRequest{Start: time.Unix(30, 0), End: time.Unix(60, 0), Selector: `rate_counter({app="foo"} | unwrap foo[30s])`}},
			},
			// 15 samples match the window (30s, 60s]: t=46..60 with values 47..61, so the
			// counter increases by 14 over a 14s sampled interval (avg 1s between samples).
			// The first sample is 16s past the window start (>> the sample spacing), so the
			// counter start is extrapolated by half an average interval (0.5s); the last
			// sample sits at the window end, so there is no extrapolation there.
			// rate = 14 * (14 + 0.5) / 14 / 30 = 14.5 / 30 = 0.4833
			promql.Vector{promql.Sample{T: 60 * 1000, F: 0.4833333333333334, Metric: labels.FromStrings("app", "foo")}},
		},
	} {
		t.Run(fmt.Sprintf("%s %s", test.qs, test.direction), func(t *testing.T) {
			t.Parallel()

			eng := NewEngine(EngineOpts{}, newQuerierRecorder(t, test.data, test.params), NoLimits, log.NewNopLogger())
			params, err := NewLiteralParams(test.qs, test.ts, test.ts, 0, 0, test.direction, test.limit, nil, nil)
			require.NoError(t, err)
			q := eng.Query(params)
			res, err := q.Exec(user.InjectOrgID(context.Background(), "fake"))
			if expectedError, ok := test.expected.(error); ok {
				assert.Equal(t, expectedError.Error(), err.Error())
			} else {
				if err != nil {
					t.Fatal(err)
				}
				assert.Equal(t, test.expected, res.Data)
			}
		})
	}
}

func TestEngine_RangeQuery(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		qs        string
		start     time.Time
		end       time.Time
		step      time.Duration
		interval  time.Duration
		direction logproto.Direction
		limit     uint32

		// an array of streams per SelectParams will be returned by the querier.
		// This is to cover logql that requires multiple queries.
		data   interface{}
		params interface{}

		expected promql_parser.Value
	}{
		{
			`{app="foo"}`, time.Unix(0, 0), time.Unix(30, 0), time.Second, 0, logproto.FORWARD, 10,
			[][]logproto.Stream{
				{newStream(testSize, identity, `{app="foo"}`)},
			},
			[]SelectLogParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(30, 0), Limit: 10, Selector: `{app="foo"}`}},
			},
			logqlmodel.Streams([]logproto.Stream{newStream(10, identity, `{app="foo"}`)}),
		},
		{
			`{app="food"}`, time.Unix(0, 0), time.Unix(30, 0), 0, 2 * time.Second, logproto.FORWARD, 10,
			[][]logproto.Stream{
				{newStream(testSize, identity, `{app="food"}`)},
			},
			[]SelectLogParams{
				{&logproto.QueryRequest{Direction: logproto.FORWARD, Start: time.Unix(0, 0), End: time.Unix(30, 0), Limit: 10, Selector: `{app="food"}`}},
			},
			logqlmodel.Streams([]logproto.Stream{newIntervalStream(10, 2*time.Second, identity, `{app="food"}`)}),
		},
		{
			`{app="fed"}`, time.Unix(0, 0), time.Unix(30, 0), 0, 2 * time.Second, logproto.BACKWARD, 10,
			[][]logproto.Stream{
				{newBackwardStream(testSize, identity, `{app="fed"}`)},
			},
			[]SelectLogParams{
				{&logproto.QueryRequest{Direction: logproto.BACKWARD, Start: time.Unix(0, 0), End: time.Unix(30, 0), Limit: 10, Selector: `{app="fed"}`}},
			},
			logqlmodel.Streams([]logproto.Stream{newBackwardIntervalStream(testSize, 10, 2*time.Second, identity, `{app="fed"}`)}),
		},
		{
			`{app="bar"} |= "foo" |~ ".+bar"`, time.Unix(0, 0), time.Unix(30, 0), time.Second, 0, logproto.BACKWARD, 30,
			[][]logproto.Stream{
				{newStream(testSize, identity, `{app="bar"}`)},
			},
			[]SelectLogParams{
				{&logproto.QueryRequest{Direction: logproto.BACKWARD, Start: time.Unix(0, 0), End: time.Unix(30, 0), Limit: 30, Selector: `{app="bar"}|="foo"|~".+bar"`}},
			},
			logqlmodel.Streams([]logproto.Stream{newStream(30, identity, `{app="bar"}`)}),
		},
		{
			`{app="barf"} |= "foo" |~ ".+bar"`, time.Unix(0, 0), time.Unix(30, 0), 0, 3 * time.Second, logproto.BACKWARD, 30,
			[][]logproto.Stream{
				{newBackwardStream(testSize, identity, `{app="barf"}`)},
			},
			[]SelectLogParams{
				{&logproto.QueryRequest{Direction: logproto.BACKWARD, Start: time.Unix(0, 0), End: time.Unix(30, 0), Limit: 30, Selector: `{app="barf"}|="foo"|~".+bar"`}},
			},
			logqlmodel.Streams([]logproto.Stream{newBackwardIntervalStream(testSize, 30, 3*time.Second, identity, `{app="barf"}`)}),
		},
	} {
		t.Run(fmt.Sprintf("%s %s", test.qs, test.direction), func(t *testing.T) {
			t.Parallel()

			eng := NewEngine(EngineOpts{}, newQuerierRecorder(t, test.data, test.params), NoLimits, log.NewNopLogger())

			params, err := NewLiteralParams(test.qs, test.start, test.end, test.step, test.interval, test.direction, test.limit, nil, nil)
			require.NoError(t, err)
			q := eng.Query(params)
			res, err := q.Exec(user.InjectOrgID(context.Background(), "fake"))
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, test.expected, res.Data)
		})
	}
}

func TestJoinMultiVariantSampleVector(t *testing.T) {
	t.Parallel()

	now := time.Now()
	expr, err := syntax.ParseExpr(`variants(count_over_time({app="foo"}[1m])) of ({app="foo"}[1m])`)
	require.NoError(t, err)

	instantParams := LiteralParams{
		queryExpr: expr,
		limit:     10,
		start:     now,
		end:       now,
		step:      time.Duration(0),
	}

	rangeParams := LiteralParams{
		queryExpr: expr,
		limit:     10,
		start:     now.Add(-time.Hour),
		end:       now,
		step:      30 * time.Second,
	}

	testCases := []struct {
		name             string
		params           Params
		maxSeries        int
		initialVector    promql.Vector
		stepResults      []StepResult
		expectedResult   promql_parser.Value
		expectedWarnings []string
	}{
		{
			name:      "instant query within limits",
			params:    instantParams,
			maxSeries: 3,
			initialVector: promql.Vector{
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo")},
				{T: 60 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "bar")},
			},
			expectedResult: promql.Vector{
				{T: 60 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "bar")}, //bar comes first alphabetically
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo")},
			},
		},
		{
			name:      "instant query where each variant falls within limits, but aggregate is over limit",
			params:    instantParams,
			maxSeries: 3,
			initialVector: promql.Vector{
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo")},
				{T: 60 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "bar")},
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "1", "app", "foo")},
				{T: 60 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "1", "app", "bar")},
			},
			expectedResult: promql.Vector{
				{T: 60 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "bar")}, //bar comes first alphabetically
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo")},
				{T: 60 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "1", "app", "bar")},
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "1", "app", "foo")},
			},
		},
		{
			name:      "instant query with a variant over the limits",
			params:    instantParams,
			maxSeries: 3,
			initialVector: promql.Vector{
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo")},
				{T: 60 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "bar")},
				{T: 60 * 1000, F: 3, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "baz")},
				{T: 60 * 1000, F: 4, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "qux")},
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "1", "app", "foo")},
				{T: 60 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "1", "app", "bar")},
			},
			expectedResult: promql.Vector{
				{T: 60 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "1", "app", "bar")},
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "1", "app", "foo")},
			},
			expectedWarnings: []string{"maximum of series (3) reached for variant (0)"},
		},
		{
			name:      "range query with multiple steps within limits",
			params:    rangeParams,
			maxSeries: 3,
			initialVector: promql.Vector{
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo")},
			},
			stepResults: []StepResult{
				vectorResult(promql.Vector{
					{T: 90 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo")},
				}),
				vectorResult(promql.Vector{
					{T: 120 * 1000, F: 3, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo")},
				}),
			},
			expectedResult: promql.Matrix{
				promql.Series{
					Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo"),
					Floats: []promql.FPoint{
						{T: 60 * 1000, F: 1},
						{T: 90 * 1000, F: 2},
						{T: 120 * 1000, F: 3},
					},
				},
			},
		},
		{
			name:      "range query with multiple steps within limits per variant, but over the limit in aggregate",
			params:    rangeParams,
			maxSeries: 3,
			initialVector: promql.Vector{
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo")},
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "bar")},
			},
			stepResults: []StepResult{
				vectorResult(promql.Vector{
					{T: 90 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo")},
					{T: 90 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "bar")},
				}),
				vectorResult(promql.Vector{
					{T: 120 * 1000, F: 3, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo")},
					{T: 120 * 1000, F: 3, Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "bar")},
				}),
				vectorResult(promql.Vector{
					{T: 150 * 1000, F: 4, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo")},
					{T: 150 * 1000, F: 4, Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "bar")},
				}),
			},
			expectedResult: promql.Matrix{
				promql.Series{
					Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo"),
					Floats: []promql.FPoint{
						{T: 60 * 1000, F: 1},
						{T: 90 * 1000, F: 2},
						{T: 120 * 1000, F: 3},
						{T: 150 * 1000, F: 4},
					},
				},
				promql.Series{
					Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "bar"),
					Floats: []promql.FPoint{
						{T: 60 * 1000, F: 1},
						{T: 90 * 1000, F: 2},
						{T: 120 * 1000, F: 3},
						{T: 150 * 1000, F: 4},
					},
				},
			},
		},
		{
			name:      "range query with a variant over the limit",
			params:    rangeParams,
			maxSeries: 3,
			initialVector: promql.Vector{
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo")},
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "foo")},
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "bar")},
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "baz")},
				{T: 60 * 1000, F: 1, Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "qux")},
			},
			stepResults: []StepResult{
				vectorResult(promql.Vector{
					{T: 90 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo")},
					{T: 90 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "foo")},
					{T: 90 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "bar")},
					{T: 90 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "baz")},
					{T: 90 * 1000, F: 2, Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "qux")},
				}),
				vectorResult(promql.Vector{
					{T: 120 * 1000, F: 3, Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo")},
					{T: 120 * 1000, F: 3, Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "foo")},
					{T: 120 * 1000, F: 3, Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "bar")},
					{T: 120 * 1000, F: 3, Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "baz")},
					{T: 120 * 1000, F: 3, Metric: labels.FromStrings(constants.VariantLabel, "1", "job", "qux")},
				}),
			},
			expectedResult: promql.Matrix{
				promql.Series{
					Metric: labels.FromStrings(constants.VariantLabel, "0", "app", "foo"),
					Floats: []promql.FPoint{
						{T: 60 * 1000, F: 1},
						{T: 90 * 1000, F: 2},
						{T: 120 * 1000, F: 3},
					},
				},
			},
			expectedWarnings: []string{"maximum of series (3) reached for variant (1)"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			q := &query{
				params: tc.params,
			}

			mockEvaluator := &mockStepEvaluator{
				results: tc.stepResults,
				t:       t,
			}

			metadataCtx, ctx := metadata.NewContext(context.Background())
			result, err := q.JoinMultiVariantSampleVector(ctx, true, vectorResult(tc.initialVector), mockEvaluator, tc.maxSeries)
			require.NoError(t, err)
			require.Equal(t, tc.expectedResult, result)

			if tc.expectedWarnings != nil {
				require.Equal(t, tc.expectedWarnings, metadataCtx.Warnings())
			}
		})
	}
}

// vectorResult is a helper that creates a StepResult from a vector
func vectorResult(v promql.Vector) StepResult {
	return &storeSampleResult{vector: v}
}

// mockStepEvaluator is a mock implementation of StepEvaluator for testing
type mockStepEvaluator struct {
	results []StepResult
	current int
	err     error
	t       *testing.T
}

func (m *mockStepEvaluator) Next() (bool, int64, StepResult) {
	if m.current >= len(m.results) {
		return false, 0, nil
	}
	result := m.results[m.current]
	m.current++
	return true, 0, result
}

func (m *mockStepEvaluator) Error() error {
	return m.err
}

func (m *mockStepEvaluator) Close() error {
	return nil
}

func (m *mockStepEvaluator) Explain(_ Node) {
}

func (m *mockStepEvaluator) SetMaxOutputSeries(int) {}

// storeSampleResult implements StepResult for testing
type storeSampleResult struct {
	vector promql.Vector
}

func (s *storeSampleResult) SampleVector() promql.Vector {
	return s.vector
}

func (s *storeSampleResult) QuantileSketchVec() ProbabilisticQuantileVector {
	return ProbabilisticQuantileVector{}
}

func (s *storeSampleResult) CountMinSketchVec() CountMinSketchVector {
	return CountMinSketchVector{}
}

type statsQuerier struct{}

func (statsQuerier) SelectLogs(ctx context.Context, _ SelectLogParams) (iter.EntryIterator, error) {
	st := stats.FromContext(ctx)
	st.AddDecompressedBytes(1)
	return iter.NoopEntryIterator, nil
}

func (statsQuerier) SelectSamples(ctx context.Context, _ SelectSampleParams) (iter.SampleIterator, error) {
	st := stats.FromContext(ctx)
	st.AddDecompressedBytes(1)
	return iter.NoopSampleIterator, nil
}

func TestEngine_Stats(t *testing.T) {
	eng := NewEngine(EngineOpts{}, &statsQuerier{}, NoLimits, log.NewNopLogger())

	queueTime := 2 * time.Nanosecond

	params, err := NewLiteralParams(`{foo="bar"}`, time.Now(), time.Now(), 0, 0, logproto.FORWARD, 1000, nil, nil)
	require.NoError(t, err)
	q := eng.Query(params)

	ctx := context.WithValue(context.Background(), httpreq.QueryQueueTimeHTTPHeader, queueTime)
	r, err := q.Exec(user.InjectOrgID(ctx, "fake"))
	require.NoError(t, err)
	require.Equal(t, int64(1), r.Statistics.TotalDecompressedBytes())
	require.Equal(t, queueTime.Seconds(), r.Statistics.Summary.QueueTime)
}

type metaQuerier struct{}

func (metaQuerier) SelectLogs(ctx context.Context, _ SelectLogParams) (iter.EntryIterator, error) {
	_ = metadata.JoinHeaders(ctx, []*definitions.PrometheusResponseHeader{
		{
			Name:   "Header",
			Values: []string{"value"},
		},
	})
	return iter.NoopEntryIterator, nil
}

func (metaQuerier) SelectSamples(
	ctx context.Context,
	_ SelectSampleParams,
) (iter.SampleIterator, error) {
	_ = metadata.JoinHeaders(ctx, []*definitions.PrometheusResponseHeader{
		{Name: "Header", Values: []string{"value"}},
	})
	return iter.NoopSampleIterator, nil
}

func TestEngine_Metadata(t *testing.T) {
	eng := NewEngine(EngineOpts{}, &metaQuerier{}, NoLimits, log.NewNopLogger())

	params, err := NewLiteralParams(`{foo="bar"}`, time.Now(), time.Now(), 0, 0, logproto.BACKWARD, 1000, nil, nil)
	require.NoError(t, err)
	q := eng.Query(params)

	r, err := q.Exec(user.InjectOrgID(context.Background(), "fake"))
	require.NoError(t, err)
	require.Equal(t, []*definitions.PrometheusResponseHeader{
		{Name: "Header", Values: []string{"value"}},
	}, r.Headers)
}

func TestEngine_LogsInstantQuery_Vector(t *testing.T) {
	eng := NewEngine(EngineOpts{}, &statsQuerier{}, NoLimits, log.NewNopLogger())
	now := time.Now()
	queueTime := 2 * time.Nanosecond
	logqlVector := `vector(5)`

	params, err := NewLiteralParams(logqlVector, now, now, 0, time.Second*30, logproto.BACKWARD, 1000, nil, nil)
	require.NoError(t, err)
	q := eng.Query(params)
	ctx := context.WithValue(context.Background(), httpreq.QueryQueueTimeHTTPHeader, queueTime)
	_, err = q.Exec(user.InjectOrgID(ctx, "fake"))

	require.NoError(t, err)

	qry, ok := q.(*query)
	require.Equal(t, ok, true)
	vectorExpr := syntax.NewVectorExpr("5")

	data, err := qry.evalSample(ctx, vectorExpr)
	require.NoError(t, err)
	result, ok := data.(promql.Vector)
	require.Equal(t, ok, true)
	require.Equal(t, result[0].F, float64(5))
	require.Equal(t, result[0].T, now.UnixNano()/int64(time.Millisecond))
}

type errorIteratorQuerier struct {
	samples func() []iter.SampleIterator
	entries func() []iter.EntryIterator
}

func (e errorIteratorQuerier) SelectLogs(_ context.Context, p SelectLogParams) (iter.EntryIterator, error) {
	return iter.NewSortEntryIterator(e.entries(), p.Direction), nil
}

func (e errorIteratorQuerier) SelectSamples(_ context.Context, _ SelectSampleParams) (iter.SampleIterator, error) {
	return iter.NewSortSampleIterator(e.samples()), nil
}

func TestMultiVariantQueries_Unsupported(t *testing.T) {
	variantQuery := `variants(bytes_over_time({app="foo"}[1m]), count_over_time({app="foo"}[1m])) of ({app="foo"}[1m])`
	testTime := time.Unix(60, 0)

	eng := NewEngine(EngineOpts{}, &statsQuerier{}, NoLimits, log.NewNopLogger())
	params, err := NewLiteralParams(
		variantQuery,
		testTime,
		testTime,
		0,
		0,
		logproto.BACKWARD,
		0,
		nil,
		nil,
	)
	require.NoError(t, err)

	q := eng.Query(params)
	_, err = q.Exec(user.InjectOrgID(context.Background(), "fake"))
	require.ErrorIs(t, err, logqlmodel.ErrVariantsUnsupported)
}

func TestStepEvaluator_Error(t *testing.T) {
	tests := []struct {
		name    string
		qs      string
		querier Querier
		err     error
	}{
		{
			"rangeAggEvaluator",
			`count_over_time({app="foo"}[1m])`,
			&errorIteratorQuerier{
				samples: func() []iter.SampleIterator {
					return []iter.SampleIterator{
						iter.NewSeriesIterator(newSeries(testSize, identity, `{app="foo"}`)),
						iter.ErrorSampleIterator,
					}
				},
			},
			ErrMock,
		},
		{
			"stream",
			`{app="foo"}`,
			&errorIteratorQuerier{
				entries: func() []iter.EntryIterator {
					return []iter.EntryIterator{
						iter.NewStreamIterator(newStream(testSize, identity, `{app="foo"}`)),
						iter.ErrorEntryIterator,
					}
				},
			},
			ErrMock,
		},
		{
			"binOpStepEvaluator",
			`count_over_time({app="foo"}[1m]) / count_over_time({app="foo"}[1m])`,
			&errorIteratorQuerier{
				samples: func() []iter.SampleIterator {
					return []iter.SampleIterator{
						iter.NewSeriesIterator(newSeries(testSize, identity, `{app="foo"}`)),
						iter.ErrorSampleIterator,
					}
				},
			},
			ErrMockMultiple,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eng := NewEngine(EngineOpts{}, tc.querier, NoLimits, log.NewNopLogger())

			params, err := NewLiteralParams(tc.qs, time.Unix(0, 0), time.Unix(180, 0), 1*time.Second, 0, logproto.BACKWARD, 1, nil, nil)
			require.NoError(t, err)
			q := eng.Query(params)
			_, err = q.Exec(user.InjectOrgID(context.Background(), "fake"))
			require.Equal(t, tc.err, err)
		})
	}
}

func TestEngine_MaxSeries(t *testing.T) {
	eng := NewEngine(EngineOpts{}, getLocalQuerier(100000), &fakeLimits{maxSeries: 1}, log.NewNopLogger())

	for _, test := range []struct {
		qs             string
		direction      logproto.Direction
		expectLimitErr bool
	}{
		{`topk(1,rate(({app=~"foo|bar"})[1m]))`, logproto.FORWARD, true},
		{`{app="foo"}`, logproto.FORWARD, false},
		{`{app="bar"} |= "foo" |~ ".+bar"`, logproto.BACKWARD, false},
		{`rate({app="foo"} |~".+bar" [1m])`, logproto.BACKWARD, true},
		{`rate({app="foo"}[30s])`, logproto.FORWARD, true},
		{`count_over_time({app="foo|bar"} |~".+bar" [1m])`, logproto.BACKWARD, true},
		{`avg(count_over_time({app=~"foo|bar"} |~".+bar" [1m]))`, logproto.FORWARD, false},
	} {
		t.Run(test.qs, func(t *testing.T) {
			params, err := NewLiteralParams(test.qs, time.Unix(0, 0), time.Unix(100000, 0), 60*time.Second, 0, test.direction, 1000, nil, nil)
			require.NoError(t, err)
			q := eng.Query(params)
			_, err = q.Exec(user.InjectOrgID(context.Background(), "fake"))
			if test.expectLimitErr {
				require.NotNil(t, err)
				require.True(t, errors.Is(err, logqlmodel.ErrLimit))
			} else {
				require.Nil(t, err)
			}
		})
	}
}

func TestEngine_MaxRangeInterval(t *testing.T) {
	eng := NewEngine(EngineOpts{}, getLocalQuerier(100000), &fakeLimits{rangeLimit: 24 * time.Hour, maxSeries: 100000}, log.NewNopLogger())

	for _, test := range []struct {
		qs             string
		direction      logproto.Direction
		expectLimitErr bool
	}{
		{`topk(1,rate(({app=~"foo|bar"})[2d]))`, logproto.FORWARD, true},
		{`topk(1,rate(({app=~"foo|bar"})[1d]))`, logproto.FORWARD, false},
		{`topk(1,rate({app=~"foo|bar"}[12h]) / (rate({app="baz"}[23h]) + rate({app="fiz"}[25h])))`, logproto.FORWARD, true},
	} {
		t.Run(test.qs, func(t *testing.T) {
			params, err := NewLiteralParams(test.qs, time.Unix(0, 0), time.Unix(100000, 0), 60*time.Second, 0, test.direction, 1000, nil, nil)
			require.NoError(t, err)
			q := eng.Query(params)

			_, err = q.Exec(user.InjectOrgID(context.Background(), "fake"))
			if test.expectLimitErr {
				require.Error(t, err)
				require.ErrorIs(t, err, logqlmodel.ErrIntervalLimit)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// go test -mod=vendor ./pkg/logql/ -bench=.  -benchmem -memprofile memprofile.out -cpuprofile cpuprofile.out
func BenchmarkRangeQuery100000(b *testing.B) {
	benchmarkRangeQuery(int64(100000), b)
}

func BenchmarkRangeQuery200000(b *testing.B) {
	benchmarkRangeQuery(int64(200000), b)
}

func BenchmarkRangeQuery500000(b *testing.B) {
	benchmarkRangeQuery(int64(500000), b)
}

func BenchmarkRangeQuery1000000(b *testing.B) {
	benchmarkRangeQuery(int64(1000000), b)
}

var result promql_parser.Value

func benchmarkRangeQuery(testsize int64, b *testing.B) {
	b.ReportAllocs()
	eng := NewEngine(EngineOpts{}, getLocalQuerier(testsize), NoLimits, log.NewNopLogger())
	start := time.Unix(0, 0)
	end := time.Unix(testsize, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, test := range []struct {
			qs        string
			direction logproto.Direction
		}{
			{`{app="foo"}`, logproto.FORWARD},
			{`{app="bar"} |= "foo" |~ ".+bar"`, logproto.BACKWARD},
			{`rate({app="foo"} |~".+bar" [1m])`, logproto.BACKWARD},
			{`rate({app="foo"}[30s])`, logproto.FORWARD},
			{`count_over_time({app="foo"} |~".+bar" [1m])`, logproto.BACKWARD},
			{`count_over_time(({app="foo"} |~".+bar")[5m])`, logproto.BACKWARD},
			{`avg(count_over_time({app=~"foo|bar"} |~".+bar" [1m]))`, logproto.FORWARD},
			{`min(rate({app=~"foo|bar"} |~".+bar" [1m]))`, logproto.FORWARD},
			{`max by (app) (rate({app=~"foo|bar"} |~".+bar" [1m]))`, logproto.FORWARD},
			{`max(rate({app=~"foo|bar"} |~".+bar" [1m]))`, logproto.FORWARD},
			{`sum(rate({app=~"foo|bar"} |~".+bar" [1m]))`, logproto.FORWARD},
			{`sum(count_over_time({app=~"foo|bar"} |~".+bar" [1m])) by (app)`, logproto.FORWARD},
			{`count(count_over_time({app=~"foo|bar"} |~".+bar" [1m])) without (app)`, logproto.FORWARD},
			{`stdvar without (app) (count_over_time(({app=~"foo|bar"} |~".+bar")[1m])) `, logproto.FORWARD},
			{`stddev(count_over_time(({app=~"foo|bar"} |~".+bar")[1m])) `, logproto.FORWARD},
			{`rate(({app=~"foo|bar"} |~".+bar")[1m])`, logproto.FORWARD},
			{`topk(2,rate(({app=~"foo|bar"} |~".+bar")[1m]))`, logproto.FORWARD},
			{`topk(1,rate(({app=~"foo|bar"} |~".+bar")[1m]))`, logproto.FORWARD},
			{`topk(1,rate(({app=~"foo|bar"} |~".+bar")[1m])) by (app)`, logproto.FORWARD},
			{`bottomk(2,rate(({app=~"foo|bar"} |~".+bar")[1m]))`, logproto.FORWARD},
			{`bottomk(3,rate(({app=~"foo|bar"} |~".+bar")[1m])) without (app)`, logproto.FORWARD},
		} {
			params, err := NewLiteralParams(test.qs, start, end, 60*time.Second, 0, logproto.BACKWARD, 1000, nil, nil)
			require.NoError(b, err)
			q := eng.Query(params)

			res, err := q.Exec(user.InjectOrgID(context.Background(), "fake"))
			if err != nil {
				b.Fatal(err)
			}
			result = res.Data
			if result == nil {
				b.Fatal("unexpected nil result")
			}
		}
	}
}

// TestHashingStability tests logging stability between engine and RecordRangeAndInstantQueryMetrics methods.
func TestHashingStability(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "fake")
	params := LiteralParams{
		start:     time.Unix(0, 0),
		end:       time.Unix(5, 0),
		step:      60 * time.Second,
		direction: logproto.FORWARD,
		limit:     1000,
	}

	queryWithEngine := func() string {
		buf := bytes.NewBufferString("")
		logger := log.NewLogfmtLogger(buf)
		eng := NewEngine(EngineOpts{LogExecutingQuery: true}, getLocalQuerier(4), NoLimits, logger)

		parsed, err := syntax.ParseExpr(params.QueryString())
		require.NoError(t, err)
		params.queryExpr = parsed

		query := eng.Query(params)
		_, err = query.Exec(ctx)
		require.NoError(t, err)
		return buf.String()
	}

	queryDirectly := func() string {
		statsResult := stats.Result{
			Summary: stats.Summary{
				BytesProcessedPerSecond: 100000,
				QueueTime:               0.000000002,
				ExecTime:                25.25,
				TotalBytesProcessed:     100000,
				TotalEntriesReturned:    10,
			},
		}
		buf := bytes.NewBufferString("")
		logger := log.NewLogfmtLogger(buf)
		RecordRangeAndInstantQueryMetrics(ctx, logger, params, "200", statsResult, logqlmodel.Streams{logproto.Stream{Entries: make([]logproto.Entry, 10)}})
		return buf.String()
	}

	for _, test := range []struct {
		qs string
	}{
		{`sum by(query_hash) (count_over_time({app="myapp",env="myenv"} |= "error" |= "metrics.go" | logfmt [10s]))`},
		{`sum (count_over_time({app="myapp",env="myenv"} |= "error" |= "metrics.go" | logfmt [10s])) by(query_hash)`},
	} {
		params.queryString = test.qs
		expectedQueryHash := util.HashedQuery(test.qs)

		// check that both places will end up having the same query hash, even though they're emitting different log lines.
		withEngine := queryWithEngine()
		require.Contains(t, withEngine, fmt.Sprintf("query_hash=%d", expectedQueryHash))
		require.Contains(t, withEngine, "step=1m0s")

		directly := queryDirectly()
		require.Contains(t, directly, fmt.Sprintf("query_hash=%d", expectedQueryHash))
		require.Contains(t, directly, "length=5s")
		require.Contains(t, directly, "latency=slow")
	}
}

func TestUnexpectedEmptyResults(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "fake")

	mock := &mockEvaluatorFactory{
		SampleEvaluatorFunc(
			func(context.Context, SampleEvaluatorFactory, syntax.SampleExpr, Params) (StepEvaluator, error) {
				return EmptyEvaluator[SampleVector]{value: nil}, nil
			},
		),
		VariantsEvaluatorFunc(
			func(context.Context, syntax.VariantsExpr, Params) (StepEvaluator, error) {
				return EmptyEvaluator[SampleVector]{value: nil}, nil
			},
		),
	}

	eng := NewEngine(EngineOpts{}, nil, NoLimits, log.NewNopLogger())
	params, err := NewLiteralParams(`first_over_time({a=~".+"} | logfmt | unwrap value [1s])`, time.Now(), time.Now(), 0, 0, logproto.BACKWARD, 0, nil, nil)
	require.NoError(t, err)
	q := eng.Query(params).(*query)
	q.evaluator = mock

	_, err = q.Exec(ctx)
	require.Error(t, err)
}

type mockEvaluatorFactory struct {
	sampleEvalFunc  SampleEvaluatorFunc
	variantEvalFunc VariantsEvaluatorFunc
}

func (m *mockEvaluatorFactory) NewStepEvaluator(ctx context.Context, nextEvaluatorFactory SampleEvaluatorFactory, expr syntax.SampleExpr, p Params) (StepEvaluator, error) {
	if m.sampleEvalFunc != nil {
		return m.sampleEvalFunc(ctx, nextEvaluatorFactory, expr, p)
	}
	return nil, errors.New("unimplemented mock SampleEvaluatorFactory")
}

func (m *mockEvaluatorFactory) NewVariantsStepEvaluator(ctx context.Context, expr syntax.VariantsExpr, p Params) (StepEvaluator, error) {
	if m.variantEvalFunc != nil {
		return m.variantEvalFunc(ctx, expr, p)
	}
	return nil, errors.New("unimplemented mock VariantEvaluatorFactory")
}

func (m *mockEvaluatorFactory) NewIterator(context.Context, syntax.LogSelectorExpr, Params) (iter.EntryIterator, error) {
	return nil, errors.New("unimplemented mock EntryEvaluatorFactory")
}

func getLocalQuerier(size int64) Querier {
	return &querierRecorder{
		series: map[string][]logproto.Series{
			"": {
				newSeries(size, identity, `{app="foo"}`),
				newSeries(size, identity, `{app="foo",bar="foo"}`),
				newSeries(size, identity, `{app="foo",bar="bazz"}`),
				newSeries(size, identity, `{app="foo",bar="fuzz"}`),
				newSeries(size, identity, `{app="bar"}`),
				newSeries(size, identity, `{app="bar",bar="foo"}`),
				newSeries(size, identity, `{app="bar",bar="bazz"}`),
				newSeries(size, identity, `{app="bar",bar="fuzz"}`),
			},
		},
		streams: map[string][]logproto.Stream{
			"": {
				newStream(size, identity, `{app="foo"}`),
				newStream(size, identity, `{app="foo",bar="foo"}`),
				newStream(size, identity, `{app="foo",bar="bazz"}`),
				newStream(size, identity, `{app="foo",bar="fuzz"}`),
				newStream(size, identity, `{app="bar"}`),
				newStream(size, identity, `{app="bar",bar="foo"}`),
				newStream(size, identity, `{app="bar",bar="bazz"}`),
				newStream(size, identity, `{app="bar",bar="fuzz"}`),
			},
		},
	}
}

type querierRecorder struct {
	streams map[string][]logproto.Stream
	series  map[string][]logproto.Series
	match   bool
}

func newQuerierRecorder(t *testing.T, data interface{}, params interface{}) *querierRecorder {
	t.Helper()
	streams := map[string][]logproto.Stream{}
	if streamsIn, ok := data.([][]logproto.Stream); ok {
		if paramsIn, ok2 := params.([]SelectLogParams); ok2 {
			for i, p := range paramsIn {
				p.Plan = &plan.QueryPlan{
					AST: syntax.MustParseExpr(p.Selector),
				}
				streams[paramsID(p)] = streamsIn[i]
			}
		}
	}

	series := map[string][]logproto.Series{}
	if seriesIn, ok := data.([][]logproto.Series); ok {
		if paramsIn, ok2 := params.([]SelectSampleParams); ok2 {
			for i, p := range paramsIn {
				expr, ok3 := syntax.MustParseExpr(p.Selector).(syntax.VariantsExpr)
				if ok3 {
					if p.Plan == nil {
						p.Plan = &plan.QueryPlan{
							AST: expr,
						}
					}

					curSeries := seriesIn[i]
					variants := expr.Variants()
					newSeries := make([]logproto.Series, len(curSeries)*len(variants))

					for vi := range variants {
						for si, s := range curSeries {
							lbls, err := promql_parser.NewParser(promql_parser.Options{}).ParseMetric(s.Labels)
							if err != nil {
								return nil
							}

							// Add variant label
							b := labels.NewBuilder(lbls)
							b.Set(constants.VariantLabel, fmt.Sprintf("%d", vi))
							lbls = b.Labels()

							// Copy series with new labels
							idx := vi*len(curSeries) + si
							newSeries[idx] = logproto.Series{
								Labels:  lbls.String(),
								Samples: s.Samples,
							}
						}
					}
					series[paramsID(p)] = newSeries
				} else {
					for i, p := range paramsIn {
						if p.Plan == nil {
							p.Plan = &plan.QueryPlan{
								AST: syntax.MustParseExpr(p.Selector),
							}
						}
						series[paramsID(p)] = seriesIn[i]
					}
				}
			}
		}
	}

	return &querierRecorder{
		streams: streams,
		series:  series,
		match:   true,
	}
}

func (q *querierRecorder) SelectLogs(_ context.Context, p SelectLogParams) (iter.EntryIterator, error) {
	if !q.match {
		for _, s := range q.streams {
			return iter.NewStreamsIterator(s, p.Direction), nil
		}
	}
	recordID := paramsID(p)
	streams, ok := q.streams[recordID]
	if !ok {
		return nil, fmt.Errorf("no streams found for id: %s has: %+v", recordID, q.streams)
	}
	return iter.NewStreamsIterator(streams, p.Direction), nil
}

func (q *querierRecorder) SelectSamples(
	_ context.Context,
	p SelectSampleParams,
) (iter.SampleIterator, error) {
	if !q.match {
		for _, s := range q.series {
			return iter.NewMultiSeriesIterator(s), nil
		}
	}
	recordID := paramsID(p)
	if len(q.series) == 0 {
		return iter.NoopSampleIterator, nil
	}
	series, ok := q.series[recordID]
	if !ok {
		return nil, fmt.Errorf("no series found for id: %s has: %+v", recordID, q.series)
	}
	return iter.NewMultiSeriesIterator(series), nil
}

func paramsID(p interface{}) string {
	switch params := p.(type) {
	case SelectLogParams:
	case SelectSampleParams:
		return fmt.Sprintf("%d", params.Plan.Hash())
	}
	b, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	return strings.ReplaceAll(string(b), " ", "")
}

type logData struct {
	logproto.Entry
	// nolint
	logproto.Sample
}

type generator func(i int64) logData

func newStream(n int64, f generator, lbsString string) logproto.Stream {
	labels, err := syntax.ParseLabels(lbsString)
	if err != nil {
		panic(err)
	}
	entries := []logproto.Entry{}
	for i := int64(0); i < n; i++ {
		entries = append(entries, f(i).Entry)
	}
	return logproto.Stream{
		Entries: entries,
		Labels:  labels.String(),
	}
}

func newSeries(n int64, f generator, lbsString string) logproto.Series {
	labels, err := syntax.ParseLabels(lbsString)
	if err != nil {
		panic(err)
	}
	samples := []logproto.Sample{}
	for i := int64(0); i < n; i++ {
		samples = append(samples, f(i).Sample)
	}
	return logproto.Series{
		Samples: samples,
		Labels:  labels.String(),
	}
}

func newIntervalStream(n int64, step time.Duration, f generator, labels string) logproto.Stream {
	entries := []logproto.Entry{}
	lastEntry := int64(-100) // Start with a really small value (negative) so we always output the first item
	for i := int64(0); int64(len(entries)) < n; i++ {
		if float64(lastEntry)+step.Seconds() <= float64(i) {
			entries = append(entries, f(i).Entry)
			lastEntry = i
		}
	}
	return logproto.Stream{
		Entries: entries,
		Labels:  labels,
	}
}

func newBackwardStream(n int64, f generator, labels string) logproto.Stream {
	entries := []logproto.Entry{}
	for i := n - 1; i > 0; i-- {
		entries = append(entries, f(i).Entry)
	}
	return logproto.Stream{
		Entries: entries,
		Labels:  labels,
	}
}

func newBackwardIntervalStream(n, expectedResults int64, step time.Duration, f generator, labels string) logproto.Stream {
	entries := []logproto.Entry{}
	lastEntry := int64(100000) // Start with some really big value so that we always output the first item
	for i := n - 1; int64(len(entries)) < expectedResults; i-- {
		if float64(lastEntry)-step.Seconds() >= float64(i) {
			entries = append(entries, f(i).Entry)
			lastEntry = i
		}
	}
	return logproto.Stream{
		Entries: entries,
		Labels:  labels,
	}
}

func identity(i int64) logData {
	return logData{
		Entry: logproto.Entry{
			Timestamp: time.Unix(i, 0),
			Line:      fmt.Sprintf("%d", i),
		},
		Sample: logproto.Sample{
			Timestamp: time.Unix(i, 0).UnixNano(),
			Value:     1.,
			Hash:      uint64(i),
		},
	}
}

// nolint
func factor(j int64, g generator) generator {
	return func(i int64) logData {
		return g(i * j)
	}
}

// nolint
func offset(j int64, g generator) generator {
	return func(i int64) logData {
		return g(i + j)
	}
}

// nolint
func constant(t int64) generator {
	return func(i int64) logData {
		return logData{
			Entry: logproto.Entry{
				Timestamp: time.Unix(t, 0),
				Line:      fmt.Sprintf("%d", i),
			},
			Sample: logproto.Sample{
				Timestamp: time.Unix(t, 0).UnixNano(),
				Hash:      uint64(i),
				Value:     1.0,
			},
		}
	}
}

// nolint
func constantValue(t int64) generator {
	return func(i int64) logData {
		return logData{
			Entry: logproto.Entry{
				Timestamp: time.Unix(i, 0),
				Line:      fmt.Sprintf("%d", i),
			},
			Sample: logproto.Sample{
				Timestamp: time.Unix(i, 0).UnixNano(),
				Hash:      uint64(i),
				Value:     float64(t),
			},
		}
	}
}

// nolint
func incValue(val int64) generator {
	return func(i int64) logData {
		return logData{
			Entry: logproto.Entry{
				Timestamp: time.Unix(i, 0),
				Line:      fmt.Sprintf("%d", i),
			},
			Sample: logproto.Sample{
				Timestamp: time.Unix(i, 0).UnixNano(),
				Hash:      uint64(i),
				Value:     float64(val + i),
			},
		}
	}
}

func TestJoinSampleVector_LogsDrilldownBehavior(t *testing.T) {
	t.Parallel()

	// Test the JoinSampleVector method directly to test both code paths
	tests := []struct {
		name               string
		queryTags          string
		maxSeries          int
		vectorSize         int // Number of series in the vector to test immediate limit check
		isRangeQuery       bool
		additionalVectors  []int // Additional vectors for range query testing
		expectError        bool
		expectTruncation   bool
		expectedWarningMsg string
	}{
		{
			name:               "Drilldown - immediate limit exceeded in first vector",
			queryTags:          "Source=grafana-lokiexplore-app",
			maxSeries:          2,
			vectorSize:         3,
			isRangeQuery:       false,
			expectError:        false,
			expectTruncation:   true,
			expectedWarningMsg: "maximum number of series (2) reached for a single query; returning partial results",
		},
		{
			name:               "Non-drilldown - immediate limit exceeded in first vector",
			queryTags:          "Source=grafana",
			maxSeries:          2,
			vectorSize:         3,
			isRangeQuery:       false,
			expectError:        true,
			expectTruncation:   false,
			expectedWarningMsg: "",
		},
		{
			name:               "Drilldown - limit NOT exceeded",
			queryTags:          "Source=grafana-lokiexplore-app",
			maxSeries:          5,
			vectorSize:         3,
			isRangeQuery:       false,
			expectError:        false,
			expectTruncation:   false,
			expectedWarningMsg: "",
		},
		{
			name:               "Non-drilldown - limit NOT exceeded",
			queryTags:          "Source=grafana",
			maxSeries:          5,
			vectorSize:         3,
			isRangeQuery:       false,
			expectError:        false,
			expectTruncation:   false,
			expectedWarningMsg: "",
		},
		{
			name:               "Drilldown - range query limit exceeded in second vector",
			queryTags:          "Source=grafana-lokiexplore-app",
			maxSeries:          3,
			vectorSize:         2, // First vector has 2 series
			isRangeQuery:       true,
			additionalVectors:  []int{2}, // Second vector has 2 more unique series (total 4 > limit 3)
			expectError:        false,
			expectTruncation:   true,
			expectedWarningMsg: "maximum number of series (3) reached for a single query; returning partial results",
		},
		{
			name:               "Non-drilldown - range query limit exceeded in second vector",
			queryTags:          "Source=grafana",
			maxSeries:          3,
			vectorSize:         2, // First vector has 2 series
			isRangeQuery:       true,
			additionalVectors:  []int{2}, // Second vector has 2 more unique series (total 4 > limit 3)
			expectError:        true,
			expectTruncation:   false,
			expectedWarningMsg: "",
		},
		{
			name:               "Drilldown - range query limit NOT exceeded across multiple vectors",
			queryTags:          "Source=grafana-lokiexplore-app",
			maxSeries:          5,
			vectorSize:         2, // First vector has 2 series
			isRangeQuery:       true,
			additionalVectors:  []int{2, 1}, // Second has 2, third has 1 (total 5 = limit)
			expectError:        false,
			expectTruncation:   false,
			expectedWarningMsg: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Create a mock query with the necessary context
			ctx := context.Background()
			if test.queryTags != "" {
				ctx = httpreq.InjectQueryTags(ctx, test.queryTags)
			}
			_, ctx = metadata.NewContext(ctx)

			// Create mock params - adjust for range vs instant query
			var params *LiteralParams
			if test.isRangeQuery {
				params = &LiteralParams{
					queryString: `rate({app="foo"}[1m])`,
					start:       time.Unix(0, 0),
					end:         time.Unix(120, 0), // Range query: multiple steps
					step:        60 * time.Second,
					interval:    0,
					direction:   logproto.FORWARD,
					limit:       100,
				}
			} else {
				params = &LiteralParams{
					queryString: `rate({app="foo"}[1m])`,
					start:       time.Unix(0, 0),
					end:         time.Unix(60, 0), // Instant query: single step
					step:        30 * time.Second,
					interval:    0,
					direction:   logproto.FORWARD,
					limit:       100,
				}
			}

			q := &query{
				params: params,
			}

			// Create the initial vector with the specified number of series
			vec := make(promql.Vector, test.vectorSize)
			for i := 0; i < test.vectorSize; i++ {
				vec[i] = promql.Sample{
					T:      60 * 1000,
					F:      float64(i + 1),
					Metric: labels.FromStrings("app", fmt.Sprintf("app%d", i)),
				}
			}

			// Create additional vectors for range query testing
			var stepResults []StepResult
			if test.isRangeQuery && len(test.additionalVectors) > 0 {
				seriesOffset := test.vectorSize // Start naming series after the initial vector
				for _, additionalSize := range test.additionalVectors {
					additionalVec := make(promql.Vector, additionalSize)
					for i := 0; i < additionalSize; i++ {
						additionalVec[i] = promql.Sample{
							T:      120 * 1000, // Different timestamp for subsequent steps
							F:      float64(seriesOffset + i + 1),
							Metric: labels.FromStrings("app", fmt.Sprintf("app%d", seriesOffset+i)),
						}
					}
					stepResults = append(stepResults, &storeSampleResult{vector: additionalVec})
					seriesOffset += additionalSize
				}
			}

			// Create a mock step evaluator
			stepEvaluator := &mockStepEvaluator{
				results: stepResults,
				current: 0,
				t:       t,
			}

			// Call JoinSampleVector with context
			result, err := q.JoinSampleVector(ctx, true, &storeSampleResult{vector: vec}, stepEvaluator, test.maxSeries, false)

			if test.expectError {
				require.Error(t, err)
				require.True(t, errors.Is(err, logqlmodel.ErrLimit))
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)

				if test.expectTruncation {
					// Check that the result was truncated to maxSeries
					var actualSeriesCount int
					if vec, ok := result.(promql.Vector); ok {
						// Instant query result
						actualSeriesCount = len(vec)
					} else if matrix, ok := result.(promql.Matrix); ok {
						// Range query result - count unique series
						seriesMap := make(map[string]bool)
						for _, series := range matrix {
							seriesMap[series.Metric.String()] = true
						}
						actualSeriesCount = len(seriesMap)
					} else {
						t.Fatalf("Unexpected result type: %T", result)
					}

					require.LessOrEqual(t, actualSeriesCount, test.maxSeries,
						"Expected result to be truncated to maxSeries (%d), but got %d series",
						test.maxSeries, actualSeriesCount)

					// Check for warning
					meta := metadata.FromContext(ctx)
					warnings := meta.Warnings()
					require.NotEmpty(t, warnings, "Expected warnings but got none")
					require.Contains(t, warnings[0], test.expectedWarningMsg)
				} else {
					// No truncation expected - verify no warnings
					meta := metadata.FromContext(ctx)
					warnings := meta.Warnings()
					if test.expectedWarningMsg == "" {
						require.Empty(t, warnings, "Expected no warnings but got: %v", warnings)
					}
				}
			}
		})
	}
}

func TestHttpreqIsLogsDrilldownRequest(t *testing.T) {
	tests := []struct {
		name      string
		queryTags string
		expected  bool
	}{
		{
			name:      "Valid Logs Drilldown request",
			queryTags: "Source=grafana-lokiexplore-app,Feature=patterns",
			expected:  true,
		},
		{
			name:      "Case insensitive source matching",
			queryTags: "Source=GRAFANA-LOKIEXPLORE-APP,Feature=patterns",
			expected:  true,
		},
		{
			name:      "Different source",
			queryTags: "Source=grafana,Feature=explore",
			expected:  false,
		},
		{
			name:      "No source tag",
			queryTags: "Feature=patterns,User=test",
			expected:  false,
		},
		{
			name:      "Empty query tags",
			queryTags: "",
			expected:  false,
		},
		{
			name:      "Malformed tags",
			queryTags: "invalid_tags_format",
			expected:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.queryTags != "" {
				ctx = httpreq.InjectQueryTags(ctx, test.queryTags)
			}

			result := httpreq.IsLogsDrilldownRequest(ctx)
			require.Equal(t, test.expected, result, "Expected %v, got %v for queryTags: %s", test.expected, result, test.queryTags)
		})
	}
}

func TestJoinSampleVector_RangeQueryVectorOverwrite(t *testing.T) {
	t.Parallel()

	// This test covers a vector overwrite issue in range queries for Logs Drilldown.
	// The problem was that after truncating the first vector due to series limit,
	// subsequent steps in the range query can overwrite the truncated vector with larger vectors,
	// causing the final result to exceed the intended series limit.

	ctx := context.Background()
	ctx = httpreq.InjectQueryTags(ctx, "Source=grafana-lokiexplore-app")
	_, ctx = metadata.NewContext(ctx)

	// Create mock params for a range query (multiple steps)
	params := &LiteralParams{
		queryString: `rate({app="foo"}[1m])`,
		start:       time.Unix(0, 0),
		end:         time.Unix(120, 0), // 3 steps with 60s step
		step:        60 * time.Second,
		interval:    0,
		direction:   logproto.FORWARD,
		limit:       100,
	}

	q := &query{
		params: params,
	}

	maxSeries := 2 // Limit to 2 series

	// Create first vector that exceeds the limit (3 series)
	firstVec := make(promql.Vector, 3)
	for i := range 3 {
		firstVec[i] = promql.Sample{
			T:      0 * 1000, // First time step
			F:      float64(i + 1),
			Metric: labels.FromStrings("app", fmt.Sprintf("app%d", i)),
		}
	}

	// Create second vector that also exceeds the limit (4 series)
	// This simulates the case where subsequent steps return even more series
	secondVec := make(promql.Vector, 4)
	for i := range 4 {
		secondVec[i] = promql.Sample{
			T:      60 * 1000, // Second time step
			F:      float64(i + 10),
			Metric: labels.FromStrings("app", fmt.Sprintf("app%d", i)),
		}
	}

	// Create third vector that also exceeds the limit (5 series)
	thirdVec := make(promql.Vector, 5)
	for i := range 5 {
		thirdVec[i] = promql.Sample{
			T:      120 * 1000, // Third time step
			F:      float64(i + 20),
			Metric: labels.FromStrings("app", fmt.Sprintf("app%d", i)),
		}
	}

	// Create a mock step evaluator that returns vectors exceeding the limit on each call
	stepEvaluator := &mockStepEvaluator{
		results: []StepResult{
			&storeSampleResult{vector: secondVec}, // Second call will return 4 series
			&storeSampleResult{vector: thirdVec},  // Third call will return 5 series
		},
		current: 0,
		t:       t,
	}

	// Call JoinSampleVector with the first vector (3 series) and step evaluator
	// that will return even larger vectors in subsequent steps
	result, err := q.JoinSampleVector(ctx, true, &storeSampleResult{vector: firstVec}, stepEvaluator, maxSeries, false)

	require.NoError(t, err)
	require.NotNil(t, result)

	// This test expects the CORRECT behavior: series limit should be respected
	// across all steps of a range query for Logs Drilldown requests
	if matrix, ok := result.(promql.Matrix); ok {
		// Count total unique series across all steps
		seriesMap := make(map[string]bool)
		for _, series := range matrix {
			seriesMap[series.Metric.String()] = true
		}

		// The correct behavior: final result should never exceed maxSeries
		// This assertion will FAIL initially, demonstrating the bug exists
		require.LessOrEqual(t, len(seriesMap), maxSeries,
			"Expected series limit to be respected across all range query steps. "+
				"Found %d series but limit is %d. This indicates the vector overwrite bug exists.",
			len(seriesMap), maxSeries)

		t.Logf("Correct behavior: found %d unique series (within limit of %d)", len(seriesMap), maxSeries)
	} else {
		t.Fatalf("Expected Matrix result, got %T", result)
	}

	// Verify that warnings were still added for the first truncation
	meta := metadata.FromContext(ctx)
	warnings := meta.Warnings()
	require.NotEmpty(t, warnings, "Expected warnings due to series limit exceeded")
	require.Contains(t, warnings[0], "maximum number of series")
}
