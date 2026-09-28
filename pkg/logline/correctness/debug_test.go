package correctness

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
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
