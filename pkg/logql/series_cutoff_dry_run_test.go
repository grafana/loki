package logql

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/logproto"
	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
	logqlmodel "github.com/grafana/loki/v3/pkg/logql/model"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

func TestCutoffTracker_ShouldReportTheOutcomeOnlyWhenItFired(t *testing.T) {
	otherErr := errors.New("boom")

	for name, tc := range map[string]struct {
		fired       bool
		err         error
		wantOutcome string
		wantOK      bool
	}{
		"did not fire and succeeded": {fired: false, err: nil},
		"did not fire and failed":    {fired: false, err: logqlerr.NewSeriesLimitError(1)},
		"fired and succeeded":        {fired: true, err: nil, wantOutcome: cutoffDryRunMismatch, wantOK: true},
		"fired and hit the limit":    {fired: true, err: logqlerr.NewSeriesLimitError(1), wantOutcome: cutoffDryRunAgreed, wantOK: true},
		"fired and failed otherwise": {fired: true, err: otherErr, wantOutcome: cutoffDryRunInconclusive, wantOK: true},
	} {
		t.Run(name, func(t *testing.T) {
			tracker := newCutoffTracker()
			if tc.fired {
				tracker.record()
			}
			outcome, ok := tracker.outcome(tc.err)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.wantOutcome, outcome)
		})
	}

	t.Run("nil tracker", func(t *testing.T) {
		var tracker *cutoffTracker
		tracker.record()
		_, ok := tracker.outcome(nil)
		require.False(t, ok)
	})
}

func TestIteratorLimits_ShouldRecordInsteadOfFailingInADryRun(t *testing.T) {
	tracker := newCutoffTracker()
	limits := iteratorLimits{maxSeries: 2, cutoffTracker: tracker}

	require.NoError(t, limits.validateMaxSeries(2))
	_, fired := tracker.outcome(nil)
	require.False(t, fired)

	require.NoError(t, limits.validateMaxSeries(3))
	outcome, fired := tracker.outcome(nil)
	require.True(t, fired)
	require.Equal(t, cutoffDryRunMismatch, outcome)
}

func TestEngine_MaxQuerySeriesCutoffDryRun_ShouldNotChangeAnyOutcome(t *testing.T) {
	const maxSeries = 3
	var (
		start = time.Unix(60, 0)
		end   = time.Unix(120, 0)
	)
	run := func(t *testing.T, ctx context.Context, opts EngineOpts, query string, numSeries int) (any, int, error) {
		t.Helper()
		var read atomic.Int64
		querier := newCountingQuerier(NewMockQuerier(1, distinctStreams(numSeries, end)), &read)
		eng := NewEngine(opts, querier, &fakeLimits{maxSeries: maxSeries, timeout: time.Minute}, log.NewNopLogger())
		params, err := NewLiteralParams(query, start, end, 30*time.Second, 0, logproto.FORWARD, 1000, nil, nil)
		require.NoError(t, err)
		res, err := eng.Query(params).Exec(ctx)
		return res.Data, int(read.Load()), err
	}

	for name, tc := range map[string]struct {
		query        string
		numSeries    int
		wantErr      bool
		wantAgreed   float64
		wantMismatch float64
		drilldown    bool
	}{
		"bare range aggregation over the limit": {query: `count_over_time({app="a"}[1m])`, numSeries: 10, wantErr: true, wantAgreed: 1},
		"stream iterator over the limit":        {query: `count_over_time({app="a"}[10s])`, numSeries: 10, wantErr: true, wantAgreed: 1},
		"sum by over the limit":                 {query: `sum by (id) (count_over_time({app="a"}[1m]))`, numSeries: 10, wantErr: true, wantAgreed: 1},
		"under the limit":                       {query: `count_over_time({app="a"}[1m])`, numSeries: maxSeries},
		"sum is not output preserving":          {query: `sum(count_over_time({app="a"}[1m]))`, numSeries: 10},
		"Logs Drilldown requests are exempt":    {query: `count_over_time({app="a"}[1m])`, numSeries: 10, drilldown: true},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := user.InjectOrgID(context.Background(), "fake")
			if tc.drilldown {
				ctx = httpreq.InjectQueryTags(ctx, "Source=grafana-lokiexplore-app")
			}
			agreed := testutil.ToFloat64(seriesCutoffDryRunTotal.WithLabelValues(cutoffDryRunAgreed))
			mismatch := testutil.ToFloat64(seriesCutoffDryRunTotal.WithLabelValues(cutoffDryRunMismatch))

			dataOff, readOff, errOff := run(t, ctx, EngineOpts{}, tc.query, tc.numSeries)
			dataDry, readDry, errDry := run(t, ctx, EngineOpts{MaxQuerySeriesCutoffDryRun: true}, tc.query, tc.numSeries)

			require.Equal(t, errOff, errDry)
			if tc.drilldown {
				// A partial result keeps whichever series arrived first, so only its size is stable.
				require.Len(t, dataDry, len(dataOff.(promql.Matrix)))
			} else {
				require.Equal(t, dataOff, dataDry)
			}
			require.Equal(t, readOff, readDry, "a dry run must read as much as the query without the cut-off")
			require.Equal(t, tc.wantErr, errDry != nil)
			require.Equal(t, tc.wantAgreed, testutil.ToFloat64(seriesCutoffDryRunTotal.WithLabelValues(cutoffDryRunAgreed))-agreed)
			require.Equal(t, tc.wantMismatch, testutil.ToFloat64(seriesCutoffDryRunTotal.WithLabelValues(cutoffDryRunMismatch))-mismatch)
		})
	}

	t.Run("the cut-off wins when both are enabled", func(t *testing.T) {
		ctx := user.InjectOrgID(context.Background(), "fake")
		agreed := testutil.ToFloat64(seriesCutoffDryRunTotal.WithLabelValues(cutoffDryRunAgreed))

		_, readOff, _ := run(t, ctx, EngineOpts{}, `count_over_time({app="a"}[1m])`, 10)
		_, readBoth, err := run(t, ctx, EngineOpts{MaxQuerySeriesCutoff: true, MaxQuerySeriesCutoffDryRun: true}, `count_over_time({app="a"}[1m])`, 10)

		require.Equal(t, logqlerr.NewSeriesLimitError(maxSeries), err)
		require.Less(t, readBoth, readOff)
		require.Equal(t, agreed, testutil.ToFloat64(seriesCutoffDryRunTotal.WithLabelValues(cutoffDryRunAgreed)))
	})
}

func TestEngine_MaxQuerySeriesCutoffDryRun_ShouldNotChangeStreamFirstExecution(t *testing.T) {
	const maxSeries = 3
	var (
		start  = time.Unix(60, 0)
		end    = time.Unix(120, 0)
		limits = tenantStreamFirstLimits{
			fakeLimits: fakeLimits{maxSeries: maxSeries, timeout: time.Minute},
			enabled:    map[string]bool{"fake": true},
		}
	)
	run := func(t *testing.T, ctx context.Context, opts EngineOpts) (logqlmodel.Result, int, error) {
		t.Helper()
		var read atomic.Int64
		querier := newCountingQuerier(NewMockQuerier(1, distinctStreams(10, end)), &read)
		eng := NewEngine(opts, querier, limits, log.NewNopLogger())
		params, err := NewLiteralParams(`sum by (id) (count_over_time({app="a"}[1m]))`, start, end, 30*time.Second, 0, logproto.FORWARD, 1000, nil, nil)
		require.NoError(t, err)
		res, err := eng.Query(params).Exec(ctx)
		return res, int(read.Load()), err
	}

	for name, drilldown := range map[string]bool{
		"the accumulator keeps its own series limit":         false,
		"Logs Drilldown requests keep their partial results": true,
	} {
		t.Run(name, func(t *testing.T) {
			ctx := user.InjectOrgID(context.Background(), "fake")
			if drilldown {
				ctx = httpreq.InjectQueryTags(ctx, "Source=grafana-lokiexplore-app")
			}
			counted := func() float64 {
				return testutil.ToFloat64(seriesCutoffDryRunTotal.WithLabelValues(cutoffDryRunAgreed)) +
					testutil.ToFloat64(seriesCutoffDryRunTotal.WithLabelValues(cutoffDryRunMismatch)) +
					testutil.ToFloat64(seriesCutoffDryRunTotal.WithLabelValues(cutoffDryRunInconclusive))
			}
			before := counted()

			resOff, readOff, errOff := run(t, ctx, EngineOpts{})
			resDry, readDry, errDry := run(t, ctx, EngineOpts{MaxQuerySeriesCutoffDryRun: true})

			require.Equal(t, errOff, errDry)
			require.Equal(t, readOff, readDry)
			require.Equal(t, resOff.Warnings, resDry.Warnings)
			if drilldown {
				require.NoError(t, errDry)
				require.Len(t, resDry.Data, len(resOff.Data.(promql.Matrix)))
			} else {
				require.Equal(t, logqlerr.NewSeriesLimitError(maxSeries), errDry)
			}
			require.Equal(t, before, counted(), "the accumulator's own limit is not the cut-off")
		})
	}
}

func TestCutoffTracker_ShouldLogTheQueryThatMismatched(t *testing.T) {
	const query = `count_over_time({app="a"}[1m])`
	start := time.Unix(60, 0)

	newParams := func(t *testing.T, step time.Duration, end time.Time) Params {
		t.Helper()
		params, err := NewLiteralParams(query, start, end, step, 0, logproto.FORWARD, 1000, nil, nil)
		require.NoError(t, err)
		return params
	}
	run := func(t *testing.T, params Params, err error) string {
		t.Helper()
		var buf bytes.Buffer
		ctx := httpreq.InjectQueryTags(user.InjectOrgID(context.Background(), "fake"), "Source=grafana-lokiexplore-app")
		tracker := newCutoffTracker()
		tracker.record()
		tracker.report(ctx, log.NewLogfmtLogger(&buf), params, err)
		return buf.String()
	}

	t.Run("a mismatch logs the identifying fields of a range query", func(t *testing.T) {
		out := run(t, newParams(t, 30*time.Second, time.Unix(120, 0)), nil)

		require.Contains(t, out, "level=warn")
		require.Contains(t, out, "would have failed a query that succeeded")
		require.Contains(t, out, `query="count_over_time({app=\"a\"}[1m])"`)
		require.Contains(t, out, fmt.Sprintf("query_hash=%d", util.HashedQuery(query)))
		require.Contains(t, out, "type=range")
		require.Contains(t, out, "length=1m0s")
		require.Contains(t, out, "step=30s")
		require.Contains(t, out, "org_id=fake")
		require.Contains(t, out, "source=grafana-lokiexplore-app")
	})

	t.Run("a mismatch logs an instant query as instant", func(t *testing.T) {
		out := run(t, newParams(t, 0, start), nil)

		require.Contains(t, out, "type=instant")
		require.NotContains(t, out, "step=")
	})

	t.Run("an agreed outcome logs nothing", func(t *testing.T) {
		require.Empty(t, run(t, newParams(t, 30*time.Second, time.Unix(120, 0)), logqlerr.NewSeriesLimitError(1)))
	})
}
