package correctness

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logline/querybounds"
	"github.com/grafana/loki/v3/pkg/logline/verification"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestHandleDebug_Success(t *testing.T) {
	base := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	witnessTS := base.Add(1 * time.Minute)
	needle := "9fA81cD2Ef0077aa"

	loki := newTestLokiServer(t, witnessTS, needle)
	defer loki.Close()

	svc := newTestService(t, newTestStore(t), loki.URL, base)
	svc.hintProvider = staticHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: witnessTS.Add(-5 * time.Second), End: witnessTS.Add(5 * time.Second), Source: "first"},
				{Start: witnessTS.Add(-2 * time.Second), End: witnessTS.Add(2 * time.Second), Source: "second"},
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/correctness/debug?start=2026-02-26T10:00:00Z&end=2026-02-26T10:02:00Z&selector=%7Bjob%3D%22api%22%7D&needle=9fA81cD2Ef0077aa", nil)
	rec := httptest.NewRecorder()

	svc.handleDebug(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp debugResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, `{job="api"} |= "9fA81cD2Ef0077aa"`, resp.HintQuery)
	require.Equal(t, string(hintQueryTypeLineFilter), resp.HintQueryType)
	require.Equal(t, base, resp.RangeStart)
	require.Equal(t, base.Add(2*time.Minute), resp.RangeEnd)
	require.Equal(t, resp.RangeStart, resp.ReadStart)
	require.Equal(t, resp.RangeEnd, resp.ReadEnd)
	require.Equal(t, `{job="api"}`, resp.Selector)
	require.Equal(t, needle, resp.Needle)
	require.Equal(t, 0, resp.OverlappingIndexes)
	require.NotEmpty(t, resp.HintDuration)

	require.Len(t, resp.Entries, 1)
	require.Equal(t, witnessTS, resp.Entries[0].Timestamp)
	require.Equal(t, "request trace="+needle+" completed", resp.Entries[0].Line)
	require.True(t, resp.Entries[0].Covered)
	require.NotNil(t, resp.Entries[0].CoveredBy)
	require.Equal(t, 0, *resp.Entries[0].CoveredBy)

	require.Len(t, resp.HintRanges, 2)
	require.Equal(t, 0, resp.HintRanges[0].Index)
	require.Equal(t, "first", resp.HintRanges[0].Source)
	require.Equal(t, 1, resp.HintRanges[0].EntryCount)
	require.Equal(t, 1, resp.HintRanges[1].Index)
	require.Equal(t, "second", resp.HintRanges[1].Source)
	require.Equal(t, 1, resp.HintRanges[1].EntryCount)

	require.Equal(t, 1, resp.Summary.TotalResults)
	require.Equal(t, 1, resp.Summary.CoveredResults)
	require.Equal(t, 0, resp.Summary.FalseNegatives)
	require.Equal(t, 0, resp.Summary.FalsePositives)
	require.True(t, resp.Summary.Correct)
}

func TestHandleDebug_LabelFilter(t *testing.T) {
	base := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	witnessTS := base.Add(1 * time.Minute)
	needle := "checkout-service"

	loki := newTestLokiServer(t, witnessTS, needle)
	defer loki.Close()

	svc := newTestService(t, newTestStore(t), loki.URL, base)
	svc.hintProvider = staticHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: witnessTS.Add(-5 * time.Second), End: witnessTS.Add(5 * time.Second), Source: "label"},
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/correctness/debug?start=2026-02-26T10:00:00Z&end=2026-02-26T10:02:00Z&selector=%7Bjob%3D%22checkout-service%22%7D&needle=checkout-service&query_type=label_filter&label=job", nil)
	rec := httptest.NewRecorder()

	svc.handleDebug(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp debugResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, `{job="checkout-service"} | job="checkout-service"`, resp.HintQuery)
	require.Equal(t, string(hintQueryTypeLabelFilter), resp.HintQueryType)
	require.Equal(t, "job", resp.Label)
	require.Equal(t, needle, resp.Needle)
	require.True(t, resp.Summary.Correct)
}

func TestHandleDebug_SMLabelFilter(t *testing.T) {
	base := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	witnessTS := base.Add(1 * time.Minute)
	needle := "traceidvalue99"

	loki := newTestLokiServer(t, witnessTS, needle)
	defer loki.Close()

	svc := newTestService(t, newTestStore(t), loki.URL, base)
	svc.hintProvider = staticHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: witnessTS.Add(-5 * time.Second), End: witnessTS.Add(5 * time.Second), Source: "sm"},
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/correctness/debug?start=2026-02-26T10:00:00Z&end=2026-02-26T10:02:00Z&selector=%7Bjob%3D%22api%22%7D&needle=traceidvalue99&query_type=sm_label_filter&label=trace_id", nil)
	rec := httptest.NewRecorder()

	svc.handleDebug(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp debugResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, `{job="api"} | trace_id="traceidvalue99"`, resp.HintQuery)
	require.Equal(t, string(hintQueryTypeSMLabelFilter), resp.HintQueryType)
	require.Equal(t, "trace_id", resp.Label)
	require.Equal(t, needle, resp.Needle)
	require.True(t, resp.Summary.Correct)
}

func TestHandleDebug_JSONLabelFilter(t *testing.T) {
	base := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	witnessTS := base.Add(1 * time.Minute)
	needle := "grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"

	loki := newTestLokiServer(t, witnessTS, needle)
	defer loki.Close()

	svc := newTestService(t, newTestStore(t), loki.URL, base)
	svc.hintProvider = staticHintProvider{
		hints: &hintprovider.Hints{
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: witnessTS.Add(-5 * time.Second), End: witnessTS.Add(5 * time.Second), Source: "json"},
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/correctness/debug?start=2026-02-26T10:00:00Z&end=2026-02-26T10:02:00Z&selector=%7Bjob%3D%22api%22%7D&needle=grafana_slo_app-klu4xpj1w5lmbmvi8u6ec&query_type=json_label_filter&label=dashboardUID", nil)
	rec := httptest.NewRecorder()

	svc.handleDebug(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp debugResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, `{job="api"} | json | dashboardUID="grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"`, resp.HintQuery)
	require.Equal(t, string(hintQueryTypeJSONLabelFilter), resp.HintQueryType)
	require.Equal(t, "dashboardUID", resp.Label)
	require.Equal(t, needle, resp.Needle)
	require.True(t, resp.Summary.Correct)
}

func TestBuildDebugSamples_UsesSampleLookback(t *testing.T) {
	hq, err := buildDebugHintQuery(debugRequest{
		selector:  `{job="api"}`,
		needle:    "9fA81cD2Ef0077aa",
		queryType: hintQueryTypeMetricRange,
	})
	require.NoError(t, err)

	step := time.Date(2026, 2, 26, 2, 5, 0, 0, time.UTC)
	hints := []hintprovider.HintTimeRange{{
		Start: step.Add(-10 * time.Minute),
		End:   step.Add(-5 * time.Minute),
	}}
	samples := []verification.MetricSample{
		{Time: step.Add(-5 * time.Minute), Value: 1},
		{Time: step, Value: 1},
		{Time: step.Add(-5 * time.Minute), Value: 0},
	}

	got := buildDebugSamples(hq.expr, samples, hints, time.Time{})
	require.Len(t, got, 2)
	require.True(t, got[0].Covered)
	require.False(t, got[1].Covered)

	ranges := buildDebugMetricHintRanges(hq.expr, samples, hints, time.Time{})
	require.Equal(t, 1, ranges[0].EntryCount)
}

func TestBuildDebugSamples_OmitsLookbackAfterEligibleEnd(t *testing.T) {
	hq, err := buildDebugHintQuery(debugRequest{
		selector:  `{job="api"}`,
		needle:    "9fA81cD2Ef0077aa",
		queryType: hintQueryTypeMetricRange,
	})
	require.NoError(t, err)

	step := time.Date(2026, 2, 26, 2, 5, 0, 0, time.UTC)
	later := step.Add(time.Minute)
	hints := []hintprovider.HintTimeRange{{
		Start: step.Add(-5 * time.Minute),
		End:   later,
	}}
	samples := []verification.MetricSample{
		{Time: step, Value: 1},
		{Time: later, Value: 1},
	}

	got := buildDebugSamples(hq.expr, samples, hints, step)
	require.Len(t, got, 1)
	require.True(t, got[0].Time.Equal(step))
	require.True(t, got[0].Covered)

	ranges := buildDebugMetricHintRanges(hq.expr, samples, hints, step)
	require.Equal(t, 1, ranges[0].EntryCount)
}

func TestHandleDebug_MetricRange(t *testing.T) {
	start := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Minute)

	resp, metric, hints, qb := runMetricDebug(t, start, end)
	require.Equal(t, qb.ReadStart, resp.ReadStart)
	require.Equal(t, qb.ReadEnd, resp.ReadEnd)
	require.Equal(t, end, qb.ReadEnd)
	require.True(t, qb.ReadStart.Before(start))
	require.Equal(t, qb.RequestedStart, metric.start)
	require.Equal(t, qb.RequestedEnd, metric.end)
	require.Equal(t, "60", metric.step)
	require.Equal(t, model.TimeFromUnixNano(qb.ReadStart.UnixNano()), hints.from)
	require.Equal(t, model.TimeFromUnixNano(qb.ReadEnd.UnixNano()), hints.through)

	require.Empty(t, resp.Entries)
	require.Len(t, resp.Samples, 1)
	require.True(t, resp.Samples[0].Time.Equal(end))
	require.Equal(t, float64(1), resp.Samples[0].Value)
	require.True(t, resp.Samples[0].Covered)
	require.Equal(t, 1, resp.HintRanges[0].EntryCount)
	require.True(t, resp.Summary.Correct)
}

func TestHandleDebug_MetricRange_OffMinuteEnd(t *testing.T) {
	start := time.Date(2026, 2, 26, 2, 0, 10, 0, time.UTC)
	end := time.Date(2026, 2, 26, 2, 5, 10, 0, time.UTC)

	resp, metric, hints, qb := runMetricDebug(t, start, end)
	require.Equal(t, end, resp.RangeEnd)
	require.Equal(t, qb.ReadStart, resp.ReadStart)
	require.Equal(t, time.Date(2026, 2, 26, 2, 6, 0, 0, time.UTC), resp.ReadEnd)
	require.Equal(t, end, qb.RequestedEnd)
	require.Equal(t, time.Date(2026, 2, 26, 2, 6, 0, 0, time.UTC), qb.ReadEnd)
	require.Equal(t, qb.RequestedStart, metric.start)
	require.Equal(t, qb.RequestedEnd, metric.end)
	require.Equal(t, "60", metric.step)
	require.Equal(t, model.TimeFromUnixNano(qb.ReadEnd.UnixNano()), hints.through)
}

func runMetricDebug(t *testing.T, start, end time.Time) (debugResponse, *recordedMetricQuery, *debugWindowHintProvider, querybounds.QueryBounds) {
	t.Helper()
	needle := "9fA81cD2Ef0077aa"
	metric := &recordedMetricQuery{}
	loki := newTestLokiServerRecording(t, end, needle, metric)
	t.Cleanup(loki.Close)

	svc := newTestService(t, newTestStore(t), loki.URL, start)
	hints := &debugWindowHintProvider{hints: &hintprovider.Hints{
		TimeRanges: []hintprovider.HintTimeRange{{
			Start:  end.Add(-5 * time.Minute),
			End:    end,
			Source: "metric",
		}},
	}}
	svc.hintProvider = hints

	query := url.Values{}
	query.Set("start", start.Format(time.RFC3339))
	query.Set("end", end.Format(time.RFC3339))
	query.Set("selector", `{job="api"}`)
	query.Set("needle", needle)
	query.Set("query_type", "metric_range")
	req := httptest.NewRequest(http.MethodGet, "/correctness/debug?"+query.Encode(), nil)
	rec := httptest.NewRecorder()
	svc.handleDebug(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp debugResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, `sum(count_over_time({job="api"} |= "9fA81cD2Ef0077aa" [5m0s]))`, resp.HintQuery)
	require.Equal(t, string(hintQueryTypeMetricRange), resp.HintQueryType)

	expr, err := syntax.ParseExpr(resp.HintQuery)
	require.NoError(t, err)
	qb := querybounds.GetQueryBounds(start, end, time.Minute.Milliseconds(), expr, querybounds.QueryTypeMetricRange, false)
	return resp, metric, hints, qb
}

func TestBuildDebugHintQuery_MetricRange(t *testing.T) {
	hq, err := buildDebugHintQuery(debugRequest{
		selector:  `{job="api"}`,
		needle:    "9fA81cD2Ef0077aa",
		queryType: hintQueryTypeMetricRange,
	})
	require.NoError(t, err)
	require.Equal(t, hintQueryTypeMetricRange, hq.queryType)
	require.Equal(t, "9fA81cD2Ef0077aa", hq.needle)
	require.Equal(t, `sum(count_over_time({job="api"} |= "9fA81cD2Ef0077aa" [5m0s]))`, hq.query)
}

func TestParseDebugRequest_MetricRange(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/correctness/debug?start=2026-02-26T10:00:00Z&end=2026-02-26T10:02:00Z&selector=%7Bjob%3D%22api%22%7D&needle=9fA81cD2Ef0077aa&query_type=metric_range", nil)

	got, err := parseDebugRequest(req)
	require.NoError(t, err)
	require.Equal(t, hintQueryTypeMetricRange, got.queryType)
	require.Empty(t, got.label)
}

func TestParseDebugRequest_MetricInstantRejected(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/correctness/debug?start=2026-02-26T10:00:00Z&end=2026-02-26T10:02:00Z&selector=%7Bjob%3D%22api%22%7D&needle=9fA81cD2Ef0077aa&query_type=metric_instant", nil)

	_, err := parseDebugRequest(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "query_type must be")
}

func TestHandleDebug_BadRequest(t *testing.T) {
	svc := &Service{}

	t.Run("missing start", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/correctness/debug?end=2026-02-26T10:02:00Z&selector=%7Bjob%3D%22api%22%7D&needle=test", nil)
		rec := httptest.NewRecorder()

		svc.handleDebug(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "start is required")
	})

	t.Run("label filter missing label", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/correctness/debug?start=2026-02-26T10:00:00Z&end=2026-02-26T10:02:00Z&selector=%7Bjob%3D%22api%22%7D&needle=checkout-service&query_type=label_filter", nil)
		rec := httptest.NewRecorder()

		svc.handleDebug(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "label is required")
	})

	t.Run("invalid query_type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/correctness/debug?start=2026-02-26T10:00:00Z&end=2026-02-26T10:02:00Z&selector=%7Bjob%3D%22api%22%7D&needle=test&query_type=nope", nil)
		rec := httptest.NewRecorder()

		svc.handleDebug(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "query_type must be")
	})
}

func TestHandleDebug_UnsupportedQuery(t *testing.T) {
	base := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	witnessTS := base.Add(1 * time.Minute)
	needle := "9fA81cD2Ef0077aa"

	loki := newTestLokiServer(t, witnessTS, needle)
	defer loki.Close()

	svc := newTestService(t, newTestStore(t), loki.URL, base)
	svc.hintProvider = unsupportedHintProvider{}

	req := httptest.NewRequest(http.MethodGet, "/correctness/debug?start=2026-02-26T10:00:00Z&end=2026-02-26T10:02:00Z&selector=%7Bjob%3D%22api%22%7D&needle=9fA81cD2Ef0077aa", nil)
	rec := httptest.NewRecorder()

	svc.handleDebug(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "query not supported by logline index")
}

// debugWindowHintProvider records the hint-lookup window and returns a fixed hint.
type debugWindowHintProvider struct {
	from, through model.Time
	hints         *hintprovider.Hints
}

func (p *debugWindowHintProvider) ProvideHints(
	_ context.Context,
	_ string,
	_ syntax.Expr,
	from, through model.Time,
) (*hintprovider.Hints, *hintprovider.QueryStats, error) {
	p.from = from
	p.through = through
	return p.hints, nil, nil
}

type staticHintProvider struct {
	hints *hintprovider.Hints
	err   error
}

func (p staticHintProvider) ProvideHints(
	_ context.Context,
	_ string,
	_ syntax.Expr,
	_,
	_ model.Time,
) (*hintprovider.Hints, *hintprovider.QueryStats, error) {
	return p.hints, nil, p.err
}
