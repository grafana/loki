package queryrange

import (
	"bytes"
	"context"
	"io"
	"net/http"

	jsoniter "github.com/json-iterator/go"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
)

var (
	jsonStd   = jsoniter.ConfigCompatibleWithStandardLibrary
	extractor = queryrangebase.PrometheusResponseExtractor{}
)

// PrometheusExtractor implements Extractor interface
type PrometheusExtractor struct{}

// Extract wraps the original prometheus cache extractor
func (PrometheusExtractor) Extract(start, end int64, res resultscache.Response, resStart, resEnd int64) resultscache.Response {
	response := extractor.Extract(start, end, res.(*LokiPromResponse).Response, resStart, resEnd)
	return &LokiPromResponse{
		Response: response.(*queryrangebase.PrometheusResponse),
	}
}

// ResponseWithoutHeaders wraps the original prometheus caching without headers
func (PrometheusExtractor) ResponseWithoutHeaders(resp queryrangebase.Response) queryrangebase.Response {
	response := extractor.ResponseWithoutHeaders(resp.(*LokiPromResponse).Response)
	return &LokiPromResponse{
		Response: response.(*queryrangebase.PrometheusResponse),
	}
}

// encode encodes a Prometheus response and injects Loki stats.
func (p *LokiPromResponse) encode(ctx context.Context) (*http.Response, error) {
	sp := opentracing.SpanFromContext(ctx)
	var buf bytes.Buffer

	err := p.encodeTo(&buf)
	if err != nil {
		return nil, err
	}

	if sp != nil {
		sp.LogFields(otlog.Int("bytes", buf.Len()))
	}

	resp := http.Response{
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:       io.NopCloser(&buf),
		StatusCode: http.StatusOK,
	}
	return &resp, nil
}

func (p *LokiPromResponse) encodeTo(w io.Writer) error {
	var (
		b   []byte
		err error
	)

	switch p.Response.Data.ResultType {
	case loghttp.ResultTypeVector:
		b, err = p.marshalVector()
	case loghttp.ResultTypeMatrix:
		b, err = p.marshalMatrix()
	case loghttp.ResultTypeScalar:
		b, err = p.marshalScalar()
	}
	if err != nil {
		return err
	}

	_, err = w.Write(b)
	return err
}

func (p *LokiPromResponse) marshalVector() ([]byte, error) {
	vec := make(loghttp.Vector, len(p.Response.Data.Result))
	for i, v := range p.Response.Data.Result {
		lbs := make(model.LabelSet, len(v.Labels))
		for _, v := range v.Labels {
			lbs[model.LabelName(v.Name)] = model.LabelValue(v.Value)
		}
		vec[i] = model.Sample{
			Metric:    model.Metric(lbs),
			Timestamp: model.Time(v.Samples[0].TimestampMs),
			Value:     model.SampleValue(v.Samples[0].Value),
		}
	}

	return jsonStd.Marshal(struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string         `json:"resultType"`
			Result     loghttp.Vector `json:"result"`
			Statistics stats.Result   `json:"stats,omitempty"`
		} `json:"data,omitempty"`
		ErrorType string   `json:"errorType,omitempty"`
		Error     string   `json:"error,omitempty"`
		Warnings  []string `json:"warnings,omitempty"`
	}{
		Error: p.Response.Error,
		Data: struct {
			ResultType string         `json:"resultType"`
			Result     loghttp.Vector `json:"result"`
			Statistics stats.Result   `json:"stats,omitempty"`
		}{
			ResultType: loghttp.ResultTypeVector,
			Result:     vec,
			Statistics: p.Statistics,
		},
		ErrorType: p.Response.ErrorType,
		Status:    p.Response.Status,
		Warnings:  p.Response.Warnings,
	})
}

func (p *LokiPromResponse) marshalMatrix() ([]byte, error) {

	// Make sure nil is not encoded as null.
	if p.Response.Data.Result == nil {
		p.Response.Data.Result = []queryrangebase.SampleStream{}
	}

	// embed response and add statistics.
	return jsonStd.Marshal(struct {
		Status string `json:"status"`
		Data   struct {
			queryrangebase.PrometheusData
			Statistics stats.Result `json:"stats,omitempty"`
		} `json:"data,omitempty"`
		ErrorType string   `json:"errorType,omitempty"`
		Error     string   `json:"error,omitempty"`
		Warnings  []string `json:"warnings,omitempty"`
	}{
		Error: p.Response.Error,
		Data: struct {
			queryrangebase.PrometheusData
			Statistics stats.Result `json:"stats,omitempty"`
		}{
			PrometheusData: p.Response.Data,
			Statistics:     p.Statistics,
		},
		ErrorType: p.Response.ErrorType,
		Status:    p.Response.Status,
		Warnings:  p.Response.Warnings,
	})
}

func (p *LokiPromResponse) marshalScalar() ([]byte, error) {
	var scalar loghttp.Scalar

	for _, r := range p.Response.Data.Result {
		if len(r.Samples) <= 0 {
			continue
		}

		scalar = loghttp.Scalar{
			Value:     model.SampleValue(r.Samples[0].Value),
			Timestamp: model.TimeFromUnix(r.Samples[0].TimestampMs),
		}
		break
	}

	return jsonStd.Marshal(struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string         `json:"resultType"`
			Result     loghttp.Scalar `json:"result"`
			Statistics stats.Result   `json:"stats,omitempty"`
		} `json:"data,omitempty"`
		ErrorType string   `json:"errorType,omitempty"`
		Error     string   `json:"error,omitempty"`
		Warnings  []string `json:"warnings,omitempty"`
	}{
		Error: p.Response.Error,
		Data: struct {
			ResultType string         `json:"resultType"`
			Result     loghttp.Scalar `json:"result"`
			Statistics stats.Result   `json:"stats,omitempty"`
		}{
			ResultType: loghttp.ResultTypeScalar,
			Result:     scalar,
			Statistics: p.Statistics,
		},
		ErrorType: p.Response.ErrorType,
		Status:    p.Response.Status,
		Warnings:  p.Response.Warnings,
	})
}
