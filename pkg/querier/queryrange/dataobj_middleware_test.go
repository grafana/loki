package queryrange

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/querier/testutil"
)

func TestDataObjEligibilityMiddleware(t *testing.T) {
	var (
		insideStart = dataObjRoutingNow.Add(-24 * time.Hour)
		insideEnd   = dataObjRoutingNow.Add(-12 * time.Hour)
		ctx         = user.InjectOrgID(t.Context(), "1")
		cfg         = Config{DataObjEnabled: true, DataObjStorageLag: dataObjRoutingLag, now: func() time.Time { return dataObjRoutingNow }}
	)

	newMiddleware := func(limits fakeLimits, rec *requestRecorder) (queryrangebase.Handler, *prometheus.Registry) {
		reg := prometheus.NewRegistry()
		return newDataObjEligibilityMiddleware(cfg, limits, NewDataObjMetrics(reg, "loki")).Wrap(rec), reg
	}

	rangeRequest := func(query string, options *logproto.DataObjOptions) *LokiRequest {
		return &LokiRequest{
			Query:          query,
			Plan:           testutil.MustPlan(query),
			Step:           60000,
			StartTs:        insideStart,
			EndTs:          insideEnd,
			Path:           "/loki/api/v1/query_range",
			DataObjOptions: options,
		}
	}

	instantRequest := func(query string, options *logproto.DataObjOptions) *LokiInstantRequest {
		return &LokiInstantRequest{
			Query:          query,
			Plan:           testutil.MustPlan(query),
			TimeTs:         insideEnd,
			Path:           "/loki/api/v1/query",
			DataObjOptions: options,
		}
	}

	// streamFirstRequestRange returns the request range whose read window for streamFirstQuery is
	// windowStart to windowEnd. The range vector of 1m moves the start by 1m, and the end is exclusive.
	streamFirstRequestRange := func(windowStart, windowEnd time.Time) (start, end time.Time) {
		return windowStart.Add(time.Minute), windowEnd.Add(-time.Nanosecond)
	}

	t.Run("marks an eligible range request", func(t *testing.T) {
		rec := &requestRecorder{}
		handler, reg := newMiddleware(dataObjTenantLimits("1"), rec)

		_, err := handler.Do(ctx, rangeRequest(streamFirstQuery, nil))

		require.NoError(t, err)
		require.True(t, rec.requests[0].(*LokiRequest).DataObjOptions.GetEligible())
		requireEligibilityCounts(t, reg, map[string]int{"eligible": 1})
	})

	t.Run("marks an eligible instant request", func(t *testing.T) {
		rec := &requestRecorder{}
		handler, reg := newMiddleware(dataObjTenantLimits("1"), rec)

		_, err := handler.Do(ctx, instantRequest(streamFirstQuery, nil))

		require.NoError(t, err)
		require.True(t, rec.requests[0].(*LokiInstantRequest).DataObjOptions.GetEligible())
		requireEligibilityCounts(t, reg, map[string]int{"eligible": 1})
	})

	t.Run("clears the options a client sets on a request that is not eligible", func(t *testing.T) {
		rec := &requestRecorder{}
		handler, reg := newMiddleware(dataObjTenantLimits("1"), rec)

		_, err := handler.Do(ctx, rangeRequest(`count_over_time({app="a"}[1m])`, &logproto.DataObjOptions{Eligible: true}))
		require.NoError(t, err)
		_, err = handler.Do(ctx, instantRequest(`count_over_time({app="a"}[1m])`, &logproto.DataObjOptions{Eligible: true}))
		require.NoError(t, err)

		require.Nil(t, rec.requests[0].(*LokiRequest).DataObjOptions)
		require.Nil(t, rec.requests[1].(*LokiInstantRequest).DataObjOptions)
		requireEligibilityCounts(t, reg, map[string]int{"unsupported_query": 2})
	})

	t.Run("sets the options on a copy and leaves the request it receives unchanged", func(t *testing.T) {
		rec := &requestRecorder{}
		handler, reg := newMiddleware(dataObjTenantLimits("1"), rec)
		req := rangeRequest(streamFirstQuery, nil)

		_, err := handler.Do(ctx, req)

		require.NoError(t, err)
		require.Nil(t, req.DataObjOptions)
		require.NotSame(t, req, rec.requests[0])
		requireEligibilityCounts(t, reg, map[string]int{"eligible": 1})
	})

	t.Run("does not change the options of the request it receives", func(t *testing.T) {
		rec := &requestRecorder{}
		handler, reg := newMiddleware(dataObjTenantLimits("1"), rec)
		req := rangeRequest(`count_over_time({app="a"}[1m])`, &logproto.DataObjOptions{Eligible: true})
		received := req.DataObjOptions

		_, err := handler.Do(ctx, req)

		require.NoError(t, err)
		require.Same(t, received, req.DataObjOptions)
		require.True(t, received.GetEligible())
		requireEligibilityCounts(t, reg, map[string]int{"unsupported_query": 1})
	})

	t.Run("forwards other request types as they are", func(t *testing.T) {
		rec := &requestRecorder{}
		handler, reg := newMiddleware(dataObjTenantLimits("1"), rec)
		req := &LokiSeriesRequest{Match: []string{`{app="a"}`}, StartTs: insideStart, EndTs: insideEnd}

		_, err := handler.Do(ctx, req)

		require.NoError(t, err)
		require.Same(t, req, rec.requests[0])
		requireEligibilityCounts(t, reg, nil)
	})

	t.Run("forwards every request as it is when the config turns data objects off", func(t *testing.T) {
		rec := &requestRecorder{}
		reg := prometheus.NewRegistry()
		handler := newDataObjEligibilityMiddleware(Config{}, dataObjTenantLimits("1"), NewDataObjMetrics(reg, "loki")).Wrap(rec)
		req := rangeRequest(streamFirstQuery, nil)

		_, err := handler.Do(ctx, req)

		require.NoError(t, err)
		require.Same(t, req, rec.requests[0])
		requireEligibilityCounts(t, reg, nil)
	})

	t.Run("clears the options of a request without a tenant and counts it as disabled", func(t *testing.T) {
		rec := &requestRecorder{}
		handler, reg := newMiddleware(dataObjTenantLimits("1"), rec)
		req := rangeRequest(streamFirstQuery, &logproto.DataObjOptions{Eligible: true})

		_, err := handler.Do(t.Context(), req)

		require.NoError(t, err)
		require.Nil(t, rec.requests[0].(*LokiRequest).DataObjOptions)
		requireEligibilityCounts(t, reg, map[string]int{"data_objects_disabled": 1})
	})

	t.Run("parses the query of a request without a plan", func(t *testing.T) {
		rec := &requestRecorder{}
		handler, reg := newMiddleware(dataObjTenantLimits("1"), rec)
		req := rangeRequest(streamFirstQuery, nil)
		req.Plan = nil

		_, err := handler.Do(ctx, req)

		require.NoError(t, err)
		require.True(t, rec.requests[0].(*LokiRequest).DataObjOptions.GetEligible())
		requireEligibilityCounts(t, reg, map[string]int{"eligible": 1})
	})

	t.Run("marks a request that overlaps the end of the data-object range by a quarter of its read window", func(t *testing.T) {
		rec := &requestRecorder{}
		handler, reg := newMiddleware(dataObjTenantLimits("1"), rec)
		windowStart := dataObjRoutingBandEnd.Add(-25 * time.Minute)
		req := rangeRequest(streamFirstQuery, nil)
		req.StartTs, req.EndTs = streamFirstRequestRange(windowStart, windowStart.Add(100*time.Minute))

		_, err := handler.Do(ctx, req)

		require.NoError(t, err)
		require.True(t, rec.requests[0].(*LokiRequest).DataObjOptions.GetEligible())
		requireEligibilityCounts(t, reg, map[string]int{"eligible": 1})
	})

	t.Run("marks a request that overlaps the start of the data-object range by a quarter of its read window", func(t *testing.T) {
		rec := &requestRecorder{}
		handler, reg := newMiddleware(dataObjTenantLimits("1"), rec)
		windowEnd := dataObjRoutingBandStart.Add(25 * time.Minute)
		req := rangeRequest(streamFirstQuery, nil)
		req.StartTs, req.EndTs = streamFirstRequestRange(windowEnd.Add(-100*time.Minute), windowEnd)

		_, err := handler.Do(ctx, req)

		require.NoError(t, err)
		require.True(t, rec.requests[0].(*LokiRequest).DataObjOptions.GetEligible())
		requireEligibilityCounts(t, reg, map[string]int{"eligible": 1})
	})

	t.Run("counts a request with a query that does not parse as unsupported", func(t *testing.T) {
		rec := &requestRecorder{}
		handler, reg := newMiddleware(dataObjTenantLimits("1"), rec)
		req := rangeRequest(streamFirstQuery, &logproto.DataObjOptions{Eligible: true})
		req.Query, req.Plan = `sum(`, nil

		_, err := handler.Do(ctx, req)

		require.NoError(t, err)
		require.Nil(t, rec.requests[0].(*LokiRequest).DataObjOptions)
		requireEligibilityCounts(t, reg, map[string]int{"unsupported_query": 1})
	})

	t.Run("counts every request by its outcome", func(t *testing.T) {
		rec := &requestRecorder{}
		limits := dataObjTenantLimits("1")
		handler, reg := newMiddleware(limits, rec)

		for _, req := range []queryrangebase.Request{
			rangeRequest(streamFirstQuery, nil),
			instantRequest(streamFirstQuery, nil),
			rangeRequest(`rate({app="a"}[1m])`, nil),
		} {
			_, err := handler.Do(ctx, req)
			require.NoError(t, err)
		}
		outside := rangeRequest(streamFirstQuery, nil)
		outside.StartTs, outside.EndTs = dataObjRoutingNow.Add(-2*time.Hour), dataObjRoutingNow.Add(-time.Hour)
		_, err := handler.Do(ctx, outside)
		require.NoError(t, err)

		requireEligibilityCounts(t, reg, map[string]int{
			"eligible":                  2,
			"unsupported_query":         1,
			"outside_data_object_range": 1,
		})
	})
}

// requireEligibilityCounts compares the eligibility metric with the expected count of each outcome.
// An outcome that is not in want must have a count of zero, because the metric creates every series
// up front.
func requireEligibilityCounts(t *testing.T, reg prometheus.Gatherer, want map[string]int) {
	t.Helper()

	const name = "loki_query_frontend_dataobj_eligibility_total"
	var expected strings.Builder
	fmt.Fprintf(&expected, "# HELP %s Query requests that the query-frontend sends to queriers, by data-object eligibility outcome. Sharding and range splitting turn one user query into many requests, and a retry sends a request again. Each request adds one count.\n", name)
	fmt.Fprintf(&expected, "# TYPE %s counter\n", name)
	for _, outcome := range dataObjOutcomes {
		fmt.Fprintf(&expected, "%s{outcome=%q} %d\n", name, outcome, want[outcome])
	}

	require.NoError(t, promtestutil.GatherAndCompare(reg, strings.NewReader(expected.String()), name))
}

// requestRecorder is a handler that records the requests it receives. It answers with an empty
// vector, because the sharding middleware needs a valid response of that type to merge.
type requestRecorder struct {
	requestsMu sync.Mutex
	requests   []queryrangebase.Request
}

func (rec *requestRecorder) Do(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	rec.requestsMu.Lock()
	defer rec.requestsMu.Unlock()
	rec.requests = append(rec.requests, r)
	return &LokiPromResponse{Response: &queryrangebase.PrometheusResponse{
		Status: loghttp.QueryStatusSuccess,
		Data:   queryrangebase.PrometheusData{ResultType: loghttp.ResultTypeVector},
	}}, nil
}
