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

// prometheusResponseExtractor wraps the original prometheus cache extractor.
// Statistics are discarded when using cache entries.
var prometheusResponseExtractor = queryrange.ExtractorFunc(func(start, end int64, from queryrange.Response) queryrange.Response {
	return &LokiPromResponse{
		Response: queryrange.PrometheusResponseExtractor.
			Extract(start, end, from.(*LokiPromResponse).Response).(*queryrange.PrometheusResponse),
	}
})

func (p *LokiPromResponse) encode(ctx context.Context) (*http.Response, error) {
	sp := opentracing.SpanFromContext(ctx)

	// embed response and add statistics.
	b, err := jsonStd.Marshal(struct {
		*queryrange.PrometheusResponse
		Statistics stats.Result `json:"statistics"`
	}{
		PrometheusResponse: p.Response,
		Statistics:         p.Statistics,
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
