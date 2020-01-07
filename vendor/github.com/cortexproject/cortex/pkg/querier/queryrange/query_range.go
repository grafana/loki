package queryrange

import (
	"bytes"
	"context"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/gogo/protobuf/proto"
	jsoniter "github.com/json-iterator/go"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/timestamp"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/cortexproject/cortex/pkg/ingester/client"
)

const statusSuccess = "success"

var (
	matrix            = model.ValMatrix.String()
	json              = jsoniter.ConfigCompatibleWithStandardLibrary
	errEndBeforeStart = httpgrpc.Errorf(http.StatusBadRequest, "end timestamp must not be before start time")
	errNegativeStep   = httpgrpc.Errorf(http.StatusBadRequest, "zero or negative query resolution step widths are not accepted. Try a positive integer")
	errStepTooSmall   = httpgrpc.Errorf(http.StatusBadRequest, "exceeded maximum resolution of 11,000 points per timeseries. Try decreasing the query resolution (?step=XX)")

	// PrometheusCodec is a codec to encode and decode Prometheus query range requests and responses.
	PrometheusCodec Codec = &prometheusCodec{}
)

// Codec is used to encode/decode query range requests and responses so they can be passed down to middlewares.
type Codec interface {
	Merger
	// DecodeRequest decodes a Request from an http request.
	DecodeRequest(context.Context, *http.Request) (Request, error)
	// DecodeResponse decodes a Response from an http response.
	// The original request is also passed as a parameter this is useful for implementation that needs the request
	// to merge result or build the result correctly.
	DecodeResponse(context.Context, *http.Response, Request) (Response, error)
	// EncodeRequest encodes a Request into an http request.
	EncodeRequest(context.Context, Request) (*http.Request, error)
	// EncodeResponse encodes a Response into an http response.
	EncodeResponse(context.Context, Response) (*http.Response, error)
}

// Merger is used by middlewares making multiple requests to merge back all responses into a single one.
type Merger interface {
	// MergeResponse merges responses from multiple requests into a single Response
	MergeResponse(...Response) (Response, error)
}

// Request represents a query range request that can be process by middlewares.
type Request interface {
	// GetStart returns the start timestamp of the request in milliseconds.
	GetStart() int64
	// GetEnd returns the end timestamp of the request in milliseconds.
	GetEnd() int64
	// GetStep returns the step of the request in milliseconds.
	GetStep() int64
	// GetQuery returns the query of the request.
	GetQuery() string
	// WithStartEnd clone the current request with different start and end timestamp.
	WithStartEnd(int64, int64) Request
	proto.Message
}

// Response represents a query range response.
type Response interface {
	proto.Message
}

// LogToSpan writes information about this request to the OpenTracing span
// in the context, if there is one.
func LogToSpan(ctx context.Context, r Request) {
	if span := opentracing.SpanFromContext(ctx); span != nil {
		span.LogFields(otlog.String("query", r.GetQuery()),
			otlog.String("start", timestamp.Time(r.GetStart()).String()),
			otlog.String("end", timestamp.Time(r.GetEnd()).String()),
			otlog.Int64("step (ms)", r.GetStep()))
	}
}

type prometheusCodec struct{}

// WithStartEnd clones the current `PrometheusRequest` with a new `start` and `end` timestamp.
func (q *PrometheusRequest) WithStartEnd(start int64, end int64) Request {
	new := *q
	new.Start = start
	new.End = end
	return &new
}

type byFirstTime []*PrometheusResponse

func (a byFirstTime) Len() int           { return len(a) }
func (a byFirstTime) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byFirstTime) Less(i, j int) bool { return a[i].minTime() < a[j].minTime() }

func (resp *PrometheusResponse) minTime() int64 {
	result := resp.Data.Result
	if len(result) == 0 {
		return -1
	}
	if len(result[0].Samples) == 0 {
		return -1
	}
	return result[0].Samples[0].TimestampMs
}

func (prometheusCodec) MergeResponse(responses ...Response) (Response, error) {
	promResponses := make([]*PrometheusResponse, 0, len(responses))
	for _, res := range responses {
		promResponses = append(promResponses, res.(*PrometheusResponse))
	}
	// Merge the responses.
	sort.Sort(byFirstTime(promResponses))

	if len(promResponses) == 0 {
		return &PrometheusResponse{
			Status: statusSuccess,
		}, nil
	}

	return &PrometheusResponse{
		Status: statusSuccess,
		Data: PrometheusData{
			ResultType: model.ValMatrix.String(),
			Result:     matrixMerge(promResponses),
		},
	}, nil
}

func (prometheusCodec) DecodeRequest(_ context.Context, r *http.Request) (Request, error) {
	var result PrometheusRequest
	var err error
	result.Start, err = ParseTime(r.FormValue("start"))
	if err != nil {
		return nil, err
	}

	result.End, err = ParseTime(r.FormValue("end"))
	if err != nil {
		return nil, err
	}

	if result.End < result.Start {
		return nil, errEndBeforeStart
	}

	result.Step, err = parseDurationMs(r.FormValue("step"))
	if err != nil {
		return nil, err
	}

	if result.Step <= 0 {
		return nil, errNegativeStep
	}

	// For safety, limit the number of returned points per timeseries.
	// This is sufficient for 60s resolution for a week or 1h resolution for a year.
	if (result.End-result.Start)/result.Step > 11000 {
		return nil, errStepTooSmall
	}

	result.Query = r.FormValue("query")
	result.Path = r.URL.Path
	return &result, nil
}

func (prometheusCodec) EncodeRequest(ctx context.Context, r Request) (*http.Request, error) {
	promReq, ok := r.(*PrometheusRequest)
	if !ok {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "invalid request format")
	}
	params := url.Values{
		"start": []string{encodeTime(promReq.Start)},
		"end":   []string{encodeTime(promReq.End)},
		"step":  []string{encodeDurationMs(promReq.Step)},
		"query": []string{promReq.Query},
	}
	u := &url.URL{
		Path:     promReq.Path,
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

func (prometheusCodec) DecodeResponse(ctx context.Context, r *http.Response, _ Request) (Response, error) {
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

	var resp PrometheusResponse
	if err := json.Unmarshal(buf, &resp); err != nil {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
	}
	return &resp, nil
}

func (prometheusCodec) EncodeResponse(ctx context.Context, res Response) (*http.Response, error) {
	sp, _ := opentracing.StartSpanFromContext(ctx, "APIResponse.ToHTTPResponse")
	defer sp.Finish()

	a, ok := res.(*PrometheusResponse)
	if !ok {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "invalid response format")
	}

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

func matrixMerge(resps []*PrometheusResponse) []SampleStream {
	output := map[string]*SampleStream{}
	for _, resp := range resps {
		for _, stream := range resp.Data.Result {
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

// ParseTime parses the string into an int64, milliseconds since epoch.
func ParseTime(s string) (int64, error) {
	if t, err := strconv.ParseFloat(s, 64); err == nil {
		s, ns := math.Modf(t)
		tm := time.Unix(int64(s), int64(ns*float64(time.Second)))
		return tm.UnixNano() / int64(time.Millisecond/time.Nanosecond), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UnixNano() / int64(time.Millisecond/time.Nanosecond), nil
	}
	return 0, httpgrpc.Errorf(http.StatusBadRequest, "cannot parse %q to a valid timestamp", s)
}

func parseDurationMs(s string) (int64, error) {
	if d, err := strconv.ParseFloat(s, 64); err == nil {
		ts := d * float64(time.Second/time.Millisecond)
		if ts > float64(math.MaxInt64) || ts < float64(math.MinInt64) {
			return 0, httpgrpc.Errorf(http.StatusBadRequest, "cannot parse %q to a valid duration. It overflows int64", s)
		}
		return int64(ts), nil
	}
	if d, err := model.ParseDuration(s); err == nil {
		return int64(d) / int64(time.Millisecond/time.Nanosecond), nil
	}
	return 0, httpgrpc.Errorf(http.StatusBadRequest, "cannot parse %q to a valid duration", s)
}

func encodeTime(t int64) string {
	f := float64(t) / 1.0e3
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func encodeDurationMs(d int64) string {
	return strconv.FormatFloat(float64(d)/float64(time.Second/time.Millisecond), 'f', -1, 64)
}
