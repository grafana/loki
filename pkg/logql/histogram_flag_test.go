package logql

import (
	"context"
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// histogramSelectCountingQuerier counts the calls that select samples. A query that is refused must not make any.
type histogramSelectCountingQuerier struct {
	Querier
	reads int
}

func (q *histogramSelectCountingQuerier) SelectSamples(ctx context.Context, p SelectSampleParams) (iter.SampleIterator, error) {
	q.reads++
	return q.Querier.SelectSamples(ctx, p)
}

// histogramFlagStreams has a line with a value, so that a query that is not refused has something to read.
var histogramFlagStreams = []logproto.Stream{{
	Labels:  `{app="a"}`,
	Entries: []logproto.Entry{{Timestamp: time.Unix(10, 0), Line: "duration=1"}},
}}

const (
	histogramFlagRange = `{app="a"} | logfmt | unwrap duration [1m]`
	histogramFlagName  = "querier.engine.enable-histograms"
)

// The query shapes that use histograms. The last ones are not queries that validation accepts, and a caller
// that does not validate, or a plan from another component, can still give them to the engine.
var histogramQueries = []struct {
	name      string
	query     string
	unchecked bool
}{
	{name: "quantile of a sum", query: `histogram_quantile(0.9, sum by (a) (histogram_over_time(` + histogramFlagRange + `)))`},
	{name: "count", query: `histogram_count(histogram_over_time(` + histogramFlagRange + `))`},
	{name: "custom buckets", query: `histogram_fraction(0, 2, histogram_over_time(buckets(1, 2), ` + histogramFlagRange + `) by (a))`},
	{name: "in arithmetic", query: `histogram_sum(histogram_over_time(` + histogramFlagRange + `)) + 1`},
	// The range aggregation after the histogram must not hide it, see UsesHistograms.
	{name: "before a range aggregation", query: `histogram_count(histogram_over_time(` + histogramFlagRange + `)) + rate({app="a"}[1m])`},
	{name: "a sum at the root", query: `sum by (a) (histogram_over_time(` + histogramFlagRange + `))`, unchecked: true},
	{name: "a range aggregation at the root", query: `histogram_over_time(` + histogramFlagRange + `)`, unchecked: true},
	{name: "a reader of something that is not a histogram", query: `histogram_count(rate({app="a"}[1m]))`, unchecked: true},
}

func histogramFlagTestParams(t *testing.T, query string, unchecked, instant bool) Params {
	t.Helper()
	start, end, step := time.Unix(60, 0), time.Unix(120, 0), time.Minute
	if instant {
		start, end, step = time.Unix(60, 0), time.Unix(60, 0), 0
	}
	if !unchecked {
		params, err := NewLiteralParams(query, start, end, step, 0, logproto.FORWARD, 100, nil, nil)
		require.NoError(t, err)
		return params
	}
	expr, err := syntax.ParseExprWithoutValidation(query)
	require.NoError(t, err)
	params, err := NewLiteralParams(`count_over_time({app="a"}[1m])`, start, end, step, 0, logproto.FORWARD, 100, nil, nil)
	require.NoError(t, err)
	return ParamsWithExpressionOverride{Params: params, ExpressionOverride: expr}
}

func TestEngine_HistogramsAreOffByDefault(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "fake")

	for _, tc := range histogramQueries {
		for name, instant := range map[string]bool{"range": false, "instant": true} {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				querier := &histogramSelectCountingQuerier{Querier: NewMockQuerier(0, histogramFlagStreams)}
				eng := NewEngine(EngineOpts{}, querier, NoLimits, log.NewNopLogger())

				_, err := eng.Query(histogramFlagTestParams(t, tc.query, tc.unchecked, instant)).Exec(ctx)
				require.Error(t, err)
				// It is a client error, so the query frontend does not retry it, and it says what to do.
				require.ErrorIs(t, err, logqlerr.ErrParse)
				require.ErrorContains(t, err, "-"+histogramFlagName)
				require.Zero(t, querier.reads, "a query that is refused does not read any sample")
			})
		}
	}
}

// With the flag on, a query that uses histograms is not refused for it: it goes on to the evaluator. The
// evaluator does not run the histogram functions yet, so it is what returns the error here.
func TestEngine_HistogramsReachTheEvaluator(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "fake")
	eng := NewEngine(EngineOpts{EnableHistograms: true}, NewMockQuerier(0, histogramFlagStreams), NoLimits, log.NewNopLogger())

	for _, tc := range histogramQueries {
		if tc.unchecked {
			continue // the evaluator does not have to handle what a query cannot be.
		}
		t.Run(tc.name, func(t *testing.T) {
			_, err := eng.Query(histogramFlagTestParams(t, tc.query, false, true)).Exec(ctx)
			require.ErrorContains(t, err, "histogram functions are not supported yet")
			require.NotContains(t, err.Error(), histogramFlagName)
		})
	}
}

// The flag is about histograms, and nothing else needs it.
func TestEngine_OtherQueriesDoNotNeedTheFlag(t *testing.T) {
	for _, query := range []string{
		`sum(count_over_time({app="a"}[1m]))`,
		`quantile_over_time(0.5, {app="a"} | logfmt | unwrap duration [1m])`,
		`sum by (app) (rate({app="a"} | logfmt | unwrap duration [1m]))`,
		`label_replace(sum by (app) (count_over_time({app="a"}[1m])), "x", "$1", "app", "(.*)")`,
		`sum by (buckets) (count_over_time({app="a"} | logfmt | buckets="x" [1m]))`,
	} {
		t.Run(query, func(t *testing.T) {
			eng := NewEngine(EngineOpts{}, NewMockQuerier(0, histogramFlagStreams), NoLimits, log.NewNopLogger())
			_, err := eng.Query(histogramFlagTestParams(t, query, false, true)).Exec(user.InjectOrgID(context.Background(), "fake"))
			require.NoError(t, err)
		})
	}
}

func TestDownstreamEngine_HistogramsAreOffByDefault(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "fake")
	regular := NewEngine(EngineOpts{EnableHistograms: true}, NewMockQuerier(0, histogramFlagStreams), NoLimits, log.NewNopLogger())

	for _, tc := range histogramQueries {
		t.Run(tc.name, func(t *testing.T) {
			params := histogramFlagTestParams(t, tc.query, tc.unchecked, true)

			off := NewDownstreamEngine(EngineOpts{}, MockDownstreamer{regular}, NoLimits, log.NewNopLogger())
			_, err := off.Query(ctx, params).Exec(ctx)
			require.ErrorIs(t, err, logqlerr.ErrParse)
			require.ErrorContains(t, err, "-"+histogramFlagName)

			// With the flag on the query is not refused for it. The downstream evaluator has no querier of its
			// own and what happens to the query next is not what is tested here.
			on := NewDownstreamEngine(EngineOpts{EnableHistograms: true}, MockDownstreamer{regular}, NoLimits, log.NewNopLogger())
			_, err = on.Query(ctx, params).Exec(ctx)
			if err != nil {
				require.NotContains(t, err.Error(), histogramFlagName)
			}
		})
	}
}

func TestEngineOpts_EnableHistogramsConfig(t *testing.T) {
	t.Run("the flag is off by default and is set with the CLI flag", func(t *testing.T) {
		var opts EngineOpts
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		opts.RegisterFlagsWithPrefix("querier.engine.", fs)
		require.False(t, opts.EnableHistograms)

		require.NoError(t, fs.Parse([]string{"-" + histogramFlagName + "=true"}))
		require.True(t, opts.EnableHistograms)
	})

	t.Run("the YAML key", func(t *testing.T) {
		var opts EngineOpts
		// Strict, as the loading of the configuration is, so that a key that is not known is an error.
		dec := yaml.NewDecoder(strings.NewReader("enable_histograms: true\n"))
		dec.KnownFields(true)
		require.NoError(t, dec.Decode(&opts))
		require.True(t, opts.EnableHistograms)
	})

	t.Run("the flag is documented as experimental", func(t *testing.T) {
		var opts EngineOpts
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		opts.RegisterFlagsWithPrefix("querier.engine.", fs)
		f := fs.Lookup(histogramFlagName)
		require.NotNil(t, f)
		require.Equal(t, "false", f.DefValue)
		require.Contains(t, f.Usage, "Experimental")
	})
}

// A query gets the options of the engine that runs it. The downstream engine of the query frontend builds
// them on its own, and it must not leave one out: that would ignore the setting of a frontend without a
// sign of it.
func TestEngine_QueryOpts(t *testing.T) {
	opts := EngineOpts{EnableHistograms: true, MaxQuerySeriesCutoff: true, LogExecutingQuery: true}
	params, err := NewLiteralParams(`count_over_time({app="a"}[1m])`, time.Unix(60, 0), time.Unix(60, 0), 0, 0, logproto.FORWARD, 100, nil, nil)
	require.NoError(t, err)

	t.Run("the engine", func(t *testing.T) {
		eng := NewEngine(opts, NewMockQuerier(0, nil), NoLimits, log.NewNopLogger())

		q, ok := eng.Query(params).(*query)
		require.True(t, ok)
		require.Equal(t, queryOpts{logExecutingQuery: true, maxQuerySeriesCutoff: true, histogramsEnabled: true}, q.opts)
	})

	t.Run("the downstream engine", func(t *testing.T) {
		regular := NewEngine(opts, NewMockQuerier(0, nil), NoLimits, log.NewNopLogger())
		eng := NewDownstreamEngine(opts, MockDownstreamer{regular}, NoLimits, log.NewNopLogger())

		q, ok := eng.Query(context.Background(), params).(*query)
		require.True(t, ok)
		// The query that it downstreams is logged by the engine that issued it.
		require.Equal(t, queryOpts{logExecutingQuery: false, maxQuerySeriesCutoff: true, histogramsEnabled: true}, q.opts)
	})
}
