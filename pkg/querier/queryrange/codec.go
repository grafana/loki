package queryrange

import (
	"bytes"
	"container/heap"
	"context"
	"errors"
	"fmt"
	io "io"
	"net/http"
	"net/url"
	"sort"
	strings "strings"
	"time"

	json "github.com/json-iterator/go"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	indexStats "github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/httpreq"
	"github.com/grafana/loki/pkg/util/marshal"
	marshal_legacy "github.com/grafana/loki/pkg/util/marshal/legacy"
)

var LokiCodec = &Codec{}

type Codec struct{}

func (r *LokiRequest) GetEnd() int64 {
	return r.EndTs.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}

func (r *LokiRequest) GetStart() int64 {
	return r.StartTs.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}

func (r *LokiRequest) WithStartEnd(s int64, e int64) queryrangebase.Request {
	new := *r
	new.StartTs = time.Unix(0, s*int64(time.Millisecond))
	new.EndTs = time.Unix(0, e*int64(time.Millisecond))
	return &new
}

func (r *LokiRequest) WithStartEndTime(s time.Time, e time.Time) *LokiRequest {
	new := *r
	new.StartTs = s
	new.EndTs = e
	return &new
}

func (r *LokiRequest) WithQuery(query string) queryrangebase.Request {
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
		otlog.Int64("interval (ms)", r.GetInterval()),
		otlog.Int64("limit", int64(r.GetLimit())),
		otlog.String("direction", r.GetDirection().String()),
		otlog.String("shards", strings.Join(r.GetShards(), ",")),
	)
}

func (*LokiRequest) GetCachingOptions() (res queryrangebase.CachingOptions) { return }

func (r *LokiInstantRequest) GetStep() int64 {
	return 0
}

func (r *LokiInstantRequest) GetEnd() int64 {
	return r.TimeTs.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}

func (r *LokiInstantRequest) GetStart() int64 {
	return r.TimeTs.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}

func (r *LokiInstantRequest) WithStartEnd(s int64, e int64) queryrangebase.Request {
	new := *r
	new.TimeTs = time.Unix(0, s*int64(time.Millisecond))
	return &new
}

func (r *LokiInstantRequest) WithQuery(query string) queryrangebase.Request {
	new := *r
	new.Query = query
	return &new
}

func (r *LokiInstantRequest) WithShards(shards logql.Shards) *LokiInstantRequest {
	new := *r
	new.Shards = shards.Encode()
	return &new
}

func (r *LokiInstantRequest) LogToSpan(sp opentracing.Span) {
	sp.LogFields(
		otlog.String("query", r.GetQuery()),
		otlog.String("ts", timestamp.Time(r.GetStart()).String()),
		otlog.Int64("limit", int64(r.GetLimit())),
		otlog.String("direction", r.GetDirection().String()),
		otlog.String("shards", strings.Join(r.GetShards(), ",")),
	)
}

func (*LokiInstantRequest) GetCachingOptions() (res queryrangebase.CachingOptions) { return }

func (r *LokiSeriesRequest) GetEnd() int64 {
	return r.EndTs.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}

func (r *LokiSeriesRequest) GetStart() int64 {
	return r.StartTs.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}

func (r *LokiSeriesRequest) WithStartEnd(s int64, e int64) queryrangebase.Request {
	new := *r
	new.StartTs = time.Unix(0, s*int64(time.Millisecond))
	new.EndTs = time.Unix(0, e*int64(time.Millisecond))
	return &new
}

func (r *LokiSeriesRequest) WithQuery(query string) queryrangebase.Request {
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
		otlog.String("shards", strings.Join(r.GetShards(), ",")),
	)
}

func (*LokiSeriesRequest) GetCachingOptions() (res queryrangebase.CachingOptions) { return }

func (r *LokiLabelNamesRequest) GetEnd() int64 {
	return r.EndTs.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}

func (r *LokiLabelNamesRequest) GetStart() int64 {
	return r.StartTs.UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
}

func (r *LokiLabelNamesRequest) WithStartEnd(s int64, e int64) queryrangebase.Request {
	new := *r
	new.StartTs = time.Unix(0, s*int64(time.Millisecond))
	new.EndTs = time.Unix(0, e*int64(time.Millisecond))
	return &new
}

func (r *LokiLabelNamesRequest) WithQuery(query string) queryrangebase.Request {
	new := *r
	return &new
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

func (*LokiLabelNamesRequest) GetCachingOptions() (res queryrangebase.CachingOptions) { return }

func (Codec) DecodeRequest(_ context.Context, r *http.Request, forwardHeaders []string) (queryrangebase.Request, error) {
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
			Step:      req.Step.Milliseconds(),
			Interval:  req.Interval.Milliseconds(),
			Path:      r.URL.Path,
			Shards:    req.Shards,
		}, nil
	case InstantQueryOp:
		req, err := loghttp.ParseInstantQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
		return &LokiInstantRequest{
			Query:     req.Query,
			Limit:     req.Limit,
			Direction: req.Direction,
			TimeTs:    req.Ts.UTC(),
			Path:      r.URL.Path,
			Shards:    req.Shards,
		}, nil
	case SeriesOp:
		req, err := loghttp.ParseAndValidateSeriesQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
		return &LokiSeriesRequest{
			Match:   req.Groups,
			StartTs: req.Start.UTC(),
			EndTs:   req.End.UTC(),
			Path:    r.URL.Path,
			Shards:  req.Shards,
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
			Query:   req.Query,
		}, nil
	case IndexStatsOp:
		req, err := loghttp.ParseIndexStatsQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
		from, through := util.RoundToMilliseconds(req.Start, req.End)
		return &logproto.IndexStatsRequest{
			From:     from,
			Through:  through,
			Matchers: req.Query,
		}, err
	default:
		return nil, httpgrpc.Errorf(http.StatusBadRequest, fmt.Sprintf("unknown request path: %s", r.URL.Path))
	}
}

func (Codec) EncodeRequest(ctx context.Context, r queryrangebase.Request) (*http.Request, error) {
	header := make(http.Header)
	queryTags := getQueryTags(ctx)
	if queryTags != "" {
		header.Set(string(httpreq.QueryTagsHTTPHeader), queryTags)
	}

	actor := httpreq.ExtractHeader(ctx, httpreq.LokiActorPathHeader)
	if actor != "" {
		header.Set(httpreq.LokiActorPathHeader, actor)
	}

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
		if request.Interval != 0 {
			params["interval"] = []string{fmt.Sprintf("%f", float64(request.Interval)/float64(1e3))}
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
			Header:     header,
		}

		return req.WithContext(ctx), nil
	case *LokiSeriesRequest:
		params := url.Values{
			"start":   []string{fmt.Sprintf("%d", request.StartTs.UnixNano())},
			"end":     []string{fmt.Sprintf("%d", request.EndTs.UnixNano())},
			"match[]": request.Match,
		}
		if len(request.Shards) > 0 {
			params["shards"] = request.Shards
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
			Header:     header,
		}
		return req.WithContext(ctx), nil
	case *LokiLabelNamesRequest:
		params := url.Values{
			"start": []string{fmt.Sprintf("%d", request.StartTs.UnixNano())},
			"end":   []string{fmt.Sprintf("%d", request.EndTs.UnixNano())},
			"query": []string{request.GetQuery()},
		}

		u := &url.URL{
			Path:     request.Path, // NOTE: this could be either /label or /label/{name}/values endpoint. So forward the original path as it is.
			RawQuery: params.Encode(),
		}
		req := &http.Request{
			Method:     "GET",
			RequestURI: u.String(), // This is what the httpgrpc code looks at.
			URL:        u,
			Body:       http.NoBody,
			Header:     header,
		}
		return req.WithContext(ctx), nil
	case *LokiInstantRequest:
		params := url.Values{
			"query":     []string{request.Query},
			"direction": []string{request.Direction.String()},
			"limit":     []string{fmt.Sprintf("%d", request.Limit)},
			"time":      []string{fmt.Sprintf("%d", request.TimeTs.UnixNano())},
		}
		if len(request.Shards) > 0 {
			params["shards"] = request.Shards
		}
		u := &url.URL{
			// the request could come /api/prom/query but we want to only use the new api.
			Path:     "/loki/api/v1/query",
			RawQuery: params.Encode(),
		}
		req := &http.Request{
			Method:     "GET",
			RequestURI: u.String(), // This is what the httpgrpc code looks at.
			URL:        u,
			Body:       http.NoBody,
			Header:     header,
		}

		return req.WithContext(ctx), nil
	case *logproto.IndexStatsRequest:
		params := url.Values{
			"start": []string{fmt.Sprintf("%d", request.From.Time().UnixNano())},
			"end":   []string{fmt.Sprintf("%d", request.Through.Time().UnixNano())},
			"query": []string{request.GetQuery()},
		}
		u := &url.URL{
			Path:     "/loki/api/v1/index/stats",
			RawQuery: params.Encode(),
		}
		req := &http.Request{
			Method:     "GET",
			RequestURI: u.String(), // This is what the httpgrpc code looks at.
			URL:        u,
			Body:       http.NoBody,
			Header:     header,
		}
		return req.WithContext(ctx), nil
	default:
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "invalid request format")
	}
}

type Buffer interface {
	Bytes() []byte
}

func (Codec) DecodeResponse(ctx context.Context, r *http.Response, req queryrangebase.Request) (queryrangebase.Response, error) {
	if r.StatusCode/100 != 2 {
		body, _ := io.ReadAll(r.Body)
		return nil, httpgrpc.Errorf(r.StatusCode, string(body))
	}

	var buf []byte
	var err error
	if buffer, ok := r.Body.(Buffer); ok {
		buf = buffer.Bytes()
	} else {
		buf, err = io.ReadAll(r.Body)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
		}
	}

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
			Headers: httpResponseHeadersToPromResponseHeaders(r.Header),
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
			Headers: httpResponseHeadersToPromResponseHeaders(r.Header),
		}, nil
	case *logproto.IndexStatsRequest:
		var resp logproto.IndexStatsResponse
		if err := json.Unmarshal(buf, &resp); err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
		}
		return &IndexStatsResponse{
			Response: &resp,
			Headers:  httpResponseHeadersToPromResponseHeaders(r.Header),
		}, nil
	default:
		var resp loghttp.QueryResponse
		if err := resp.UnmarshalJSON(buf); err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
		}
		switch string(resp.Data.ResultType) {
		case loghttp.ResultTypeMatrix:
			return &LokiPromResponse{
				Response: &queryrangebase.PrometheusResponse{
					Status: resp.Status,
					Data: queryrangebase.PrometheusData{
						ResultType: loghttp.ResultTypeMatrix,
						Result:     toProtoMatrix(resp.Data.Result.(loghttp.Matrix)),
					},
					Headers: convertPrometheusResponseHeadersToPointers(httpResponseHeadersToPromResponseHeaders(r.Header)),
				},
				Statistics: resp.Data.Statistics,
			}, nil
		case loghttp.ResultTypeStream:
			// This is the same as in querysharding.go
			params, err := paramsFromRequest(req)
			if err != nil {
				return nil, err
			}

			var path string
			switch r := req.(type) {
			case *LokiRequest:
				path = r.GetPath()
			case *LokiInstantRequest:
				path = r.GetPath()
			default:
				return nil, fmt.Errorf("expected *LokiRequest or *LokiInstantRequest, got (%T)", r)
			}
			return &LokiResponse{
				Status:     resp.Status,
				Direction:  params.Direction(),
				Limit:      params.Limit(),
				Version:    uint32(loghttp.GetVersion(path)),
				Statistics: resp.Data.Statistics,
				Data: LokiData{
					ResultType: loghttp.ResultTypeStream,
					Result:     resp.Data.Result.(loghttp.Streams).ToProto(),
				},
				Headers: httpResponseHeadersToPromResponseHeaders(r.Header),
			}, nil
		case loghttp.ResultTypeVector:
			return &LokiPromResponse{
				Response: &queryrangebase.PrometheusResponse{
					Status: resp.Status,
					Data: queryrangebase.PrometheusData{
						ResultType: loghttp.ResultTypeVector,
						Result:     toProtoVector(resp.Data.Result.(loghttp.Vector)),
					},
					Headers: convertPrometheusResponseHeadersToPointers(httpResponseHeadersToPromResponseHeaders(r.Header)),
				},
				Statistics: resp.Data.Statistics,
			}, nil
		case loghttp.ResultTypeScalar:
			return &LokiPromResponse{
				Response: &queryrangebase.PrometheusResponse{
					Status: resp.Status,
					Data: queryrangebase.PrometheusData{
						ResultType: loghttp.ResultTypeScalar,
						Result:     toProtoScalar(resp.Data.Result.(loghttp.Scalar)),
					},
					Headers: convertPrometheusResponseHeadersToPointers(httpResponseHeadersToPromResponseHeaders(r.Header)),
				},
				Statistics: resp.Data.Statistics,
			}, nil
		default:
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "unsupported response type, got (%s)", string(resp.Data.ResultType))
		}
	}
}

func (Codec) EncodeResponse(ctx context.Context, res queryrangebase.Response) (*http.Response, error) {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "codec.EncodeResponse")
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
		result := logqlmodel.Result{
			Data:       logqlmodel.Streams(streams),
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
	case *IndexStatsResponse:
		if err := marshal.WriteIndexStatsResponseJSON(response.Response, &buf); err != nil {
			return nil, err
		}

	default:
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "invalid response format")
	}

	sp.LogFields(otlog.Int("bytes", buf.Len()))

	resp := http.Response{
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:       io.NopCloser(&buf),
		StatusCode: http.StatusOK,
	}
	return &resp, nil
}

// NOTE: When we would start caching response from non-metric queries we would have to consider cache gen headers as well in
// MergeResponse implementation for Loki codecs same as it is done in Cortex at https://github.com/cortexproject/cortex/blob/21bad57b346c730d684d6d0205efef133422ab28/pkg/querier/queryrange/query_range.go#L170
func (Codec) MergeResponse(responses ...queryrangebase.Response) (queryrangebase.Response, error) {
	if len(responses) == 0 {
		return nil, errors.New("merging responses requires at least one response")
	}
	var mergedStats stats.Result
	switch responses[0].(type) {
	case *LokiPromResponse:

		promResponses := make([]queryrangebase.Response, 0, len(responses))
		for _, res := range responses {
			mergedStats.MergeSplit(res.(*LokiPromResponse).Statistics)
			promResponses = append(promResponses, res.(*LokiPromResponse).Response)
		}
		promRes, err := queryrangebase.PrometheusCodec.MergeResponse(promResponses...)
		if err != nil {
			return nil, err
		}
		return &LokiPromResponse{
			Response:   promRes.(*queryrangebase.PrometheusResponse),
			Statistics: mergedStats,
		}, nil
	case *LokiResponse:
		return mergeLokiResponse(responses...), nil
	case *LokiSeriesResponse:
		lokiSeriesRes := responses[0].(*LokiSeriesResponse)

		var lokiSeriesData []logproto.SeriesIdentifier
		uniqueSeries := make(map[string]struct{})

		// only unique series should be merged
		for _, res := range responses {
			lokiResult := res.(*LokiSeriesResponse)
			mergedStats.MergeSplit(lokiResult.Statistics)
			for _, series := range lokiResult.Data {
				if _, ok := uniqueSeries[series.String()]; !ok {
					lokiSeriesData = append(lokiSeriesData, series)
					uniqueSeries[series.String()] = struct{}{}
				}
			}
		}

		return &LokiSeriesResponse{
			Status:     lokiSeriesRes.Status,
			Version:    lokiSeriesRes.Version,
			Data:       lokiSeriesData,
			Statistics: mergedStats,
		}, nil
	case *LokiLabelNamesResponse:
		labelNameRes := responses[0].(*LokiLabelNamesResponse)
		uniqueNames := make(map[string]struct{})
		names := []string{}

		// only unique name should be merged
		for _, res := range responses {
			lokiResult := res.(*LokiLabelNamesResponse)
			mergedStats.MergeSplit(lokiResult.Statistics)
			for _, labelName := range lokiResult.Data {
				if _, ok := uniqueNames[labelName]; !ok {
					names = append(names, labelName)
					uniqueNames[labelName] = struct{}{}
				}
			}
		}

		return &LokiLabelNamesResponse{
			Status:     labelNameRes.Status,
			Version:    labelNameRes.Version,
			Data:       names,
			Statistics: mergedStats,
		}, nil
	case *IndexStatsResponse:
		headers := responses[0].(*IndexStatsResponse).Headers
		stats := make([]*indexStats.Stats, len(responses))
		for i, res := range responses {
			stats[i] = res.(*IndexStatsResponse).Response
		}

		mergedIndexStats := indexStats.MergeStats(stats...)

		return &IndexStatsResponse{
			Response: &mergedIndexStats,
			Headers:  headers,
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

func toProtoMatrix(m loghttp.Matrix) []queryrangebase.SampleStream {
	res := make([]queryrangebase.SampleStream, 0, len(m))

	if len(m) == 0 {
		return res
	}

	for _, stream := range m {
		samples := make([]logproto.LegacySample, 0, len(stream.Values))
		for _, s := range stream.Values {
			samples = append(samples, logproto.LegacySample{
				Value:       float64(s.Value),
				TimestampMs: int64(s.Timestamp),
			})
		}
		res = append(res, queryrangebase.SampleStream{
			Labels:  logproto.FromMetricsToLabelAdapters(stream.Metric),
			Samples: samples,
		})
	}
	return res
}

func toProtoVector(v loghttp.Vector) []queryrangebase.SampleStream {
	res := make([]queryrangebase.SampleStream, 0, len(v))

	if len(v) == 0 {
		return res
	}
	for _, s := range v {
		res = append(res, queryrangebase.SampleStream{
			Samples: []logproto.LegacySample{{
				Value:       float64(s.Value),
				TimestampMs: int64(s.Timestamp),
			}},
			Labels: logproto.FromMetricsToLabelAdapters(s.Metric),
		})
	}
	return res
}

func toProtoScalar(v loghttp.Scalar) []queryrangebase.SampleStream {
	res := make([]queryrangebase.SampleStream, 0, 1)

	res = append(res, queryrangebase.SampleStream{
		Samples: []logproto.LegacySample{{
			Value:       float64(v.Value),
			TimestampMs: v.Timestamp.UnixNano() / 1e6,
		}},
		Labels: nil,
	})
	return res
}

func (res LokiResponse) Count() int64 {
	var result int64
	for _, s := range res.Data.Result {
		result += int64(len(s.Entries))
	}
	return result
}

func paramsFromRequest(req queryrangebase.Request) (logql.Params, error) {
	switch r := req.(type) {
	case *LokiRequest:
		return &paramsRangeWrapper{
			LokiRequest: r,
		}, nil
	case *LokiInstantRequest:
		return &paramsInstantWrapper{
			LokiInstantRequest: r,
		}, nil
	case *LokiSeriesRequest:
		return &paramsSeriesWrapper{
			LokiSeriesRequest: r,
		}, nil
	case *LokiLabelNamesRequest:
		return &paramsLabelNamesWrapper{
			LokiLabelNamesRequest: r,
		}, nil
	default:
		return nil, fmt.Errorf("expected one of the *LokiRequest, *LokiInstantRequest, *LokiSeriesRequest, *LokiLabelNamesRequest, got (%T)", r)
	}
}

type paramsRangeWrapper struct {
	*LokiRequest
}

func (p paramsRangeWrapper) Query() string {
	return p.GetQuery()
}

func (p paramsRangeWrapper) Start() time.Time {
	return p.GetStartTs()
}

func (p paramsRangeWrapper) End() time.Time {
	return p.GetEndTs()
}

func (p paramsRangeWrapper) Step() time.Duration {
	return time.Duration(p.GetStep() * 1e6)
}
func (p paramsRangeWrapper) Interval() time.Duration {
	return time.Duration(p.GetInterval() * 1e6)
}
func (p paramsRangeWrapper) Direction() logproto.Direction {
	return p.GetDirection()
}
func (p paramsRangeWrapper) Limit() uint32 { return p.LokiRequest.Limit }
func (p paramsRangeWrapper) Shards() []string {
	return p.GetShards()
}

type paramsInstantWrapper struct {
	*LokiInstantRequest
}

func (p paramsInstantWrapper) Query() string {
	return p.GetQuery()
}

func (p paramsInstantWrapper) Start() time.Time {
	return p.LokiInstantRequest.GetTimeTs()
}

func (p paramsInstantWrapper) End() time.Time {
	return p.LokiInstantRequest.GetTimeTs()
}

func (p paramsInstantWrapper) Step() time.Duration {
	return time.Duration(p.GetStep() * 1e6)
}
func (p paramsInstantWrapper) Interval() time.Duration { return 0 }
func (p paramsInstantWrapper) Direction() logproto.Direction {
	return p.GetDirection()
}
func (p paramsInstantWrapper) Limit() uint32 { return p.LokiInstantRequest.Limit }
func (p paramsInstantWrapper) Shards() []string {
	return p.GetShards()
}

type paramsSeriesWrapper struct {
	*LokiSeriesRequest
}

func (p paramsSeriesWrapper) Query() string {
	return p.GetQuery()
}

func (p paramsSeriesWrapper) Start() time.Time {
	return p.LokiSeriesRequest.GetStartTs()
}

func (p paramsSeriesWrapper) End() time.Time {
	return p.LokiSeriesRequest.GetEndTs()
}

func (p paramsSeriesWrapper) Step() time.Duration {
	return time.Duration(p.GetStep() * 1e6)
}
func (p paramsSeriesWrapper) Interval() time.Duration { return 0 }
func (p paramsSeriesWrapper) Direction() logproto.Direction {
	return logproto.FORWARD
}
func (p paramsSeriesWrapper) Limit() uint32 { return 0 }
func (p paramsSeriesWrapper) Shards() []string {
	return p.GetShards()
}

type paramsLabelNamesWrapper struct {
	*LokiLabelNamesRequest
}

func (p paramsLabelNamesWrapper) Query() string {
	return p.GetQuery()
}

func (p paramsLabelNamesWrapper) Start() time.Time {
	return p.LokiLabelNamesRequest.GetStartTs()
}

func (p paramsLabelNamesWrapper) End() time.Time {
	return p.LokiLabelNamesRequest.GetEndTs()
}

func (p paramsLabelNamesWrapper) Step() time.Duration {
	return time.Duration(p.GetStep() * 1e6)
}
func (p paramsLabelNamesWrapper) Interval() time.Duration { return 0 }
func (p paramsLabelNamesWrapper) Direction() logproto.Direction {
	return logproto.FORWARD
}
func (p paramsLabelNamesWrapper) Limit() uint32 { return 0 }
func (p paramsLabelNamesWrapper) Shards() []string {
	return make([]string, 0)
}

func httpResponseHeadersToPromResponseHeaders(httpHeaders http.Header) []queryrangebase.PrometheusResponseHeader {
	var promHeaders []queryrangebase.PrometheusResponseHeader
	for h, hv := range httpHeaders {
		promHeaders = append(promHeaders, queryrangebase.PrometheusResponseHeader{Name: h, Values: hv})
	}

	return promHeaders
}

func getQueryTags(ctx context.Context) string {
	v, _ := ctx.Value(httpreq.QueryTagsHTTPHeader).(string) // it's ok to be empty
	return v
}

func NewEmptyResponse(r queryrangebase.Request) (queryrangebase.Response, error) {
	switch req := r.(type) {
	case *LokiSeriesRequest:
		return &LokiSeriesResponse{
			Status:  loghttp.QueryStatusSuccess,
			Version: uint32(loghttp.GetVersion(req.Path)),
		}, nil
	case *LokiLabelNamesRequest:
		return &LokiLabelNamesResponse{
			Status:  loghttp.QueryStatusSuccess,
			Version: uint32(loghttp.GetVersion(req.Path)),
		}, nil
	case *LokiInstantRequest:
		// instant queries in the frontend are always metrics queries.
		return &LokiPromResponse{
			Response: &queryrangebase.PrometheusResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: queryrangebase.PrometheusData{
					ResultType: loghttp.ResultTypeVector,
				},
			},
		}, nil
	case *LokiRequest:
		// range query can either be metrics or logs
		expr, err := syntax.ParseExpr(req.Query)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
		if _, ok := expr.(syntax.SampleExpr); ok {
			return &LokiPromResponse{
				Response: queryrangebase.NewEmptyPrometheusResponse(),
			}, nil
		}
		return &LokiResponse{
			Status:    loghttp.QueryStatusSuccess,
			Direction: req.Direction,
			Limit:     req.Limit,
			Version:   uint32(loghttp.GetVersion(req.Path)),
			Data: LokiData{
				ResultType: loghttp.ResultTypeStream,
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported request type %T", req)
	}
}

func mergeLokiResponse(responses ...queryrangebase.Response) *LokiResponse {
	if len(responses) == 0 {
		return nil
	}
	var (
		lokiRes       = responses[0].(*LokiResponse)
		mergedStats   stats.Result
		lokiResponses = make([]*LokiResponse, 0, len(responses))
	)

	for _, res := range responses {
		lokiResult := res.(*LokiResponse)
		mergedStats.MergeSplit(lokiResult.Statistics)
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
	}
}
