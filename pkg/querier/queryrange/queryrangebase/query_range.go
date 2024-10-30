package queryrangebase

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"golang.org/x/exp/maps"

	"github.com/gogo/status"
	"github.com/grafana/dskit/httpgrpc"
	jsoniter "github.com/json-iterator/go"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/timestamp"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
)

// StatusSuccess Prometheus success result.
const StatusSuccess = "success"

var (
	matrix = model.ValMatrix.String()
	json   = jsoniter.Config{
		EscapeHTML:             false, // No HTML in our responses.
		SortMapKeys:            true,
		ValidateJsonRawMessage: true,
	}.Froze()
	errEndBeforeStart = httpgrpc.Errorf(http.StatusBadRequest, "end timestamp must not be before start time")
	errNegativeStep   = httpgrpc.Errorf(http.StatusBadRequest, "zero or negative query resolution step widths are not accepted. Try a positive integer")
	errStepTooSmall   = httpgrpc.Errorf(http.StatusBadRequest, "exceeded maximum resolution of 11,000 points per time series. Try increasing the value of the step parameter")

	// PrometheusCodecForRangeQueries is a codec to encode and decode Loki range metric query requests and responses.
	PrometheusCodecForRangeQueries = &prometheusCodec{
		resultType: model.ValMatrix,
	}

	// PrometheusCodecForInstantQueries is a codec to encode and decode Loki range metric query requests and responses.
	PrometheusCodecForInstantQueries = &prometheusCodec{
		resultType: model.ValVector,
	}

	// Name of the cache control header.
	cacheControlHeader = "Cache-Control"
)

type prometheusCodec struct {
	// prometheusCodec is used to merge multiple response of either range (matrix) or instant queries(vector).
	// when creating empty responses during merge, it need to be aware what kind of valueType it should create with.
	// helps other middlewares to filter the correct result type.
	resultType model.ValueType
}

// WithStartEnd clones the current `PrometheusRequest` with a new `start` and `end` timestamp.
func (q *PrometheusRequest) WithStartEnd(start, end time.Time) Request {
	clone := *q
	clone.Start = start
	clone.End = end
	return &clone
}

// WithStartEndForCache implements resultscache.Request.
func (q *PrometheusRequest) WithStartEndForCache(s time.Time, e time.Time) resultscache.Request {
	clone := q.WithStartEnd(s, e).(resultscache.Request)
	return clone
}

// WithQuery clones the current `PrometheusRequest` with a new query.
func (q *PrometheusRequest) WithQuery(query string) Request {
	clone := *q
	clone.Query = query
	return &clone
}

// LogToSpan logs the current `PrometheusRequest` parameters to the specified span.
func (q *PrometheusRequest) LogToSpan(sp opentracing.Span) {
	sp.LogFields(
		otlog.String("query", q.GetQuery()),
		otlog.String("start", timestamp.Time(q.GetStart().UnixMilli()).String()),
		otlog.String("end", timestamp.Time(q.GetEnd().UnixMilli()).String()),
		otlog.Int64("step (ms)", q.GetStep()),
	)
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

func convertPrometheusResponseHeadersToPointers(h []PrometheusResponseHeader) []*PrometheusResponseHeader {
	if h == nil {
		return nil
	}

	resp := make([]*PrometheusResponseHeader, len(h))
	for i := range h {
		resp[i] = &h[i]
	}

	return resp
}

func (resp *PrometheusResponse) WithHeaders(h []PrometheusResponseHeader) Response {
	resp.Headers = convertPrometheusResponseHeadersToPointers(h)
	return resp
}

func (resp *PrometheusResponse) SetHeader(name, value string) {
	for i, h := range resp.Headers {
		if h.Name == name {
			resp.Headers[i].Values = []string{value}
			return
		}
	}

	resp.Headers = append(resp.Headers, &PrometheusResponseHeader{Name: name, Values: []string{value}})
}

// NewEmptyPrometheusResponse returns an empty successful Prometheus query range response.
func NewEmptyPrometheusResponse(v model.ValueType) *PrometheusResponse {
	return &PrometheusResponse{
		Status: StatusSuccess,
		Data: PrometheusData{
			ResultType: v.String(),
			Result:     []SampleStream{},
		},
	}
}

func (p prometheusCodec) MergeResponse(responses ...Response) (Response, error) {
	if len(responses) == 0 {
		return NewEmptyPrometheusResponse(p.resultType), nil
	}

	promResponses := make([]*PrometheusResponse, 0, len(responses))
	// we need to pass on all the headers for results cache gen numbers.
	var resultsCacheGenNumberHeaderValues []string

	for _, res := range responses {
		promResponses = append(promResponses, res.(*PrometheusResponse))
		resultsCacheGenNumberHeaderValues = append(resultsCacheGenNumberHeaderValues, getHeaderValuesWithName(res, ResultsCacheGenNumberHeaderName)...)
	}

	// Merge the responses.
	sort.Sort(byFirstTime(promResponses))

	uniqueWarnings := map[string]struct{}{}
	for _, resp := range promResponses {
		for _, w := range resp.Warnings {
			uniqueWarnings[w] = struct{}{}
		}
	}

	warnings := maps.Keys(uniqueWarnings)
	sort.Strings(warnings)

	if len(warnings) == 0 {
		// When there are no warnings, keep it nil so it can be compared against
		// the default value
		warnings = nil
	}

	response := PrometheusResponse{
		Status:   StatusSuccess,
		Warnings: warnings,
		Data: PrometheusData{
			ResultType: p.resultType.String(),
			Result:     matrixMerge(promResponses),
		},
	}

	if len(resultsCacheGenNumberHeaderValues) != 0 {
		response.Headers = []*PrometheusResponseHeader{{
			Name:   ResultsCacheGenNumberHeaderName,
			Values: resultsCacheGenNumberHeaderValues,
		}}
	}

	return &response, nil
}

func (prometheusCodec) DecodeResponse(ctx context.Context, r *http.Response, _ Request) (Response, error) {
	if r.StatusCode/100 != 2 {
		body, _ := io.ReadAll(r.Body)
		return nil, httpgrpc.Errorf(r.StatusCode, "%s", string(body))
	}
	sp, ctx := opentracing.StartSpanFromContext(ctx, "ParseQueryRangeResponse") //nolint:ineffassign,staticcheck
	defer sp.Finish()

	buf, err := bodyBuffer(r)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	sp.LogKV(otlog.Int("bytes", len(buf)))

	var resp PrometheusResponse
	if err := json.Unmarshal(buf, &resp); err != nil {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
	}

	for h, hv := range r.Header {
		resp.Headers = append(resp.Headers, &PrometheusResponseHeader{Name: h, Values: hv})
	}
	return &resp, nil
}

// Buffer can be used to read a response body.
// This allows to avoid reading the body multiple times from the `http.Response.Body`.
type Buffer interface {
	Bytes() []byte
}

func bodyBuffer(res *http.Response) ([]byte, error) {
	// Attempt to cast the response body to a Buffer and use it if possible.
	// This is because the frontend may have already read the body and buffered it.
	if buffer, ok := res.Body.(Buffer); ok {
		return buffer.Bytes(), nil
	}
	// Preallocate the buffer with the exact size so we don't waste allocations
	// while progressively growing an initial small buffer. The buffer capacity
	// is increased by MinRead to avoid extra allocations due to how ReadFrom()
	// internally works.
	buf := bytes.NewBuffer(make([]byte, 0, res.ContentLength+bytes.MinRead))
	if _, err := buf.ReadFrom(res.Body); err != nil {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
	}
	return buf.Bytes(), nil
}

// TODO(karsten): remove prometheusCodec from code base since only MergeResponse is used.
func (prometheusCodec) EncodeResponse(ctx context.Context, _ *http.Request, res Response) (*http.Response, error) {
	sp, _ := opentracing.StartSpanFromContext(ctx, "APIResponse.ToHTTPResponse")
	defer sp.Finish()

	a, ok := res.(*PrometheusResponse)
	if !ok {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "invalid response format")
	}

	sp.LogFields(otlog.Int("series", len(a.Data.Result)))

	b, err := json.Marshal(a)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error encoding response: %v", err)
	}

	sp.LogFields(otlog.Int("bytes", len(b)))

	resp := http.Response{
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:          io.NopCloser(bytes.NewBuffer(b)),
		StatusCode:    http.StatusOK,
		ContentLength: int64(len(b)),
	}
	return &resp, nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *SampleStream) UnmarshalJSON(data []byte) error {
	var stream struct {
		Metric model.Metric            `json:"metric"`
		Values []logproto.LegacySample `json:"values"`
	}
	if err := json.Unmarshal(data, &stream); err != nil {
		return err
	}
	s.Labels = logproto.FromMetricsToLabelAdapters(stream.Metric)
	s.Samples = stream.Values
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s *SampleStream) MarshalJSON() ([]byte, error) {
	stream := struct {
		Metric model.Metric            `json:"metric"`
		Values []logproto.LegacySample `json:"values"`
	}{
		Metric: logproto.FromLabelAdaptersToMetric(s.Labels),
		Values: s.Samples,
	}
	return json.Marshal(stream)
}

func matrixMerge(resps []*PrometheusResponse) []SampleStream {
	output := map[string]*SampleStream{}
	for _, resp := range resps {
		for _, stream := range resp.Data.Result {
			metric := logproto.FromLabelAdaptersToLabels(stream.Labels).String()
			existing, ok := output[metric]
			if !ok {
				existing = &SampleStream{
					Labels: stream.Labels,
				}
			}
			// We need to make sure we don't repeat samples. This causes some visualisations to be broken in Grafana.
			// The prometheus API is inclusive of start and end timestamps.
			if len(existing.Samples) > 0 && len(stream.Samples) > 0 {
				existingEndTs := existing.Samples[len(existing.Samples)-1].TimestampMs
				if existingEndTs == stream.Samples[0].TimestampMs {
					// Typically this the cases where only 1 sample point overlap,
					// so optimize with simple code.
					stream.Samples = stream.Samples[1:]
				} else if existingEndTs > stream.Samples[0].TimestampMs {
					// Overlap might be big, use heavier algorithm to remove overlap.
					stream.Samples = sliceSamples(stream.Samples, existingEndTs)
				} // else there is no overlap, yay!
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

// sliceSamples assumes given samples are sorted by timestamp in ascending order and
// return a sub slice whose first element's is the smallest timestamp that is strictly
// bigger than the given minTs. Empty slice is returned if minTs is bigger than all the
// timestamps in samples.
func sliceSamples(samples []logproto.LegacySample, minTs int64) []logproto.LegacySample {
	if len(samples) <= 0 || minTs < samples[0].TimestampMs {
		return samples
	}

	if len(samples) > 0 && minTs > samples[len(samples)-1].TimestampMs {
		return samples[len(samples):]
	}

	searchResult := sort.Search(len(samples), func(i int) bool {
		return samples[i].TimestampMs > minTs
	})

	return samples[searchResult:]
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

func decorateWithParamName(err error, field string) error {
	errTmpl := "invalid parameter %q; %v"
	if status, ok := status.FromError(err); ok {
		return httpgrpc.Errorf(int(status.Code()), errTmpl, field, status.Message())
	}
	return fmt.Errorf(errTmpl, field, err)
}
