package base

import (
	"context"
	"errors"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type fakePusher struct {
	request  *logproto.WriteRequest
	response *logproto.WriteResponse
	err      error
}

func (p *fakePusher) Push(_ context.Context, r *logproto.WriteRequest) (*logproto.WriteResponse, error) {
	p.request = r
	return p.response, p.err
}

func TestPusherAppendable(t *testing.T) {
	pusher := &fakePusher{}
	pa := NewPusherAppendable(pusher, "user-1", prometheus.NewCounter(prometheus.CounterOpts{}), prometheus.NewCounter(prometheus.CounterOpts{}))

	for _, tc := range []struct {
		name       string
		series     string
		value      float64
		expectedTS int64
	}{
		{
			name:       "tenant, normal value",
			series:     "foo_bar",
			value:      1.234,
			expectedTS: 120_000,
		},
		{
			name:       "tenant, stale nan value",
			series:     "foo_bar",
			value:      math.Float64frombits(value.StaleNaN),
			expectedTS: 120_000,
		},
		{
			name:       "ALERTS, normal value",
			series:     `ALERTS{alertname="boop"}`,
			value:      1.234,
			expectedTS: 120_000,
		},
		{
			name:       "ALERTS, stale nan value",
			series:     `ALERTS{alertname="boop"}`,
			value:      math.Float64frombits(value.StaleNaN),
			expectedTS: 120_000,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			lbls, err := parser.ParseMetric(tc.series)
			require.NoError(t, err)

			pusher.response = &logproto.WriteResponse{}
			a := pa.Appender(ctx)
			_, err = a.Append(0, lbls, 120_000, tc.value)
			require.NoError(t, err)

			require.NoError(t, a.Commit())

			require.Equal(t, tc.expectedTS, pusher.request.Timeseries[0].Samples[0].TimestampMs)

		})
	}
}

func TestPusherErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		returnedError    error
		expectedWrites   int
		expectedFailures int
	}{
		"no error": {
			expectedWrites:   1,
			expectedFailures: 0,
		},

		"400 error": {
			returnedError:    httpgrpc.Errorf(http.StatusBadRequest, "test error"),
			expectedWrites:   1,
			expectedFailures: 0, // 400 errors not reported as failures.
		},

		"500 error": {
			returnedError:    httpgrpc.Errorf(http.StatusInternalServerError, "test error"),
			expectedWrites:   1,
			expectedFailures: 1, // 500 errors are failures
		},

		"unknown error": {
			returnedError:    errors.New("test error"),
			expectedWrites:   1,
			expectedFailures: 1, // unknown errors are not 400, so they are reported.
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			pusher := &fakePusher{err: tc.returnedError, response: &logproto.WriteResponse{}}

			writes := prometheus.NewCounter(prometheus.CounterOpts{})
			failures := prometheus.NewCounter(prometheus.CounterOpts{})

			pa := NewPusherAppendable(pusher, "user-1", writes, failures)

			lbls, err := parser.ParseMetric("foo_bar")
			require.NoError(t, err)

			a := pa.Appender(ctx)
			_, err = a.Append(0, lbls, int64(model.Now()), 123456)
			require.NoError(t, err)

			require.Equal(t, tc.returnedError, a.Commit())

			require.Equal(t, tc.expectedWrites, int(testutil.ToFloat64(writes)))
			require.Equal(t, tc.expectedFailures, int(testutil.ToFloat64(failures)))
		})
	}
}

func TestMetricsQueryFuncErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		returnedError         error
		expectedQueries       int
		expectedFailedQueries int
	}{
		"no error": {
			expectedQueries:       1,
			expectedFailedQueries: 0,
		},

		"400 error": {
			returnedError:         httpgrpc.Errorf(http.StatusBadRequest, "test error"),
			expectedQueries:       1,
			expectedFailedQueries: 0, // 400 errors not reported as failures.
		},

		"500 error": {
			returnedError:         httpgrpc.Errorf(http.StatusInternalServerError, "test error"),
			expectedQueries:       1,
			expectedFailedQueries: 1, // 500 errors are failures
		},

		"promql.ErrStorage": {
			returnedError:         promql.ErrStorage{Err: errors.New("test error")},
			expectedQueries:       1,
			expectedFailedQueries: 1,
		},

		"promql.ErrQueryCanceled": {
			returnedError:         promql.ErrQueryCanceled("test error"),
			expectedQueries:       1,
			expectedFailedQueries: 0, // Not interesting.
		},

		"promql.ErrQueryTimeout": {
			returnedError:         promql.ErrQueryTimeout("test error"),
			expectedQueries:       1,
			expectedFailedQueries: 0, // Not interesting.
		},

		"promql.ErrTooManySamples": {
			returnedError:         promql.ErrTooManySamples("test error"),
			expectedQueries:       1,
			expectedFailedQueries: 0, // Not interesting.
		},

		"unknown error": {
			returnedError:         errors.New("test error"),
			expectedQueries:       1,
			expectedFailedQueries: 1, // unknown errors are not 400, so they are reported.
		},
	} {
		t.Run(name, func(t *testing.T) {
			queries := prometheus.NewCounter(prometheus.CounterOpts{})
			failures := prometheus.NewCounter(prometheus.CounterOpts{})

			mockFunc := func(_ context.Context, _ string, _ time.Time) (promql.Vector, error) {
				return promql.Vector{}, WrapQueryableErrors(tc.returnedError)
			}
			qf := MetricsQueryFunc(mockFunc, queries, failures)

			_, err := qf(context.Background(), "test", time.Now())
			require.Equal(t, tc.returnedError, err)

			require.Equal(t, tc.expectedQueries, int(testutil.ToFloat64(queries)))
			require.Equal(t, tc.expectedFailedQueries, int(testutil.ToFloat64(failures)))
		})
	}
}

func TestRecordAndReportRuleQueryMetrics(t *testing.T) {
	queryTime := prometheus.NewCounterVec(prometheus.CounterOpts{}, []string{"user"})

	mockFunc := func(_ context.Context, _ string, _ time.Time) (promql.Vector, error) {
		time.Sleep(1 * time.Second)
		return promql.Vector{}, nil
	}
	qf := RecordAndReportRuleQueryMetrics(mockFunc, queryTime.WithLabelValues("userID"), log.NewNopLogger())
	_, _ = qf(context.Background(), "test", time.Now())

	require.GreaterOrEqual(t, testutil.ToFloat64(queryTime.WithLabelValues("userID")), float64(1))
}
