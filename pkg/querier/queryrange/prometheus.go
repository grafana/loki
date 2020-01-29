package queryrange

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/logql/stats"
	jsoniter "github.com/json-iterator/go"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
)

var jsonStd = jsoniter.ConfigCompatibleWithStandardLibrary

// PrometheusResponse is the same as queryrange.PrometheusResponse but with statistics.
// While this struct still implements proto.Message via embedded struct the statistics property won't be serialize.
// We currently don't use the proto serialization on queryrange.Response, the frontend actually proto serialize
// the http response.
type PrometheusResponse struct {
	*queryrange.PrometheusResponse
	Statistics stats.Result `json:"statistics"`
}

// prometheusResponseExtractor wraps the original prometheus cache extractor.
// Statistics are discarded when using cache entries.
var prometheusResponseExtractor = queryrange.ExtractorFunc(func(start, end int64, from queryrange.Response) queryrange.Response {
	return &PrometheusResponse{
		PrometheusResponse: queryrange.PrometheusResponseExtractor.
			Extract(start, end, from.(*PrometheusResponse)).(*queryrange.PrometheusResponse),
	}
})

func (p *PrometheusResponse) encode(ctx context.Context) (*http.Response, error) {
	sp := opentracing.SpanFromContext(ctx)
	b, err := jsonStd.Marshal(p)
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
