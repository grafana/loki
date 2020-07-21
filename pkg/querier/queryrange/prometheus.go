package queryrange

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	jsoniter "github.com/json-iterator/go"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"

	"github.com/grafana/loki/pkg/logql/stats"
)

var (
	jsonStd   = jsoniter.ConfigCompatibleWithStandardLibrary
	extractor = queryrange.PrometheusResponseExtractor{}
)

// PrometheusExtractor implements Extractor interface
type PrometheusExtractor struct{}

// Extract wraps the original prometheus cache extractor
func (PrometheusExtractor) Extract(start, end int64, from queryrange.Response) queryrange.Response {
	response := extractor.Extract(start, end, from.(*LokiPromResponse).Response)
	return &LokiPromResponse{
		Response: response.(*queryrange.PrometheusResponse),
	}
}

// ResponseWithoutHeaders wraps the original prometheus caching without headers
func (PrometheusExtractor) ResponseWithoutHeaders(resp queryrange.Response) queryrange.Response {
	response := extractor.ResponseWithoutHeaders(resp.(*LokiPromResponse).Response)
	return &LokiPromResponse{
		Response: response.(*queryrange.PrometheusResponse),
	}
}

// encode encodes a Prometheus response and injects Loki stats.
func (p *LokiPromResponse) encode(ctx context.Context) (*http.Response, error) {
	sp := opentracing.SpanFromContext(ctx)
	// embed response and add statistics.
	b, err := jsonStd.Marshal(struct {
		Status string `json:"status"`
		Data   struct {
			queryrange.PrometheusData
			Statistics stats.Result `json:"stats"`
		} `json:"data,omitempty"`
		ErrorType string `json:"errorType,omitempty"`
		Error     string `json:"error,omitempty"`
	}{
		Error: p.Response.Error,
		Data: struct {
			queryrange.PrometheusData
			Statistics stats.Result `json:"stats"`
		}{
			PrometheusData: p.Response.Data,
			Statistics:     p.Statistics,
		},
		ErrorType: p.Response.ErrorType,
		Status:    p.Response.Status,
	})
	if err != nil {
		return nil, err
	}

	if sp != nil {
		sp.LogFields(otlog.Int("bytes", len(b)))
	}

	resp := http.Response{
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:       ioutil.NopCloser(bytes.NewBuffer(b)),
		StatusCode: http.StatusOK,
	}
	return &resp, nil
}
