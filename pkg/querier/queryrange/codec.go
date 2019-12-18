package queryrange

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/marshal"
	marshal_legacy "github.com/grafana/loki/pkg/logql/marshal/legacy"
	json "github.com/json-iterator/go"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/weaveworks/common/httpgrpc"
)

var lokiCodec = &codec{}

type codec struct{}

func (r *LokiRequest) GetEnd() int64 {
	return r.EndTs.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}

func (r *LokiRequest) GetStart() int64 {
	return r.StartTs.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}

func (r *LokiRequest) WithStartEnd(s int64, e int64) queryrange.Request {
	new := *r
	new.StartTs = time.Unix(0, s*int64(time.Millisecond))
	new.EndTs = time.Unix(0, e*int64(time.Millisecond))
	return &new
}

func (codec) DecodeRequest(_ context.Context, r *http.Request) (queryrange.Request, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	req, err := loghttp.ParseRangeQuery(r)
	if err != nil {
		return nil, err
	}
	return &LokiRequest{
		Query:     req.Query,
		Limit:     req.Limit,
		Direction: req.Direction,
		StartTs:   req.Start.UTC(),
		EndTs:     req.End.UTC(),
		// GetStep must return milliseconds
		Step: int64(req.Step) / 1e6,
		Path: r.URL.Path,
	}, nil
}

func (codec) EncodeRequest(ctx context.Context, r queryrange.Request) (*http.Request, error) {
	lokiReq, ok := r.(*LokiRequest)
	if !ok {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "invalid request format")
	}
	params := url.Values{
		"start":     []string{fmt.Sprintf("%d", lokiReq.StartTs.UnixNano())},
		"end":       []string{fmt.Sprintf("%d", lokiReq.EndTs.UnixNano())},
		"query":     []string{lokiReq.Query},
		"direction": []string{lokiReq.Direction.String()},
		"limit":     []string{fmt.Sprintf("%d", lokiReq.Limit)},
	}
	if lokiReq.Step != 0 {
		params["step"] = []string{fmt.Sprintf("%f", float64(lokiReq.Step)/float64(1e3))}
	}
	u := &url.URL{
		// the request could come /api/prom/query but we want to only use the new api.
		Path:     "/loki/api/v1/query_range",
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

func (codec) DecodeResponse(ctx context.Context, r *http.Response, req queryrange.Request) (queryrange.Response, error) {
	if r.StatusCode/100 != 2 {
		body, _ := ioutil.ReadAll(r.Body)
		return nil, httpgrpc.Errorf(r.StatusCode, string(body))
	}

	sp, _ := opentracing.StartSpanFromContext(ctx, "DecodeResponse")
	defer sp.Finish()

	buf, err := ioutil.ReadAll(r.Body)
	if err != nil {
		sp.LogFields(otlog.Error(err))
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
	}

	sp.LogFields(otlog.Int("bytes", len(buf)))

	var resp loghttp.QueryResponse
	if err := json.Unmarshal(buf, &resp); err != nil {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
	}
	if resp.Status != loghttp.QueryStatusSuccess {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error executing request: %v", resp.Status)
	}
	switch string(resp.Data.ResultType) {
	case loghttp.ResultTypeMatrix:
		return &queryrange.PrometheusResponse{
			Status: loghttp.QueryStatusSuccess,
			Data: queryrange.PrometheusData{
				ResultType: loghttp.ResultTypeMatrix,
				Result:     toProto(resp.Data.Result.(loghttp.Matrix)),
			},
		}, nil
	case loghttp.ResultTypeStream:
		return &LokiResponse{
			Status:    loghttp.QueryStatusSuccess,
			Direction: req.(*LokiRequest).Direction,
			Limit:     req.(*LokiRequest).Limit,
			Version:   uint32(loghttp.GetVersion(req.(*LokiRequest).Path)),
			Data: LokiData{
				ResultType: loghttp.ResultTypeStream,
				Result:     resp.Data.Result.(loghttp.Streams).ToProto(),
			},
		}, nil
	default:
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "unsupported response type")
	}
}

func (codec) EncodeResponse(ctx context.Context, res queryrange.Response) (*http.Response, error) {
	sp, _ := opentracing.StartSpanFromContext(ctx, "APIResponse.ToHTTPResponse")
	defer sp.Finish()

	if _, ok := res.(*queryrange.PrometheusResponse); ok {
		return queryrange.PrometheusCodec.EncodeResponse(ctx, res)
	}

	proto, ok := res.(*LokiResponse)
	if !ok {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "invalid response format")
	}

	streams := make([]*logproto.Stream, len(proto.Data.Result))

	for i, stream := range proto.Data.Result {
		streams[i] = &stream
	}
	var buf bytes.Buffer
	if loghttp.Version(proto.Version) == loghttp.VersionLegacy {
		if err := marshal_legacy.WriteQueryResponseJSON(logql.Streams(streams), &buf); err != nil {
			return nil, err
		}
	} else {
		if err := marshal.WriteQueryResponseJSON(logql.Streams(streams), &buf); err != nil {
			return nil, err
		}
	}

	sp.LogFields(otlog.Int("bytes", buf.Len()))

	resp := http.Response{
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:       ioutil.NopCloser(&buf),
		StatusCode: http.StatusOK,
	}
	return &resp, nil
}

func (codec) MergeResponse(responses ...queryrange.Response) (queryrange.Response, error) {
	if len(responses) == 0 {
		return nil, errors.New("merging responses requires at least one response")
	}
	if _, ok := responses[0].(*queryrange.PrometheusResponse); ok {
		return queryrange.PrometheusCodec.MergeResponse(responses...)
	}
	lokiRes, ok := responses[0].(*LokiResponse)
	if !ok {
		return nil, errors.New("unexpected response type while merging")
	}

	lokiResponses := make([]*LokiResponse, 0, len(responses))
	for _, res := range responses {
		lokiResponses = append(lokiResponses, res.(*LokiResponse))
	}

	return &LokiResponse{
		Status:    loghttp.QueryStatusSuccess,
		Direction: lokiRes.Direction,
		Limit:     lokiRes.Limit,
		Version:   lokiRes.Version,
		ErrorType: lokiRes.ErrorType,
		Error:     lokiRes.Error,
		Data: LokiData{
			ResultType: loghttp.ResultTypeStream,
			Result:     mergeStreams(lokiResponses, lokiRes.Limit, lokiRes.Direction),
		},
	}, nil
}

type entry struct {
	entry  logproto.Entry
	labels string
}

type byDirection struct {
	direction logproto.Direction
	entries   []entry
}

func (a byDirection) Len() int      { return len(a.entries) }
func (a byDirection) Swap(i, j int) { a.entries[i], a.entries[j] = a.entries[j], a.entries[i] }
func (a byDirection) Less(i, j int) bool {
	e1, e2 := a.entries[i], a.entries[i]
	if a.direction == logproto.BACKWARD {
		switch {
		case e1.entry.Timestamp.UnixNano() < e2.entry.Timestamp.UnixNano():
			return false
		case e1.entry.Timestamp.UnixNano() > e2.entry.Timestamp.UnixNano():
			return true
		default:
			return e1.labels > e2.labels
		}
	}
	switch {
	case e1.entry.Timestamp.UnixNano() < e2.entry.Timestamp.UnixNano():
		return true
	case e1.entry.Timestamp.UnixNano() > e2.entry.Timestamp.UnixNano():
		return false
	default:
		return e1.labels < e2.labels
	}
}

func mergeStreams(resps []*LokiResponse, limit uint32, direction logproto.Direction) []logproto.Stream {
	output := byDirection{
		direction: direction,
		entries:   []entry{},
	}
	for _, resp := range resps {
		for _, stream := range resp.Data.Result {
			for _, e := range stream.Entries {
				output.entries = append(output.entries, entry{
					entry:  e,
					labels: stream.Labels,
				})
			}
		}
	}
	sort.Sort(output)
	// limit result
	if len(output.entries) >= int(limit) {
		output.entries = output.entries[:limit]
	}

	resultDict := map[string]*logproto.Stream{}
	for _, e := range output.entries {
		stream, ok := resultDict[e.labels]
		if !ok {
			stream = &logproto.Stream{
				Labels:  e.labels,
				Entries: []logproto.Entry{},
			}
			resultDict[e.labels] = stream
		}
		stream.Entries = append(stream.Entries, e.entry)

	}
	keys := make([]string, 0, len(resultDict))
	for key := range resultDict {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]logproto.Stream, 0, len(resultDict))
	for _, key := range keys {
		result = append(result, *resultDict[key])
	}

	return result
}

func toProto(m loghttp.Matrix) []queryrange.SampleStream {
	if len(m) == 0 {
		return nil
	}
	res := make([]queryrange.SampleStream, 0, len(m))
	for _, stream := range m {
		samples := make([]client.Sample, 0, len(stream.Values))
		for _, s := range stream.Values {
			samples = append(samples, client.Sample{
				Value:       float64(s.Value),
				TimestampMs: int64(s.Timestamp),
			})
		}
		res = append(res, queryrange.SampleStream{
			Labels:  client.FromMetricsToLabelAdapters(stream.Metric),
			Samples: samples,
		})
	}
	return res
}

func (res LokiResponse) isFull() bool {
	return countEntries(res.Data.Result) >= int64(res.Limit)
}

func countEntries(streams []logproto.Stream) int64 {
	if len(streams) == 0 {
		return 0
	}
	res := int64(0)
	for _, s := range streams {
		res += int64(len(s.Entries))
	}
	return res
}
