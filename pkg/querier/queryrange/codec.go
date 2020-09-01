package queryrange

import (
	"bytes"
	"container/heap"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sort"
	strings "strings"
	"time"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	json "github.com/json-iterator/go"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/prometheus/pkg/timestamp"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/marshal"
	marshal_legacy "github.com/grafana/loki/pkg/logql/marshal/legacy"
	"github.com/grafana/loki/pkg/logql/stats"
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

func (r *LokiRequest) WithQuery(query string) queryrange.Request {
	new := *r
	new.Query = query
	return &new
}

func (r *LokiRequest) WithShards(shards logql.Shards) *LokiRequest {
	new := *r
	new.Shards = shards.Encode()
	return &new
}

func (r *LokiRequest) LogToSpan(sp opentracing.Span) {
	sp.LogFields(
		otlog.String("query", r.GetQuery()),
		otlog.String("start", timestamp.Time(r.GetStart()).String()),
		otlog.String("end", timestamp.Time(r.GetEnd()).String()),
		otlog.Int64("step (ms)", r.GetStep()),
		otlog.Int64("limit", int64(r.GetLimit())),
		otlog.String("direction", r.GetDirection().String()),
		otlog.String("shards", strings.Join(r.GetShards(), ",")),
	)
}

func (*LokiRequest) GetCachingOptions() (res queryrange.CachingOptions) { return }

func (r *LokiSeriesRequest) GetEnd() int64 {
	return r.EndTs.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}

func (r *LokiSeriesRequest) GetStart() int64 {
	return r.StartTs.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}

func (r *LokiSeriesRequest) WithStartEnd(s int64, e int64) queryrange.Request {
	new := *r
	new.StartTs = time.Unix(0, s*int64(time.Millisecond))
	new.EndTs = time.Unix(0, e*int64(time.Millisecond))
	return &new
}

func (r *LokiSeriesRequest) WithQuery(query string) queryrange.Request {
	new := *r
	return &new
}

func (r *LokiSeriesRequest) GetQuery() string {
	return ""
}

func (r *LokiSeriesRequest) GetStep() int64 {
	return 0
}

func (r *LokiSeriesRequest) LogToSpan(sp opentracing.Span) {
	sp.LogFields(
		otlog.String("matchers", strings.Join(r.GetMatch(), ",")),
		otlog.String("start", timestamp.Time(r.GetStart()).String()),
		otlog.String("end", timestamp.Time(r.GetEnd()).String()),
	)
}

func (*LokiSeriesRequest) GetCachingOptions() (res queryrange.CachingOptions) { return }

func (r *LokiLabelNamesRequest) GetEnd() int64 {
	return r.EndTs.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}

func (r *LokiLabelNamesRequest) GetStart() int64 {
	return r.StartTs.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}

func (r *LokiLabelNamesRequest) WithStartEnd(s int64, e int64) queryrange.Request {
	new := *r
	new.StartTs = time.Unix(0, s*int64(time.Millisecond))
	new.EndTs = time.Unix(0, e*int64(time.Millisecond))
	return &new
}

func (r *LokiLabelNamesRequest) WithQuery(query string) queryrange.Request {
	new := *r
	return &new
}

func (r *LokiLabelNamesRequest) GetQuery() string {
	return ""
}

func (r *LokiLabelNamesRequest) GetStep() int64 {
	return 0
}

func (r *LokiLabelNamesRequest) LogToSpan(sp opentracing.Span) {
	sp.LogFields(
		otlog.String("start", timestamp.Time(r.GetStart()).String()),
		otlog.String("end", timestamp.Time(r.GetEnd()).String()),
	)
}

func (*LokiLabelNamesRequest) GetCachingOptions() (res queryrange.CachingOptions) { return }

func (codec) DecodeRequest(_ context.Context, r *http.Request) (queryrange.Request, error) {
	if err := r.ParseForm(); err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	switch op := getOperation(r.URL.Path); op {
	case QueryRangeOp:
		req, err := loghttp.ParseRangeQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
		return &LokiRequest{
			Query:     req.Query,
			Limit:     req.Limit,
			Direction: req.Direction,
			StartTs:   req.Start.UTC(),
			EndTs:     req.End.UTC(),
			// GetStep must return milliseconds
			Step:   int64(req.Step) / 1e6,
			Path:   r.URL.Path,
			Shards: req.Shards,
		}, nil
	case SeriesOp:
		req, err := loghttp.ParseSeriesQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
		return &LokiSeriesRequest{
			Match:   req.Groups,
			StartTs: req.Start.UTC(),
			EndTs:   req.End.UTC(),
			Path:    r.URL.Path,
		}, nil
	case LabelNamesOp:
		req, err := loghttp.ParseLabelQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
		return &LokiLabelNamesRequest{
			StartTs: *req.Start,
			EndTs:   *req.End,
			Path:    r.URL.Path,
		}, nil
	default:
		return nil, httpgrpc.Errorf(http.StatusBadRequest, fmt.Sprintf("unknown request path: %s", r.URL.Path))
	}

}

func (codec) EncodeRequest(ctx context.Context, r queryrange.Request) (*http.Request, error) {
	switch request := r.(type) {
	case *LokiRequest:
		params := url.Values{
			"start":     []string{fmt.Sprintf("%d", request.StartTs.UnixNano())},
			"end":       []string{fmt.Sprintf("%d", request.EndTs.UnixNano())},
			"query":     []string{request.Query},
			"direction": []string{request.Direction.String()},
			"limit":     []string{fmt.Sprintf("%d", request.Limit)},
		}
		if len(request.Shards) > 0 {
			params["shards"] = request.Shards
		}
		if request.Step != 0 {
			params["step"] = []string{fmt.Sprintf("%f", float64(request.Step)/float64(1e3))}
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
	case *LokiSeriesRequest:
		params := url.Values{
			"start":   []string{fmt.Sprintf("%d", request.StartTs.UnixNano())},
			"end":     []string{fmt.Sprintf("%d", request.EndTs.UnixNano())},
			"match[]": request.Match,
		}

		u := &url.URL{
			Path:     "/loki/api/v1/series",
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
	case *LokiLabelNamesRequest:
		params := url.Values{
			"start": []string{fmt.Sprintf("%d", request.StartTs.UnixNano())},
			"end":   []string{fmt.Sprintf("%d", request.EndTs.UnixNano())},
		}

		u := &url.URL{
			Path:     "/loki/api/v1/labels",
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
	default:
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "invalid request format")
	}
}

func (codec) DecodeResponse(ctx context.Context, r *http.Response, req queryrange.Request) (queryrange.Response, error) {
	if r.StatusCode/100 != 2 {
		body, _ := ioutil.ReadAll(r.Body)
		return nil, httpgrpc.Errorf(r.StatusCode, string(body))
	}

	sp, _ := opentracing.StartSpanFromContext(ctx, "codec.DecodeResponse")
	defer sp.Finish()

	buf, err := ioutil.ReadAll(r.Body)
	if err != nil {
		sp.LogFields(otlog.Error(err))
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
	}

	sp.LogFields(otlog.Int("bytes", len(buf)))

	switch req := req.(type) {
	case *LokiSeriesRequest:
		var resp loghttp.SeriesResponse
		if err := json.Unmarshal(buf, &resp); err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
		}

		data := make([]logproto.SeriesIdentifier, 0, len(resp.Data))
		for _, label := range resp.Data {
			d := logproto.SeriesIdentifier{
				Labels: label.Map(),
			}
			data = append(data, d)
		}

		return &LokiSeriesResponse{
			Status:  resp.Status,
			Version: uint32(loghttp.GetVersion(req.Path)),
			Data:    data,
		}, nil
	case *LokiLabelNamesRequest:
		var resp loghttp.LabelResponse
		if err := json.Unmarshal(buf, &resp); err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
		}
		return &LokiLabelNamesResponse{
			Status:  resp.Status,
			Version: uint32(loghttp.GetVersion(req.Path)),
			Data:    resp.Data,
		}, nil
	default:
		var resp loghttp.QueryResponse
		if err := json.Unmarshal(buf, &resp); err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
		}
		switch string(resp.Data.ResultType) {
		case loghttp.ResultTypeMatrix:
			return &LokiPromResponse{
				Response: &queryrange.PrometheusResponse{
					Status: resp.Status,
					Data: queryrange.PrometheusData{
						ResultType: loghttp.ResultTypeMatrix,
						Result:     toProto(resp.Data.Result.(loghttp.Matrix)),
					},
				},
				Statistics: resp.Data.Statistics,
			}, nil
		case loghttp.ResultTypeStream:
			return &LokiResponse{
				Status:     resp.Status,
				Direction:  req.(*LokiRequest).Direction,
				Limit:      req.(*LokiRequest).Limit,
				Version:    uint32(loghttp.GetVersion(req.(*LokiRequest).Path)),
				Statistics: resp.Data.Statistics,
				Data: LokiData{
					ResultType: loghttp.ResultTypeStream,
					Result:     resp.Data.Result.(loghttp.Streams).ToProto(),
				},
			}, nil
		default:
			return nil, httpgrpc.Errorf(http.StatusBadRequest, "unsupported response type")
		}
	}

}

func (codec) EncodeResponse(ctx context.Context, res queryrange.Response) (*http.Response, error) {
	sp, _ := opentracing.StartSpanFromContext(ctx, "codec.EncodeResponse")
	defer sp.Finish()
	var buf bytes.Buffer

	switch response := res.(type) {
	case *LokiPromResponse:
		return response.encode(ctx)
	case *LokiResponse:
		streams := make([]logproto.Stream, len(response.Data.Result))

		for i, stream := range response.Data.Result {
			streams[i] = logproto.Stream{
				Labels:  stream.Labels,
				Entries: stream.Entries,
			}
		}
		result := logql.Result{
			Data:       logql.Streams(streams),
			Statistics: response.Statistics,
		}
		if loghttp.Version(response.Version) == loghttp.VersionLegacy {
			if err := marshal_legacy.WriteQueryResponseJSON(result, &buf); err != nil {
				return nil, err
			}
		} else {
			if err := marshal.WriteQueryResponseJSON(result, &buf); err != nil {
				return nil, err
			}
		}

	case *LokiSeriesResponse:
		result := logproto.SeriesResponse{
			Series: response.Data,
		}
		if err := marshal.WriteSeriesResponseJSON(result, &buf); err != nil {
			return nil, err
		}
	case *LokiLabelNamesResponse:
		if loghttp.Version(response.Version) == loghttp.VersionLegacy {
			if err := marshal_legacy.WriteLabelResponseJSON(logproto.LabelResponse{Values: response.Data}, &buf); err != nil {
				return nil, err
			}
		} else {
			if err := marshal.WriteLabelResponseJSON(logproto.LabelResponse{Values: response.Data}, &buf); err != nil {
				return nil, err
			}
		}
	default:
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "invalid response format")
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
	var mergedStats stats.Result
	switch responses[0].(type) {
	case *LokiPromResponse:

		promResponses := make([]queryrange.Response, 0, len(responses))
		for _, res := range responses {
			mergedStats.Merge(res.(*LokiPromResponse).Statistics)
			promResponses = append(promResponses, res.(*LokiPromResponse).Response)
		}
		promRes, err := queryrange.PrometheusCodec.MergeResponse(promResponses...)
		if err != nil {
			return nil, err
		}
		return &LokiPromResponse{
			Response:   promRes.(*queryrange.PrometheusResponse),
			Statistics: mergedStats,
		}, nil
	case *LokiResponse:
		lokiRes := responses[0].(*LokiResponse)

		lokiResponses := make([]*LokiResponse, 0, len(responses))
		for _, res := range responses {
			lokiResult := res.(*LokiResponse)
			mergedStats.Merge(lokiResult.Statistics)
			lokiResponses = append(lokiResponses, lokiResult)
		}

		return &LokiResponse{
			Status:     loghttp.QueryStatusSuccess,
			Direction:  lokiRes.Direction,
			Limit:      lokiRes.Limit,
			Version:    lokiRes.Version,
			ErrorType:  lokiRes.ErrorType,
			Error:      lokiRes.Error,
			Statistics: mergedStats,
			Data: LokiData{
				ResultType: loghttp.ResultTypeStream,
				Result:     mergeOrderedNonOverlappingStreams(lokiResponses, lokiRes.Limit, lokiRes.Direction),
			},
		}, nil
	case *LokiSeriesResponse:
		lokiSeriesRes := responses[0].(*LokiSeriesResponse)

		var lokiSeriesData []logproto.SeriesIdentifier
		uniqueSeries := make(map[string]struct{})

		// only unique series should be merged
		for _, res := range responses {
			lokiResult := res.(*LokiSeriesResponse)
			for _, series := range lokiResult.Data {
				if _, ok := uniqueSeries[series.String()]; !ok {
					lokiSeriesData = append(lokiSeriesData, series)
					uniqueSeries[series.String()] = struct{}{}
				}

			}
		}

		return &LokiSeriesResponse{
			Status:  lokiSeriesRes.Status,
			Version: lokiSeriesRes.Version,
			Data:    lokiSeriesData,
		}, nil
	case *LokiLabelNamesResponse:
		labelNameRes := responses[0].(*LokiLabelNamesResponse)
		uniqueNames := make(map[string]struct{})
		names := []string{}

		// only unique name should be merged
		for _, res := range responses {
			lokiResult := res.(*LokiLabelNamesResponse)
			for _, labelName := range lokiResult.Data {
				if _, ok := uniqueNames[labelName]; !ok {
					names = append(names, labelName)
					uniqueNames[labelName] = struct{}{}
				}

			}
		}

		return &LokiLabelNamesResponse{
			Status:  labelNameRes.Status,
			Version: labelNameRes.Version,
			Data:    names,
		}, nil
	default:
		return nil, errors.New("unknown response in merging responses")
	}
}

// mergeOrderedNonOverlappingStreams merges a set of ordered, nonoverlapping responses by concatenating matching streams then running them through a heap to pull out limit values
func mergeOrderedNonOverlappingStreams(resps []*LokiResponse, limit uint32, direction logproto.Direction) []logproto.Stream {

	var total int

	// turn resps -> map[labels] []entries
	groups := make(map[string]*byDir)
	for _, resp := range resps {
		for _, stream := range resp.Data.Result {
			s, ok := groups[stream.Labels]
			if !ok {
				s = &byDir{
					direction: direction,
					labels:    stream.Labels,
				}
				groups[stream.Labels] = s
			}

			s.markers = append(s.markers, stream.Entries)
			total += len(stream.Entries)
		}

		// optimization: since limit has been reached, no need to append entries from subsequent responses
		if total >= int(limit) {
			break
		}
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	if direction == logproto.BACKWARD {
		sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	} else {
		sort.Strings(keys)
	}

	// escape hatch, can just return all the streams
	if total <= int(limit) {
		results := make([]logproto.Stream, 0, len(keys))
		for _, key := range keys {
			results = append(results, logproto.Stream{
				Labels:  key,
				Entries: groups[key].merge(),
			})
		}
		return results
	}

	pq := &priorityqueue{
		direction: direction,
	}

	for _, key := range keys {
		stream := &logproto.Stream{
			Labels:  key,
			Entries: groups[key].merge(),
		}
		if len(stream.Entries) > 0 {
			pq.streams = append(pq.streams, stream)
		}
	}

	heap.Init(pq)

	resultDict := make(map[string]*logproto.Stream)

	// we want the min(limit, num_entries)
	for i := 0; i < int(limit) && pq.Len() > 0; i++ {
		// grab the next entry off the queue. This will be a stream (to preserve labels) with one entry.
		next := heap.Pop(pq).(*logproto.Stream)

		s, ok := resultDict[next.Labels]
		if !ok {
			s = &logproto.Stream{
				Labels:  next.Labels,
				Entries: make([]logproto.Entry, 0, int(limit)/len(keys)), // allocation hack -- assume uniform distribution across labels
			}
			resultDict[next.Labels] = s
		}
		// TODO: make allocation friendly
		s.Entries = append(s.Entries, next.Entries...)
	}

	results := make([]logproto.Stream, 0, len(resultDict))
	for _, key := range keys {
		stream, ok := resultDict[key]
		if ok {
			results = append(results, *stream)
		}
	}

	return results

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

func (res LokiResponse) Count() int64 {
	var result int64
	for _, s := range res.Data.Result {
		result += int64(len(s.Entries))
	}
	return result

}

type paramsWrapper struct {
	*LokiRequest
}

func paramsFromRequest(req queryrange.Request) *paramsWrapper {
	return &paramsWrapper{
		LokiRequest: req.(*LokiRequest),
	}
}

func (p paramsWrapper) Query() string {
	return p.LokiRequest.Query
}
func (p paramsWrapper) Start() time.Time {
	return p.StartTs
}
func (p paramsWrapper) End() time.Time {
	return p.EndTs
}
func (p paramsWrapper) Step() time.Duration {
	return time.Duration(p.LokiRequest.Step * 1e6)
}
func (p paramsWrapper) Interval() time.Duration { return 0 }
func (p paramsWrapper) Direction() logproto.Direction {
	return p.LokiRequest.Direction
}
func (p paramsWrapper) Limit() uint32 { return p.LokiRequest.Limit }
func (p paramsWrapper) Shards() []string {
	return p.LokiRequest.Shards
}
