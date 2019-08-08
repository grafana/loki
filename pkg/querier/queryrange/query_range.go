package queryrange

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/request"

	jsoniter "github.com/json-iterator/go"
	opentracing "github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/cortexproject/cortex/pkg/ingester/client"
)

const statusSuccess = "success"

var (
	matrix = model.ValMatrix.String()
	json   = jsoniter.ConfigCompatibleWithStandardLibrary
)

func parseRequest(r *http.Request) (*Request, error) {
	var result Request
	req, err := request.ParseRangeQuery(r)
	if err != nil {
		return nil, err
	}
	result.Limit = req.Limit
	result.Query = req.Query
	result.Start = req.Start
	result.End = req.End
	result.Direction = req.Direction
	result.Path = r.URL.Path
	return &result, nil
}

func (q Request) copy() Request {
	return q
}

func (q Request) toHTTPRequest(ctx context.Context) (*http.Request, error) {
	direction := "FORWARD"
	if q.Direction == logproto.BACKWARD {
		direction = "BACKWARD"
	}
	params := url.Values{
		"start":     []string{encodeTime(q.Start)},
		"end":       []string{encodeTime(q.End)},
		"step":      []string{encodeDurationMs(q.Step)},
		"query":     []string{q.Query},
		"direction": []string{direction},
	}
	u := &url.URL{
		Path:     q.Path,
		RawQuery: params.Encode(),
	}
	req := &http.Request{
		Method:     "GET",
		RequestURI: u.String(), // This is what the httpgrpc code looks at.
		URL:        u,
		Body:       http.NoBody,
		Header:     http.Header{},
	}

	return req.WithContext(ctx), nil
}

// LogToSpan writes information about this request to the OpenTracing span
// in the context, if there is one.
func (q Request) LogToSpan(ctx context.Context) {
	if span := opentracing.SpanFromContext(ctx); span != nil {
		span.LogFields(otlog.String("query", q.Query),
			otlog.String("start", q.Start.String()),
			otlog.String("end", q.End.String()),
			otlog.Float64("step (s)", q.Step.Seconds()))
	}
}

func encodeTime(t int64) string {
	f := float64(t) / 1.0e3
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func encodeDurationMs(d int64) string {
	return strconv.FormatFloat(float64(d)/float64(time.Second/time.Millisecond), 'f', -1, 64)
}

func parseResponse(ctx context.Context, r *http.Response) (*APIResponse, error) {
	if r.StatusCode/100 != 2 {
		body, _ := ioutil.ReadAll(r.Body)
		return nil, httpgrpc.Errorf(r.StatusCode, string(body))
	}

	sp, _ := opentracing.StartSpanFromContext(ctx, "ParseQueryRangeResponse")
	defer sp.Finish()

	buf, err := ioutil.ReadAll(r.Body)
	if err != nil {
		sp.LogFields(otlog.Error(err))
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
	}

	sp.LogFields(otlog.Int("bytes", len(buf)))

	var resp APIResponse // todo parse result and explode into streams
	if err := json.Unmarshal(buf, &resp); err != nil {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
	}
	return &resp, nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *SampleStream) UnmarshalJSON(data []byte) error {
	var stream struct {
		Metric model.Metric    `json:"metric"`
		Values []client.Sample `json:"values"`
	}
	if err := json.Unmarshal(data, &stream); err != nil {
		return err
	}
	s.Labels = client.FromMetricsToLabelAdapters(stream.Metric)
	s.Samples = stream.Values
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *SampleStream) MarshalJSON() ([]byte, error) {
	stream := struct {
		Metric model.Metric    `json:"metric"`
		Values []client.Sample `json:"values"`
	}{
		Metric: client.FromLabelAdaptersToMetric(s.Labels),
		Values: s.Samples,
	}
	return json.Marshal(stream)
}

func (a *APIResponse) toHTTPResponse(ctx context.Context) (*http.Response, error) {
	sp, _ := opentracing.StartSpanFromContext(ctx, "APIResponse.ToHTTPResponse")
	defer sp.Finish()

	b, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}

	sp.LogFields(otlog.Int("bytes", len(b)))

	resp := http.Response{
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:       ioutil.NopCloser(bytes.NewBuffer(b)),
		StatusCode: http.StatusOK,
	}
	return &resp, nil
}

func extractMatrix(start, end int64, matrix []SampleStream) []SampleStream {
	result := make([]SampleStream, 0, len(matrix))
	for _, stream := range matrix {
		extracted, ok := extractSampleStream(start, end, stream)
		if ok {
			result = append(result, extracted)
		}
	}
	return result
}

func extractSampleStream(start, end int64, stream SampleStream) (SampleStream, bool) {
	result := SampleStream{
		Labels:  stream.Labels,
		Samples: make([]client.Sample, 0, len(stream.Samples)),
	}
	for _, sample := range stream.Samples {
		if start <= sample.TimestampMs && sample.TimestampMs <= end {
			result.Samples = append(result.Samples, sample)
		}
	}
	if len(result.Samples) == 0 {
		return SampleStream{}, false
	}
	return result, true
}

func mergeAPIResponses(responses []*APIResponse) (*APIResponse, error) {
	// Merge the responses.
	sort.Sort(byFirstTime(responses))

	if len(responses) == 0 {
		return &APIResponse{
			Status: statusSuccess,
		}, nil
	}

	return &APIResponse{
		Status: statusSuccess,
		Data: Response{
			ResultType: model.ValMatrix.String(),
			Result:     matrixMerge(responses),
		},
	}, nil
}

type byFirstTime []*APIResponse

func (a byFirstTime) Len() int           { return len(a) }
func (a byFirstTime) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byFirstTime) Less(i, j int) bool { return minTime(a[i]) < minTime(a[j]) }

func minTime(resp *APIResponse) int64 {
	result := resp.Data.Samples
	if len(result) == 0 {
		return -1
	}
	if len(result[0].Samples) == 0 {
		return -1
	}
	return result[0].Samples[0].TimestampMs
}

func matrixMerge(resps []*APIResponse) []SampleStream {
	output := map[string]*SampleStream{}
	for _, resp := range resps {
		for _, stream := range resp.Data.Samples {
			metric := client.FromLabelAdaptersToLabels(stream.Labels).String()
			existing, ok := output[metric]
			if !ok {
				existing = &SampleStream{
					Labels: stream.Labels,
				}
			}
			// We need to make sure we don't repeat samples. This causes some visualisations to be broken in Grafana.
			// The prometheus API is inclusive of start and end timestamps.
			if len(existing.Samples) > 0 && len(stream.Samples) > 0 {
				if existing.Samples[len(existing.Samples)-1].TimestampMs == stream.Samples[0].TimestampMs {
					stream.Samples = stream.Samples[1:]
				}
			}
			existing.Samples = append(existing.Samples, stream.Samples...)
			output[metric] = existing
		}
	}

	keys := make([]string, 0, len(output))
	for key := range output {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]SampleStream, 0, len(output))
	for _, key := range keys {
		result = append(result, *output[key])
	}

	return result
}
