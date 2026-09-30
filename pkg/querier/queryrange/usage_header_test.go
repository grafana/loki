package queryrange

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

func usageHeaderResponse(kind string, n int64) queryrangebase.Response {
	statistics := stats.Result{Querier: stats.Querier{Store: stats.Store{Chunk: stats.Chunk{DecompressedBytes: n}}}}
	statistics.ComputeSummary(time.Second, 0, 0)
	if kind == "logs" {
		return &LokiResponse{Status: "success", Direction: logproto.FORWARD, Limit: 100,
			Statistics: statistics, Data: LokiData{ResultType: loghttp.ResultTypeStream, Result: []logproto.Stream{
				{Labels: `{app="test"}`, Entries: []logproto.Entry{{Timestamp: time.Unix(1, 0), Line: strings.Repeat("log line ", 256)}}},
			}}}
	}
	return &LokiPromResponse{Statistics: statistics, Response: &queryrangebase.PrometheusResponse{
		Status: "success", Data: queryrangebase.PrometheusData{ResultType: kind},
	}}
}

func TestQueryUsageHeaderUsesMergedStatistics(t *testing.T) {
	for _, kind := range []string{"logs", "matrix", "vector"} {
		t.Run(kind, func(t *testing.T) {
			a, b := usageHeaderResponse(kind, 100), usageHeaderResponse(kind, 200)
			merged, err := DefaultCodec.MergeResponse(a, b)
			require.NoError(t, err)
			headers := http.Header{queryBytesProcessedHeader: []string{"999"}}
			setQueryBytesProcessedHeader(headers, merged)
			require.Equal(t, "300", headers.Get(queryBytesProcessedHeader))
		})
	}
}

func TestQueryUsageHeaderOmitsUnknownOrInvalidStatistics(t *testing.T) {
	for _, response := range []queryrangebase.Response{usageHeaderResponse("logs", -1), usageHeaderResponse("vector", -1), &LokiSeriesResponse{}, &DetectedFieldsResponse{Response: &logproto.DetectedFieldsResponse{}}} {
		headers := http.Header{}
		headers.Set(queryBytesProcessedHeader, "999")
		setQueryBytesProcessedHeader(headers, response)
		require.Empty(t, headers.Get(queryBytesProcessedHeader))
	}
}
