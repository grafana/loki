package limits

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

type fakeTimeLimits struct {
	maxQueryLookback time.Duration
	maxQueryLength   time.Duration
}

func (f fakeTimeLimits) MaxQueryLookback(_ context.Context, _ string) time.Duration {
	return f.maxQueryLookback
}

func (f fakeTimeLimits) MaxQueryLength(_ context.Context, _ string) time.Duration {
	return f.maxQueryLength
}

func Test_validateQueryTimeRangeLimits(t *testing.T) {
	now := time.Now()
	nowFunc = func() time.Time { return now }
	tests := []struct {
		name        string
		limits      TimeRangeLimits
		from        time.Time
		through     time.Time
		wantFrom    time.Time
		wantThrough time.Time
		wantErr     bool
	}{
		{"no change", fakeTimeLimits{1000 * time.Hour, 1000 * time.Hour}, now, now.Add(24 * time.Hour), now, now.Add(24 * time.Hour), false},
		{"clamped to 24h", fakeTimeLimits{24 * time.Hour, 1000 * time.Hour}, now.Add(-48 * time.Hour), now, now.Add(-24 * time.Hour), now, false},
		{"end before start", fakeTimeLimits{}, now, now.Add(-48 * time.Hour), time.Time{}, time.Time{}, true},
		{"query too long", fakeTimeLimits{maxQueryLength: 24 * time.Hour}, now.Add(-48 * time.Hour), now, time.Time{}, time.Time{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, through, err := ValidateQueryTimeRangeLimits(context.Background(), "foo", tt.limits, tt.from, tt.through)
			if tt.wantErr {
				require.NotNil(t, err)
			} else {
				require.Nil(t, err)
			}
			require.Equal(t, tt.wantFrom, from, "wanted (%s) got (%s)", tt.wantFrom, from)
			require.Equal(t, tt.wantThrough, through)
		})
	}
}

func TestValidateAggregatedMetricQuery(t *testing.T) {
	makeReqAndAST := func(queryStr string) logql.QueryParams {
		now := time.Now()
		expr, err := syntax.ParseExpr(queryStr)
		if err != nil {
			panic(err)
		}
		switch expr.(type) {
		case syntax.SampleExpr:
			return logql.SelectSampleParams{SampleQueryRequest: &logproto.SampleQueryRequest{
				Selector: queryStr,
				Start:    now.Add(-time.Hour),
				End:      now,
				Plan:     &plan.QueryPlan{AST: expr},
			},
			}
		default:
			return logql.SelectLogParams{QueryRequest: &logproto.QueryRequest{
				Selector:  queryStr,
				Start:     now.Add(-time.Hour),
				End:       now,
				Direction: logproto.BACKWARD,
				Plan: &plan.QueryPlan{
					AST: expr,
				},
			},
			}
		}
	}

	tcs := []struct {
		desc          string
		req           logql.QueryParams
		queryTags     string
		expectedError error
	}{
		{
			desc:          "normal query, no error",
			req:           makeReqAndAST(`{foo="bar"}`),
			queryTags:     "",
			expectedError: nil,
		},
		{
			desc:          "aggregated metric query from explore, no error",
			req:           makeReqAndAST(`{__aggregated_metric__="service-name"}`),
			queryTags:     "source=" + logsDrilldownAppName,
			expectedError: nil,
		},
		{
			desc:          "query tags are case insensitive",
			req:           makeReqAndAST(`{__aggregated_metric__="service-name"}`),
			queryTags:     "Source=" + logsDrilldownAppName,
			expectedError: nil,
		},
		{
			desc:          "aggregated metric query from explore, multiple selectors, no error",
			req:           makeReqAndAST(`{app="service-name", __aggregated_metric__="true"}`),
			queryTags:     "source=" + logsDrilldownAppName,
			expectedError: nil,
		},
		{
			desc:          "aggregated metric query from explore, multiple selectors, filter, no error",
			req:           makeReqAndAST(`{app="service-name", __aggregated_metric__="true"} |= "test"`),
			queryTags:     "source=" + logsDrilldownAppName,
			expectedError: nil,
		},
		{
			desc:          "aggregated metrics metric query from explore, multiple selectors, filter, no error",
			req:           makeReqAndAST(`sum by (service_name)(count_over_time({app="service-name", __aggregated_metric__="true"} |= "test" [5m]))`),
			queryTags:     "source=" + logsDrilldownAppName,
			expectedError: nil,
		},
		{
			desc:          "aggregated metric query from other source, blocked",
			req:           makeReqAndAST(`{__aggregated_metric__="service-name"}`),
			queryTags:     "source=other-app",
			expectedError: ErrAggMetricsDrilldownOnly,
		},
		{
			desc:          "aggregated metric query with no source, blocked",
			req:           makeReqAndAST(`{__aggregated_metric__="service-name"}`),
			queryTags:     "",
			expectedError: ErrAggMetricsDrilldownOnly,
		},
		{
			desc:          "aggregated metric query with no source, multiple selectors, blocked",
			req:           makeReqAndAST(`{app="service-name", __aggregated_metric__="true"}`),
			queryTags:     "",
			expectedError: ErrAggMetricsDrilldownOnly,
		},
		{
			desc:          "aggregated metrics metric query with no source, multiple selectors, filter, blocked",
			req:           makeReqAndAST(`sum by (service_name)(count_over_time({app="service-name", __aggregated_metric__="true"} |= "test" [5m]))`),
			queryTags:     "",
			expectedError: ErrAggMetricsDrilldownOnly,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			ctx := context.Background()
			if tc.queryTags != "" {
				ctx = httpreq.InjectQueryTags(ctx, tc.queryTags)
			}

			err := ValidateAggregatedMetricQuery(ctx, tc.req)
			if tc.expectedError != nil {
				require.ErrorIs(t, err, tc.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
