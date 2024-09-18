package queryrange

import (
	"bytes"
	"container/heap"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/timestamp"
	"golang.org/x/exp/maps"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/storage/detected"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	indexStats "github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	"github.com/grafana/loki/v3/pkg/util/marshal"
	marshal_legacy "github.com/grafana/loki/v3/pkg/util/marshal/legacy"
	"github.com/grafana/loki/v3/pkg/util/querylimits"
)

const (
	cacheControlHeader = "Cache-Control"
	noCacheVal         = "no-cache"
)

var DefaultCodec = &Codec{}

type Codec struct{}

type RequestProtobufCodec struct {
	Codec
}

func (r *LokiRequest) GetEnd() time.Time {
	return r.EndTs
}

func (r *LokiRequest) GetStart() time.Time {
	return r.StartTs
}

func (r *LokiRequest) WithStartEnd(s time.Time, e time.Time) queryrangebase.Request {
	clone := *r
	clone.StartTs = s
	clone.EndTs = e
	return &clone
}

// WithStartEndForCache implements resultscache.Request.
func (r *LokiRequest) WithStartEndForCache(s time.Time, e time.Time) resultscache.Request {
	return r.WithStartEnd(s, e).(resultscache.Request)
}

func (r *LokiRequest) WithQuery(query string) queryrangebase.Request {
	clone := *r
	clone.Query = query
	return &clone
}

func (r *LokiRequest) WithShards(shards logql.Shards) *LokiRequest {
	clone := *r
	clone.Shards = shards.Encode()
	return &clone
}

func (r *LokiRequest) LogToSpan(sp opentracing.Span) {
	sp.LogFields(
		otlog.String("query", r.GetQuery()),
		otlog.String("start", timestamp.Time(r.GetStart().UnixMilli()).String()),
		otlog.String("end", timestamp.Time(r.GetEnd().UnixMilli()).String()),
		otlog.Int64("step (ms)", r.GetStep()),
		otlog.Int64("interval (ms)", r.GetInterval()),
		otlog.Int64("limit", int64(r.GetLimit())),
		otlog.String("direction", r.GetDirection().String()),
		otlog.String("shards", strings.Join(r.GetShards(), ",")),
	)
}

func (r *LokiInstantRequest) GetStep() int64 {
	return 0
}

func (r *LokiInstantRequest) GetEnd() time.Time {
	return r.TimeTs
}

func (r *LokiInstantRequest) GetStart() time.Time {
	return r.TimeTs
}

func (r *LokiInstantRequest) WithStartEnd(s time.Time, _ time.Time) queryrangebase.Request {
	clone := *r
	clone.TimeTs = s
	return &clone
}

// WithStartEndForCache implements resultscache.Request.
func (r *LokiInstantRequest) WithStartEndForCache(s time.Time, e time.Time) resultscache.Request {
	return r.WithStartEnd(s, e).(resultscache.Request)
}

func (r *LokiInstantRequest) WithQuery(query string) queryrangebase.Request {
	clone := *r
	clone.Query = query
	return &clone
}

func (r *LokiInstantRequest) WithShards(shards logql.Shards) *LokiInstantRequest {
	clone := *r
	clone.Shards = shards.Encode()
	return &clone
}

func (r *LokiInstantRequest) LogToSpan(sp opentracing.Span) {
	sp.LogFields(
		otlog.String("query", r.GetQuery()),
		otlog.String("ts", timestamp.Time(r.GetStart().UnixMilli()).String()),
		otlog.Int64("limit", int64(r.GetLimit())),
		otlog.String("direction", r.GetDirection().String()),
		otlog.String("shards", strings.Join(r.GetShards(), ",")),
	)
}

func (r *LokiSeriesRequest) GetEnd() time.Time {
	return r.EndTs
}

func (r *LokiSeriesRequest) GetStart() time.Time {
	return r.StartTs
}

func (r *LokiSeriesRequest) WithStartEnd(s, e time.Time) queryrangebase.Request {
	clone := *r
	clone.StartTs = s
	clone.EndTs = e
	return &clone
}

// WithStartEndForCache implements resultscache.Request.
func (r *LokiSeriesRequest) WithStartEndForCache(s time.Time, e time.Time) resultscache.Request {
	return r.WithStartEnd(s, e).(resultscache.Request)
}

func (r *LokiSeriesRequest) WithQuery(_ string) queryrangebase.Request {
	clone := *r
	return &clone
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
		otlog.String("start", timestamp.Time(r.GetStart().UnixMilli()).String()),
		otlog.String("end", timestamp.Time(r.GetEnd().UnixMilli()).String()),
		otlog.String("shards", strings.Join(r.GetShards(), ",")),
	)
}

func (*LokiSeriesRequest) GetCachingOptions() (res queryrangebase.CachingOptions) { return }

// In some other world LabelRequest could implement queryrangebase.Request.
type LabelRequest struct {
	path string
	logproto.LabelRequest
}

func NewLabelRequest(start, end time.Time, query, name, path string) *LabelRequest {
	return &LabelRequest{
		LabelRequest: logproto.LabelRequest{
			Start:  &start,
			End:    &end,
			Query:  query,
			Name:   name,
			Values: name != "",
		},
		path: path,
	}
}

func (r *LabelRequest) AsProto() *logproto.LabelRequest {
	return &r.LabelRequest
}

func (r *LabelRequest) GetEnd() time.Time {
	return *r.End
}

func (r *LabelRequest) GetEndTs() time.Time {
	return *r.End
}

func (r *LabelRequest) GetStart() time.Time {
	return *r.Start
}

func (r *LabelRequest) GetStartTs() time.Time {
	return *r.Start
}

func (r *LabelRequest) GetStep() int64 {
	return 0
}

func (r *LabelRequest) WithStartEnd(s, e time.Time) queryrangebase.Request {
	clone := *r
	clone.Start = &s
	clone.End = &e
	return &clone
}

// WithStartEndForCache implements resultscache.Request.
func (r *LabelRequest) WithStartEndForCache(s time.Time, e time.Time) resultscache.Request {
	return r.WithStartEnd(s, e).(resultscache.Request)
}

func (r *LabelRequest) WithQuery(query string) queryrangebase.Request {
	clone := *r
	clone.Query = query
	return &clone
}

func (r *LabelRequest) LogToSpan(sp opentracing.Span) {
	sp.LogFields(
		otlog.String("start", timestamp.Time(r.GetStart().UnixMilli()).String()),
		otlog.String("end", timestamp.Time(r.GetEnd().UnixMilli()).String()),
	)
}

func (r *LabelRequest) Path() string {
	return r.path
}

func (*LabelRequest) GetCachingOptions() (res queryrangebase.CachingOptions) { return }

type DetectedLabelsRequest struct {
	path string
	logproto.DetectedLabelsRequest
}

func (r *DetectedLabelsRequest) AsProto() *logproto.DetectedLabelsRequest {
	return &r.DetectedLabelsRequest
}

func (r *DetectedLabelsRequest) GetEnd() time.Time {
	return r.End
}

func (r *DetectedLabelsRequest) GetEndTs() time.Time {
	return r.End
}

func (r *DetectedLabelsRequest) GetStart() time.Time {
	return r.Start
}

func (r *DetectedLabelsRequest) GetStartTs() time.Time {
	return r.Start
}

func (r *DetectedLabelsRequest) GetStep() int64 {
	return 0
}

func (r *DetectedLabelsRequest) WithStartEnd(s, e time.Time) queryrangebase.Request {
	clone := *r
	clone.Start = s
	clone.End = e
	return &clone
}

// WithStartEndForCache implements resultscache.Request.
func (r *DetectedLabelsRequest) WithStartEndForCache(s time.Time, e time.Time) resultscache.Request {
	return r.WithStartEnd(s, e).(resultscache.Request)
}

func (r *DetectedLabelsRequest) WithQuery(query string) queryrangebase.Request {
	clone := *r
	clone.Query = query
	return &clone
}

func (r *DetectedLabelsRequest) LogToSpan(sp opentracing.Span) {
	sp.LogFields(
		otlog.String("start", timestamp.Time(r.GetStart().UnixNano()).String()),
		otlog.String("end", timestamp.Time(r.GetEnd().UnixNano()).String()),
	)
}

func (r *DetectedLabelsRequest) Path() string {
	return r.path
}

func (*DetectedLabelsRequest) GetCachingOptions() (res queryrangebase.CachingOptions) {
	return
}

func (Codec) DecodeRequest(_ context.Context, r *http.Request, _ []string) (queryrangebase.Request, error) {
	if err := r.ParseForm(); err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	disableCacheReq := false

	if strings.ToLower(strings.TrimSpace(r.Header.Get(cacheControlHeader))) == noCacheVal {
		disableCacheReq = true
	}

	switch op := getOperation(r.URL.Path); op {
	case QueryRangeOp:
		req, err := parseRangeQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}
		return req, nil
	case InstantQueryOp:
		req, err := parseInstantQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}

		req.CachingOptions = queryrangebase.CachingOptions{
			Disabled: disableCacheReq,
		}

		return req, nil
	case SeriesOp:
		req, err := loghttp.ParseAndValidateSeriesQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
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
			return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}

		return &LabelRequest{
			LabelRequest: *req,
			path:         r.URL.Path,
		}, nil
	case IndexStatsOp:
		req, err := loghttp.ParseIndexStatsQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}
		from, through := util.RoundToMilliseconds(req.Start, req.End)
		return &logproto.IndexStatsRequest{
			From:     from,
			Through:  through,
			Matchers: req.Query,
		}, err
	case IndexShardsOp:
		req, targetBytes, err := loghttp.ParseIndexShardsQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}
		from, through := util.RoundToMilliseconds(req.Start, req.End)
		return &logproto.ShardsRequest{
			From:                from,
			Through:             through,
			Query:               req.Query,
			TargetBytesPerShard: targetBytes.Bytes(),
		}, err
	case VolumeOp:
		req, err := loghttp.ParseVolumeInstantQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}
		from, through := util.RoundToMilliseconds(req.Start, req.End)
		return &logproto.VolumeRequest{
			From:         from,
			Through:      through,
			Matchers:     req.Query,
			Limit:        int32(req.Limit),
			Step:         0,
			TargetLabels: req.TargetLabels,
			AggregateBy:  req.AggregateBy,
			CachingOptions: queryrangebase.CachingOptions{
				Disabled: disableCacheReq,
			},
		}, err
	case VolumeRangeOp:
		req, err := loghttp.ParseVolumeRangeQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}
		from, through := util.RoundToMilliseconds(req.Start, req.End)
		return &logproto.VolumeRequest{
			From:         from,
			Through:      through,
			Matchers:     req.Query,
			Limit:        int32(req.Limit),
			Step:         req.Step.Milliseconds(),
			TargetLabels: req.TargetLabels,
			AggregateBy:  req.AggregateBy,
			CachingOptions: queryrangebase.CachingOptions{
				Disabled: disableCacheReq,
			},
		}, err
	case DetectedFieldsOp:
		req, err := loghttp.ParseDetectedFieldsQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}

		_, err = syntax.ParseExpr(req.Query)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}

		return &DetectedFieldsRequest{
			DetectedFieldsRequest: *req,
			path:                  r.URL.Path,
		}, nil
	case PatternsQueryOp:
		req, err := loghttp.ParsePatternsQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}
		return req, nil
	case DetectedLabelsOp:
		req, err := loghttp.ParseDetectedLabelsQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}
		return &DetectedLabelsRequest{
			DetectedLabelsRequest: *req,
			path:                  r.URL.Path,
		}, nil
	default:
		return nil, httpgrpc.Errorf(http.StatusNotFound, "%s", fmt.Sprintf("unknown request path: %s", r.URL.Path))
	}
}

// labelNamesRoutes is used to extract the name for querying label values.
var labelNamesRoutes = regexp.MustCompile(`/loki/api/v1/label/(?P<name>[^/]+)/values`)

// DecodeHTTPGrpcRequest decodes an httpgrp.HTTPRequest to queryrangebase.Request.
func (Codec) DecodeHTTPGrpcRequest(ctx context.Context, r *httpgrpc.HTTPRequest) (queryrangebase.Request, context.Context, error) {
	httpReq, err := http.NewRequest(r.Method, r.Url, io.NopCloser(bytes.NewBuffer(r.Body)))
	if err != nil {
		return nil, ctx, httpgrpc.Errorf(http.StatusInternalServerError, "%s", err.Error())
	}
	httpReq = httpReq.WithContext(ctx)
	httpReq.RequestURI = r.Url
	httpReq.ContentLength = int64(len(r.Body))

	// Note that the org ID should be injected by the scheduler processor.
	for _, h := range r.Headers {
		httpReq.Header[h.Key] = h.Values
	}

	// If there is not org ID in the context, we try the HTTP request.
	_, err = user.ExtractOrgID(ctx)
	if err != nil {
		_, ctx, err = user.ExtractOrgIDFromHTTPRequest(httpReq)
		if err != nil {
			return nil, nil, err
		}
	}

	// Add query tags
	if queryTags := httpreq.ExtractQueryTagsFromHTTP(httpReq); queryTags != "" {
		ctx = httpreq.InjectQueryTags(ctx, queryTags)
	}

	// Add disable pipeline wrappers
	if disableWrappers := httpReq.Header.Get(httpreq.LokiDisablePipelineWrappersHeader); disableWrappers != "" {
		httpreq.InjectHeader(ctx, httpreq.LokiDisablePipelineWrappersHeader, disableWrappers)
	}

	// Add query metrics
	if queueTimeHeader := httpReq.Header.Get(string(httpreq.QueryQueueTimeHTTPHeader)); queueTimeHeader != "" {
		queueTime, err := time.ParseDuration(queueTimeHeader)
		if err == nil {
			ctx = context.WithValue(ctx, httpreq.QueryQueueTimeHTTPHeader, queueTime)
		}
	}

	// If there is not encoding flags in the context, we try the HTTP request.
	if encFlags := httpreq.ExtractEncodingFlagsFromCtx(ctx); encFlags == nil {
		encFlags = httpreq.ExtractEncodingFlagsFromProto(r)
		if encFlags != nil {
			ctx = httpreq.AddEncodingFlagsToContext(ctx, encFlags)
		}
	}

	if err := httpReq.ParseForm(); err != nil {
		return nil, ctx, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	switch op := getOperation(httpReq.URL.Path); op {
	case QueryRangeOp:
		req, err := parseRangeQuery(httpReq)
		if err != nil {
			return nil, ctx, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}

		return req, ctx, nil
	case InstantQueryOp:
		req, err := parseInstantQuery(httpReq)
		if err != nil {
			return nil, ctx, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}

		return req, ctx, nil
	case SeriesOp:
		req, err := loghttp.ParseAndValidateSeriesQuery(httpReq)
		if err != nil {
			return nil, ctx, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}
		return &LokiSeriesRequest{
			Match:   req.Groups,
			StartTs: req.Start.UTC(),
			EndTs:   req.End.UTC(),
			Path:    r.Url,
			Shards:  req.Shards,
		}, ctx, nil
	case LabelNamesOp:
		req, err := loghttp.ParseLabelQuery(httpReq)
		if err != nil {
			return nil, ctx, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}

		if req.Name == "" {
			if match := labelNamesRoutes.FindSubmatch([]byte(httpReq.URL.Path)); len(match) > 1 {
				req.Name = string(match[1])
				req.Values = true
			}
		}

		return &LabelRequest{
			LabelRequest: *req,
			path:         httpReq.URL.Path,
		}, ctx, nil
	case IndexStatsOp:
		req, err := loghttp.ParseIndexStatsQuery(httpReq)
		if err != nil {
			return nil, ctx, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}
		from, through := util.RoundToMilliseconds(req.Start, req.End)
		return &logproto.IndexStatsRequest{
			From:     from,
			Through:  through,
			Matchers: req.Query,
		}, ctx, err
	case IndexShardsOp:
		req, targetBytes, err := loghttp.ParseIndexShardsQuery(httpReq)
		if err != nil {
			return nil, ctx, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}
		from, through := util.RoundToMilliseconds(req.Start, req.End)
		return &logproto.ShardsRequest{
			From:                from,
			Through:             through,
			Query:               req.Query,
			TargetBytesPerShard: targetBytes.Bytes(),
		}, ctx, nil

	case VolumeOp:
		req, err := loghttp.ParseVolumeInstantQuery(httpReq)
		if err != nil {
			return nil, ctx, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}
		from, through := util.RoundToMilliseconds(req.Start, req.End)
		return &logproto.VolumeRequest{
			From:         from,
			Through:      through,
			Matchers:     req.Query,
			Limit:        int32(req.Limit),
			Step:         0,
			TargetLabels: req.TargetLabels,
			AggregateBy:  req.AggregateBy,
		}, ctx, err
	case VolumeRangeOp:
		req, err := loghttp.ParseVolumeRangeQuery(httpReq)
		if err != nil {
			return nil, ctx, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}
		from, through := util.RoundToMilliseconds(req.Start, req.End)
		return &logproto.VolumeRequest{
			From:         from,
			Through:      through,
			Matchers:     req.Query,
			Limit:        int32(req.Limit),
			Step:         req.Step.Milliseconds(),
			TargetLabels: req.TargetLabels,
			AggregateBy:  req.AggregateBy,
		}, ctx, err
	case DetectedFieldsOp:
		req, err := loghttp.ParseDetectedFieldsQuery(httpReq)
		if err != nil {
			return nil, ctx, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}

		return &DetectedFieldsRequest{
			DetectedFieldsRequest: *req,
			path:                  httpReq.URL.Path,
		}, ctx, nil
	case PatternsQueryOp:
		req, err := loghttp.ParsePatternsQuery(httpReq)
		if err != nil {
			return nil, ctx, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}
		return req, ctx, nil
	case DetectedLabelsOp:
		req, err := loghttp.ParseDetectedLabelsQuery(httpReq)
		if err != nil {
			return nil, ctx, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}
		return &DetectedLabelsRequest{
			DetectedLabelsRequest: *req,
			path:                  httpReq.URL.Path,
		}, ctx, err
	default:
		return nil, ctx, httpgrpc.Errorf(http.StatusBadRequest, "%s", fmt.Sprintf("unknown request path in HTTP gRPC decode: %s", r.Url))
	}
}

// DecodeHTTPGrpcResponse decodes an httpgrp.HTTPResponse to queryrangebase.Response.
func (Codec) DecodeHTTPGrpcResponse(r *httpgrpc.HTTPResponse, req queryrangebase.Request) (queryrangebase.Response, error) {
	if r.Code/100 != 2 {
		return nil, httpgrpc.Errorf(int(r.Code), "%s", string(r.Body))
	}

	headers := make(http.Header)
	for _, header := range r.Headers {
		headers[header.Key] = header.Values
	}
	return decodeResponseJSONFrom(r.Body, req, headers)
}

func (Codec) EncodeHTTPGrpcResponse(_ context.Context, req *httpgrpc.HTTPRequest, res queryrangebase.Response) (*httpgrpc.HTTPResponse, error) {
	version := loghttp.GetVersion(req.Url)
	var buf bytes.Buffer

	encodingFlags := httpreq.ExtractEncodingFlagsFromProto(req)

	err := encodeResponseJSONTo(version, res, &buf, encodingFlags)
	if err != nil {
		return nil, err
	}

	httpRes := &httpgrpc.HTTPResponse{
		Code: int32(http.StatusOK),
		Body: buf.Bytes(),
		Headers: []*httpgrpc.Header{
			{Key: "Content-Type", Values: []string{"application/json; charset=UTF-8"}},
		},
	}

	for _, h := range res.GetHeaders() {
		httpRes.Headers = append(httpRes.Headers, &httpgrpc.Header{Key: h.Name, Values: h.Values})
	}

	return httpRes, nil
}

func (c Codec) EncodeRequest(ctx context.Context, r queryrangebase.Request) (*http.Request, error) {
	header := make(http.Header)

	// Add query tags
	if queryTags := getQueryTags(ctx); queryTags != "" {
		header.Set(string(httpreq.QueryTagsHTTPHeader), queryTags)
	}

	if encodingFlags := httpreq.ExtractHeader(ctx, httpreq.LokiEncodingFlagsHeader); encodingFlags != "" {
		header.Set(httpreq.LokiEncodingFlagsHeader, encodingFlags)
	}

	// Add actor path
	if actor := httpreq.ExtractHeader(ctx, httpreq.LokiActorPathHeader); actor != "" {
		header.Set(httpreq.LokiActorPathHeader, actor)
	}

	// Add disable wrappers
	if disableWrappers := httpreq.ExtractHeader(ctx, httpreq.LokiDisablePipelineWrappersHeader); disableWrappers != "" {
		header.Set(httpreq.LokiDisablePipelineWrappersHeader, disableWrappers)
	}

	// Add limits
	if limits := querylimits.ExtractQueryLimitsContext(ctx); limits != nil {
		err := querylimits.InjectQueryLimitsHeader(&header, limits)
		if err != nil {
			return nil, err
		}
	}

	// Add org id
	orgID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}
	header.Set(user.OrgIDHeaderName, orgID)

	// Propagate trace context in request.
	tracer, span := opentracing.GlobalTracer(), opentracing.SpanFromContext(ctx)
	if tracer != nil && span != nil {
		carrier := opentracing.HTTPHeadersCarrier(header)
		if err := tracer.Inject(span.Context(), opentracing.HTTPHeaders, carrier); err != nil {
			return nil, err
		}
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
		// undocumented param to allow specifying store chunks for a request,
		// used in bounded tsdb sharding
		// TODO(owen-d): version & encode in body instead? We're experiencing the limits
		// using the same reprs for internal vs external APIs and maybe we should handle that.
		if request.StoreChunks != nil {
			b, err := request.StoreChunks.Marshal()
			if err != nil {
				return nil, errors.Wrap(err, "marshaling store chunks")
			}
			params["storeChunks"] = []string{string(b)}
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
	case *LabelRequest:
		params := url.Values{
			"start": []string{fmt.Sprintf("%d", request.Start.UnixNano())},
			"end":   []string{fmt.Sprintf("%d", request.End.UnixNano())},
			"query": []string{request.GetQuery()},
		}

		u := &url.URL{
			Path:     request.Path(), // NOTE: this could be either /label or /label/{name}/values endpoint. So forward the original path as it is.
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
	case *logproto.VolumeRequest:
		params := url.Values{
			"start":       []string{fmt.Sprintf("%d", request.From.Time().UnixNano())},
			"end":         []string{fmt.Sprintf("%d", request.Through.Time().UnixNano())},
			"query":       []string{request.GetQuery()},
			"limit":       []string{fmt.Sprintf("%d", request.Limit)},
			"aggregateBy": []string{request.AggregateBy},
		}

		if len(request.TargetLabels) > 0 {
			params["targetLabels"] = []string{strings.Join(request.TargetLabels, ",")}
		}

		var u *url.URL
		if request.Step != 0 {
			params["step"] = []string{fmt.Sprintf("%f", float64(request.Step)/float64(1e3))}
			u = &url.URL{
				Path:     "/loki/api/v1/index/volume_range",
				RawQuery: params.Encode(),
			}
		} else {
			u = &url.URL{
				Path:     "/loki/api/v1/index/volume",
				RawQuery: params.Encode(),
			}
		}
		req := &http.Request{
			Method:     "GET",
			RequestURI: u.String(),
			URL:        u,
			Body:       http.NoBody,
			Header:     header,
		}
		return req.WithContext(ctx), nil
	case *logproto.ShardsRequest:
		params := url.Values{
			"start":               []string{fmt.Sprintf("%d", request.From.Time().UnixNano())},
			"end":                 []string{fmt.Sprintf("%d", request.Through.Time().UnixNano())},
			"query":               []string{request.GetQuery()},
			"targetBytesPerShard": []string{fmt.Sprintf("%d", request.TargetBytesPerShard)},
		}
		u := &url.URL{
			Path:     "/loki/api/v1/index/shards",
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
	case *DetectedFieldsRequest:
		params := url.Values{
			"query":       []string{request.GetQuery()},
			"start":       []string{fmt.Sprintf("%d", request.Start.UnixNano())},
			"end":         []string{fmt.Sprintf("%d", request.End.UnixNano())},
			"line_limit":  []string{fmt.Sprintf("%d", request.GetLineLimit())},
			"field_limit": []string{fmt.Sprintf("%d", request.GetFieldLimit())},
		}

		if request.Step != 0 {
			params["step"] = []string{fmt.Sprintf("%f", float64(request.Step)/float64(1e3))}
		}

		u := &url.URL{
			Path:     "/loki/api/v1/detected_fields",
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
	case *logproto.QueryPatternsRequest:
		params := url.Values{
			"query": []string{request.GetQuery()},
			"start": []string{fmt.Sprintf("%d", request.Start.UnixNano())},
			"end":   []string{fmt.Sprintf("%d", request.End.UnixNano())},
		}

		if request.Step != 0 {
			params["step"] = []string{fmt.Sprintf("%f", float64(request.Step)/float64(1e3))}
		}

		u := &url.URL{
			Path:     "/loki/api/v1/patterns",
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
	case *DetectedLabelsRequest:
		params := url.Values{
			"start": []string{fmt.Sprintf("%d", request.Start.UnixNano())},
			"end":   []string{fmt.Sprintf("%d", request.End.UnixNano())},
			"query": []string{request.GetQuery()},
		}

		u := &url.URL{
			Path:     "/loki/api/v1/detected_labels",
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
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "%s", fmt.Sprintf("invalid request format, got (%T)", r))
	}
}

// nolint:goconst
func (c Codec) Path(r queryrangebase.Request) string {
	switch request := r.(type) {
	case *LokiRequest:
		return "loki/api/v1/query_range"
	case *LokiSeriesRequest:
		return "loki/api/v1/series"
	case *LabelRequest:
		if request.Values {
			// This request contains user-generated input in the URL, which is not safe to reflect in the route path.
			return "loki/api/v1/label/values"
		}

		return request.Path()
	case *LokiInstantRequest:
		return "/loki/api/v1/query"
	case *logproto.IndexStatsRequest:
		return "/loki/api/v1/index/stats"
	case *logproto.VolumeRequest:
		return "/loki/api/v1/index/volume_range"
	case *DetectedFieldsRequest:
		return "/loki/api/v1/detected_fields"
	case *logproto.QueryPatternsRequest:
		return "/loki/api/v1/patterns"
	case *DetectedLabelsRequest:
		return "/loki/api/v1/detected_labels"
	}

	return "other"
}

func (p RequestProtobufCodec) EncodeRequest(ctx context.Context, r queryrangebase.Request) (*http.Request, error) {
	req, err := p.Codec.EncodeRequest(ctx, r)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.google.protobuf")
	return req, nil
}

type Buffer interface {
	Bytes() []byte
}

func (Codec) DecodeResponse(_ context.Context, r *http.Response, req queryrangebase.Request) (queryrangebase.Response, error) {
	if r.StatusCode/100 != 2 {
		body, _ := io.ReadAll(r.Body)
		return nil, httpgrpc.Errorf(r.StatusCode, "%s", string(body))
	}

	if r.Header.Get("Content-Type") == ProtobufType {
		return decodeResponseProtobuf(r, req)
	}

	// Default to JSON.
	return decodeResponseJSON(r, req)
}

func decodeResponseJSON(r *http.Response, req queryrangebase.Request) (queryrangebase.Response, error) {
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

	return decodeResponseJSONFrom(buf, req, r.Header)
}

func decodeResponseJSONFrom(buf []byte, req queryrangebase.Request, headers http.Header) (queryrangebase.Response, error) {
	switch req := req.(type) {
	case *LokiSeriesRequest:
		var resp LokiSeriesResponse
		if err := json.Unmarshal(buf, &resp); err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
		}

		return &LokiSeriesResponse{
			Status:  resp.Status,
			Version: uint32(loghttp.GetVersion(req.Path)),
			Headers: httpResponseHeadersToPromResponseHeaders(headers),
			Data:    resp.Data,
		}, nil
	case *LabelRequest:
		var resp loghttp.LabelResponse
		if err := json.Unmarshal(buf, &resp); err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
		}
		return &LokiLabelNamesResponse{
			Status:  resp.Status,
			Version: uint32(loghttp.GetVersion(req.Path())),
			Data:    resp.Data,
			Headers: httpResponseHeadersToPromResponseHeaders(headers),
		}, nil
	case *logproto.IndexStatsRequest:
		var resp logproto.IndexStatsResponse
		if err := json.Unmarshal(buf, &resp); err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
		}
		return &IndexStatsResponse{
			Response: &resp,
			Headers:  httpResponseHeadersToPromResponseHeaders(headers),
		}, nil
	case *logproto.ShardsRequest:
		var resp logproto.ShardsResponse
		if err := json.Unmarshal(buf, &resp); err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
		}
		return &ShardsResponse{
			Response: &resp,
			Headers:  httpResponseHeadersToPromResponseHeaders(headers),
		}, nil
	case *logproto.VolumeRequest:
		var resp logproto.VolumeResponse
		if err := json.Unmarshal(buf, &resp); err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
		}
		return &VolumeResponse{
			Response: &resp,
			Headers:  httpResponseHeadersToPromResponseHeaders(headers),
		}, nil
	case *DetectedFieldsRequest:
		var resp logproto.DetectedFieldsResponse
		if err := json.Unmarshal(buf, &resp); err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
		}
		return &DetectedFieldsResponse{
			Response: &resp,
			Headers:  httpResponseHeadersToPromResponseHeaders(headers),
		}, nil
	case *logproto.QueryPatternsRequest:
		var resp logproto.QueryPatternsResponse
		if err := json.Unmarshal(buf, &resp); err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
		}
		return &QueryPatternsResponse{
			Response: &resp,
			Headers:  httpResponseHeadersToPromResponseHeaders(headers),
		}, nil
	case *DetectedLabelsRequest:
		var resp logproto.DetectedLabelsResponse
		if err := json.Unmarshal(buf, &resp); err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
		}
		return &DetectedLabelsResponse{
			Response: &resp,
			Headers:  httpResponseHeadersToPromResponseHeaders(headers),
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
					Headers:  convertPrometheusResponseHeadersToPointers(httpResponseHeadersToPromResponseHeaders(headers)),
					Warnings: resp.Warnings,
				},
				Statistics: resp.Data.Statistics,
			}, nil
		case loghttp.ResultTypeStream:
			// This is the same as in querysharding.go
			params, err := ParamsFromRequest(req)
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
				Headers:  httpResponseHeadersToPromResponseHeaders(headers),
				Warnings: resp.Warnings,
			}, nil
		case loghttp.ResultTypeVector:
			return &LokiPromResponse{
				Response: &queryrangebase.PrometheusResponse{
					Status: resp.Status,
					Data: queryrangebase.PrometheusData{
						ResultType: loghttp.ResultTypeVector,
						Result:     toProtoVector(resp.Data.Result.(loghttp.Vector)),
					},
					Headers:  convertPrometheusResponseHeadersToPointers(httpResponseHeadersToPromResponseHeaders(headers)),
					Warnings: resp.Warnings,
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
					Headers:  convertPrometheusResponseHeadersToPointers(httpResponseHeadersToPromResponseHeaders(headers)),
					Warnings: resp.Warnings,
				},
				Statistics: resp.Data.Statistics,
			}, nil
		default:
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "unsupported response type, got (%s)", string(resp.Data.ResultType))
		}
	}
}

func decodeResponseProtobuf(r *http.Response, req queryrangebase.Request) (queryrangebase.Response, error) {
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

	// Shortcut series responses without deserialization.
	if _, ok := req.(*LokiSeriesRequest); ok {
		return GetLokiSeriesResponseView(buf)
	}

	resp := &QueryResponse{}
	err = resp.Unmarshal(buf)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
	}

	headers := httpResponseHeadersToPromResponseHeaders(r.Header)
	switch req.(type) {
	case *LokiSeriesRequest:
		return resp.GetSeries().WithHeaders(headers), nil
	case *LabelRequest:
		return resp.GetLabels().WithHeaders(headers), nil
	case *logproto.IndexStatsRequest:
		return resp.GetStats().WithHeaders(headers), nil
	case *logproto.ShardsRequest:
		return resp.GetShardsResponse().WithHeaders(headers), nil
	default:
		switch concrete := resp.Response.(type) {
		case *QueryResponse_Prom:
			return concrete.Prom.WithHeaders(headers), nil
		case *QueryResponse_Streams:
			return concrete.Streams.WithHeaders(headers), nil
		case *QueryResponse_TopkSketches:
			return concrete.TopkSketches.WithHeaders(headers), nil
		case *QueryResponse_QuantileSketches:
			return concrete.QuantileSketches.WithHeaders(headers), nil
		default:
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "unsupported response type, got (%T)", resp.Response)
		}
	}
}

func (Codec) EncodeResponse(ctx context.Context, req *http.Request, res queryrangebase.Response) (*http.Response, error) {
	if req.Header.Get("Accept") == ProtobufType {
		return encodeResponseProtobuf(ctx, res)
	}

	// Default to JSON.
	version := loghttp.GetVersion(req.RequestURI)
	encodingFlags := httpreq.ExtractEncodingFlags(req)
	return encodeResponseJSON(ctx, version, res, encodingFlags)
}

func encodeResponseJSON(ctx context.Context, version loghttp.Version, res queryrangebase.Response, encodeFlags httpreq.EncodingFlags) (*http.Response, error) {
	sp, _ := opentracing.StartSpanFromContext(ctx, "codec.EncodeResponse")
	defer sp.Finish()
	var buf bytes.Buffer

	err := encodeResponseJSONTo(version, res, &buf, encodeFlags)
	if err != nil {
		return nil, err
	}

	sp.LogFields(otlog.Int("bytes", buf.Len()))

	resp := http.Response{
		Header: http.Header{
			"Content-Type": []string{"application/json; charset=UTF-8"},
		},
		Body:       io.NopCloser(&buf),
		StatusCode: http.StatusOK,
	}
	return &resp, nil
}

func encodeResponseJSONTo(version loghttp.Version, res queryrangebase.Response, w io.Writer, encodeFlags httpreq.EncodingFlags) error {
	switch response := res.(type) {
	case *LokiPromResponse:
		return response.encodeTo(w)
	case *LokiResponse:
		streams := make([]logproto.Stream, len(response.Data.Result))

		for i, stream := range response.Data.Result {
			streams[i] = logproto.Stream{
				Labels:  stream.Labels,
				Entries: stream.Entries,
			}
		}
		if version == loghttp.VersionLegacy {
			result := logqlmodel.Result{
				Data:       logqlmodel.Streams(streams),
				Statistics: response.Statistics,
			}
			if err := marshal_legacy.WriteQueryResponseJSON(result, w); err != nil {
				return err
			}
		} else {
			if err := marshal.WriteQueryResponseJSON(logqlmodel.Streams(streams), response.Warnings, response.Statistics, w, encodeFlags); err != nil {
				return err
			}
		}
	case *MergedSeriesResponseView:
		if err := WriteSeriesResponseViewJSON(response, w); err != nil {
			return err
		}
	case *LokiSeriesResponse:
		if err := marshal.WriteSeriesResponseJSON(response.Data, w); err != nil {
			return err
		}
	case *LokiLabelNamesResponse:
		if loghttp.Version(response.Version) == loghttp.VersionLegacy {
			if err := marshal_legacy.WriteLabelResponseJSON(logproto.LabelResponse{Values: response.Data}, w); err != nil {
				return err
			}
		} else {
			if err := marshal.WriteLabelResponseJSON(response.Data, w); err != nil {
				return err
			}
		}
	case *IndexStatsResponse:
		if err := marshal.WriteIndexStatsResponseJSON(response.Response, w); err != nil {
			return err
		}
	case *ShardsResponse:
		if err := marshal.WriteIndexShardsResponseJSON(response.Response, w); err != nil {
			return err
		}
	case *VolumeResponse:
		if err := marshal.WriteVolumeResponseJSON(response.Response, w); err != nil {
			return err
		}
	case *DetectedFieldsResponse:
		if err := marshal.WriteDetectedFieldsResponseJSON(response.Response, w); err != nil {
			return err
		}
	case *QueryPatternsResponse:
		if err := marshal.WriteQueryPatternsResponseJSON(response.Response, w); err != nil {
			return err
		}
	case *DetectedLabelsResponse:
		if err := marshal.WriteDetectedLabelsResponseJSON(response.Response, w); err != nil {
			return err
		}
	default:
		return httpgrpc.Errorf(http.StatusInternalServerError, "%s", fmt.Sprintf("invalid response format, got (%T)", res))
	}

	return nil
}

func encodeResponseProtobuf(ctx context.Context, res queryrangebase.Response) (*http.Response, error) {
	sp, _ := opentracing.StartSpanFromContext(ctx, "codec.EncodeResponse")
	defer sp.Finish()

	p, err := QueryResponseWrap(res)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "%s", err.Error())
	}

	buf, err := p.Marshal()
	if err != nil {
		return nil, fmt.Errorf("could not marshal protobuf: %w", err)
	}

	resp := http.Response{
		Header: http.Header{
			"Content-Type": []string{ProtobufType},
		},
		Body:       io.NopCloser(bytes.NewBuffer(buf)),
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
	switch res := responses[0].(type) {
	// LokiPromResponse type is used for both instant and range queries.
	// Meaning, values that are merged can be either vector or matrix types.
	case *LokiPromResponse:

		codec := queryrangebase.PrometheusCodecForRangeQueries
		if res.Response.Data.ResultType == model.ValVector.String() {
			codec = queryrangebase.PrometheusCodecForInstantQueries
		}

		promResponses := make([]queryrangebase.Response, 0, len(responses))
		for _, res := range responses {
			mergedStats.MergeSplit(res.(*LokiPromResponse).Statistics)
			promResponses = append(promResponses, res.(*LokiPromResponse).Response)
		}
		promRes, err := codec.MergeResponse(promResponses...)
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
		uniqueSeries := make(map[uint64]struct{})

		// The buffers are used by `series.Hash`. They are allocated
		// outside of the method in order to reuse them for the next
		// iteration. This saves a lot of allocations.
		// 1KB is used for `b` after some experimentation. The
		// benchmarks are ~10% faster in comparison to no buffer with
		// little overhead. A run with 4MB should the same speedup but
		// much much more overhead.
		b := make([]byte, 0, 1024)
		var key uint64

		// only unique series should be merged
		for _, res := range responses {
			lokiResult := res.(*LokiSeriesResponse)
			mergedStats.MergeSplit(lokiResult.Statistics)
			for _, series := range lokiResult.Data {
				// Use series hash as the key.
				key = series.Hash(b)

				// TODO(karsten): There is a chance that the
				// keys match but not the labels due to hash
				// collision. Ideally there's an else block the
				// compares the series labels. However, that's
				// not trivial. Besides, instance.Series has the
				// same issue in its deduping logic.
				if _, ok := uniqueSeries[key]; !ok {
					lokiSeriesData = append(lokiSeriesData, series)
					uniqueSeries[key] = struct{}{}
				}
			}
		}

		return &LokiSeriesResponse{
			Status:     lokiSeriesRes.Status,
			Version:    lokiSeriesRes.Version,
			Data:       lokiSeriesData,
			Headers:    lokiSeriesRes.Headers,
			Statistics: mergedStats,
		}, nil
	case *LokiSeriesResponseView:
		v := &MergedSeriesResponseView{}
		for _, r := range responses {
			v.responses = append(v.responses, r.(*LokiSeriesResponseView))
		}
		return v, nil
	case *MergedSeriesResponseView:
		v := &MergedSeriesResponseView{}
		for _, r := range responses {
			v.responses = append(v.responses, r.(*MergedSeriesResponseView).responses...)
		}
		return v, nil
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
			Headers:    labelNameRes.Headers,
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
	case *VolumeResponse:
		resp0 := responses[0].(*VolumeResponse)
		headers := resp0.Headers

		resps := make([]*logproto.VolumeResponse, 0, len(responses))
		for _, r := range responses {
			resps = append(resps, r.(*VolumeResponse).Response)
		}

		return &VolumeResponse{
			Response: seriesvolume.Merge(resps, resp0.Response.Limit),
			Headers:  headers,
		}, nil
	case *DetectedFieldsResponse:
		resp0 := responses[0].(*DetectedFieldsResponse)
		headers := resp0.Headers
		fieldLimit := resp0.Response.GetFieldLimit()

		fields := []*logproto.DetectedField{}
		for _, r := range responses {
			fields = append(fields, r.(*DetectedFieldsResponse).Response.Fields...)
		}

		mergedFields, err := detected.MergeFields(fields, fieldLimit)

		if err != nil {
			return nil, err
		}

		return &DetectedFieldsResponse{
			Response: &logproto.DetectedFieldsResponse{
				Fields:     mergedFields,
				FieldLimit: 0,
			},
			Headers: headers,
		}, nil
	case *DetectedLabelsResponse:
		resp0 := responses[0].(*DetectedLabelsResponse)
		headers := resp0.Headers
		var labels []*logproto.DetectedLabel

		for _, r := range responses {
			labels = append(labels, r.(*DetectedLabelsResponse).Response.DetectedLabels...)
		}
		mergedLabels, err := detected.MergeLabels(labels)
		if err != nil {
			return nil, err
		}

		return &DetectedLabelsResponse{
			Response: &logproto.DetectedLabelsResponse{
				DetectedLabels: mergedLabels,
			},
			Headers: headers,
		}, nil
	default:
		return nil, fmt.Errorf("unknown response type (%T) in merging responses", responses[0])
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

func ParamsFromRequest(req queryrangebase.Request) (logql.Params, error) {
	switch r := req.(type) {
	case *LokiRequest:
		return &paramsRangeWrapper{
			LokiRequest: r,
		}, nil
	case *logproto.VolumeRequest:
		return &paramsRangeWrapper{
			LokiRequest: &LokiRequest{
				Query:   r.GetQuery(),
				Limit:   uint32(r.GetLimit()),
				Step:    r.GetStep(),
				StartTs: time.UnixMilli(r.GetStart().UnixNano()),
				EndTs:   time.UnixMilli(r.GetEnd().UnixNano()),
			},
		}, nil
	case *LokiInstantRequest:
		return &paramsInstantWrapper{
			LokiInstantRequest: r,
		}, nil
	case *LokiSeriesRequest:
		return &paramsSeriesWrapper{
			LokiSeriesRequest: r,
		}, nil
	case *LabelRequest:
		return &paramsLabelWrapper{
			LabelRequest: r,
		}, nil
	case *logproto.IndexStatsRequest:
		return &paramsStatsWrapper{
			IndexStatsRequest: r,
		}, nil
	case *DetectedFieldsRequest:
		return &paramsDetectedFieldsWrapper{
			DetectedFieldsRequest: r,
		}, nil
	case *DetectedLabelsRequest:
		return &paramsDetectedLabelsWrapper{
			DetectedLabelsRequest: r,
		}, nil
	default:
		return nil, fmt.Errorf("expected one of the *LokiRequest, *LokiInstantRequest, *LokiSeriesRequest, *LokiLabelNamesRequest, *DetectedFieldsRequest, got (%T)", r)
	}
}

type paramsRangeWrapper struct {
	*LokiRequest
}

func (p paramsRangeWrapper) QueryString() string {
	return p.GetQuery()
}

func (p paramsRangeWrapper) GetExpression() syntax.Expr {
	return p.LokiRequest.Plan.AST
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

func (p paramsRangeWrapper) CachingOptions() resultscache.CachingOptions {
	return resultscache.CachingOptions{}
}

type paramsInstantWrapper struct {
	*LokiInstantRequest
}

func (p paramsInstantWrapper) QueryString() string {
	return p.GetQuery()
}

func (p paramsInstantWrapper) GetExpression() syntax.Expr {
	return p.LokiInstantRequest.Plan.AST
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

func (p paramsInstantWrapper) CachingOptions() resultscache.CachingOptions {
	return p.LokiInstantRequest.CachingOptions
}

type paramsSeriesWrapper struct {
	*LokiSeriesRequest
}

func (p paramsSeriesWrapper) QueryString() string {
	return p.GetQuery()
}

func (p paramsSeriesWrapper) GetExpression() syntax.Expr {
	return nil
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

func (p paramsSeriesWrapper) GetStoreChunks() *logproto.ChunkRefGroup {
	return nil
}

func (p paramsSeriesWrapper) CachingOptions() resultscache.CachingOptions {
	return resultscache.CachingOptions{}
}

type paramsLabelWrapper struct {
	*LabelRequest
}

func (p paramsLabelWrapper) QueryString() string {
	return p.GetQuery()
}

func (p paramsLabelWrapper) GetExpression() syntax.Expr {
	return nil
}

func (p paramsLabelWrapper) Start() time.Time {
	return p.LabelRequest.GetStartTs()
}

func (p paramsLabelWrapper) End() time.Time {
	return p.LabelRequest.GetEndTs()
}

func (p paramsLabelWrapper) Step() time.Duration {
	return time.Duration(p.GetStep() * 1e6)
}
func (p paramsLabelWrapper) Interval() time.Duration { return 0 }
func (p paramsLabelWrapper) Direction() logproto.Direction {
	return logproto.FORWARD
}
func (p paramsLabelWrapper) Limit() uint32 { return 0 }
func (p paramsLabelWrapper) Shards() []string {
	return make([]string, 0)
}

func (p paramsLabelWrapper) GetStoreChunks() *logproto.ChunkRefGroup {
	return nil
}

func (p paramsLabelWrapper) CachingOptions() resultscache.CachingOptions {
	return resultscache.CachingOptions{}
}

type paramsStatsWrapper struct {
	*logproto.IndexStatsRequest
}

func (p paramsStatsWrapper) QueryString() string {
	return p.GetQuery()
}

func (p paramsStatsWrapper) GetExpression() syntax.Expr {
	return nil
}

func (p paramsStatsWrapper) Start() time.Time {
	return p.From.Time()
}

func (p paramsStatsWrapper) End() time.Time {
	return p.Through.Time()
}

func (p paramsStatsWrapper) Step() time.Duration {
	return time.Duration(p.GetStep() * 1e6)
}
func (p paramsStatsWrapper) Interval() time.Duration { return 0 }
func (p paramsStatsWrapper) Direction() logproto.Direction {
	return logproto.FORWARD
}
func (p paramsStatsWrapper) Limit() uint32 { return 0 }
func (p paramsStatsWrapper) Shards() []string {
	return make([]string, 0)
}

func (p paramsStatsWrapper) GetStoreChunks() *logproto.ChunkRefGroup {
	return nil
}

func (p paramsStatsWrapper) CachingOptions() resultscache.CachingOptions {
	return resultscache.CachingOptions{}
}

type paramsDetectedFieldsWrapper struct {
	*DetectedFieldsRequest
}

func (p paramsDetectedFieldsWrapper) QueryString() string {
	return p.GetQuery()
}

func (p paramsDetectedFieldsWrapper) GetExpression() syntax.Expr {
	expr, err := syntax.ParseExpr(p.GetQuery())
	if err != nil {
		return nil
	}

	return expr
}

func (p paramsDetectedFieldsWrapper) Start() time.Time {
	return p.GetStartTs()
}

func (p paramsDetectedFieldsWrapper) End() time.Time {
	return p.GetEndTs()
}

func (p paramsDetectedFieldsWrapper) Step() time.Duration {
	return time.Duration(p.GetStep() * 1e6)
}

func (p paramsDetectedFieldsWrapper) Interval() time.Duration {
	return 0
}

func (p paramsDetectedFieldsWrapper) Direction() logproto.Direction {
	return logproto.BACKWARD
}

func (p paramsDetectedFieldsWrapper) Limit() uint32 { return p.DetectedFieldsRequest.LineLimit }

func (p paramsDetectedFieldsWrapper) Shards() []string {
	return make([]string, 0)
}

type paramsDetectedLabelsWrapper struct {
	*DetectedLabelsRequest
}

func (p paramsDetectedLabelsWrapper) QueryString() string {
	return p.GetQuery()
}

func (p paramsDetectedLabelsWrapper) GetExpression() syntax.Expr {
	expr, err := syntax.ParseExpr(p.GetQuery())
	if err != nil {
		return nil
	}

	return expr
}

func (p paramsDetectedLabelsWrapper) Start() time.Time {
	return p.GetStartTs()
}

func (p paramsDetectedLabelsWrapper) End() time.Time {
	return p.GetEndTs()
}

func (p paramsDetectedLabelsWrapper) Step() time.Duration {
	return time.Duration(p.GetStep() * 1e6)
}

func (p paramsDetectedLabelsWrapper) Interval() time.Duration {
	return 0
}

func (p paramsDetectedLabelsWrapper) Direction() logproto.Direction {
	return logproto.BACKWARD
}
func (p paramsDetectedLabelsWrapper) Limit() uint32 { return 0 }
func (p paramsDetectedLabelsWrapper) Shards() []string {
	return make([]string, 0)
}

func (p paramsDetectedLabelsWrapper) GetStoreChunks() *logproto.ChunkRefGroup {
	return nil
}

func (p paramsDetectedFieldsWrapper) GetStoreChunks() *logproto.ChunkRefGroup {
	return nil
}

func (p paramsDetectedLabelsWrapper) CachingOptions() resultscache.CachingOptions {
	return resultscache.CachingOptions{}
}

func (p paramsDetectedFieldsWrapper) CachingOptions() resultscache.CachingOptions {
	return resultscache.CachingOptions{}
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
	case *LabelRequest:
		return &LokiLabelNamesResponse{
			Status:  loghttp.QueryStatusSuccess,
			Version: uint32(loghttp.GetVersion(req.Path())),
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
			return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}
		if _, ok := expr.(syntax.SampleExpr); ok {
			return &LokiPromResponse{
				Response: queryrangebase.NewEmptyPrometheusResponse(model.ValMatrix), // range metric query
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
	case *logproto.IndexStatsRequest:
		return &IndexStatsResponse{
			Response: &logproto.IndexStatsResponse{},
		}, nil
	case *logproto.VolumeRequest:
		return &VolumeResponse{
			Response: &logproto.VolumeResponse{},
		}, nil
	case *DetectedLabelsRequest:
		return &DetectedLabelsResponse{
			Response: &logproto.DetectedLabelsResponse{},
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

	uniqueWarnings := map[string]struct{}{}
	for _, res := range responses {
		lokiResult := res.(*LokiResponse)
		mergedStats.MergeSplit(lokiResult.Statistics)
		lokiResponses = append(lokiResponses, lokiResult)

		for _, w := range lokiResult.Warnings {
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

	return &LokiResponse{
		Status:     loghttp.QueryStatusSuccess,
		Direction:  lokiRes.Direction,
		Limit:      lokiRes.Limit,
		Version:    lokiRes.Version,
		ErrorType:  lokiRes.ErrorType,
		Error:      lokiRes.Error,
		Statistics: mergedStats,
		Warnings:   warnings,
		Data: LokiData{
			ResultType: loghttp.ResultTypeStream,
			Result:     mergeOrderedNonOverlappingStreams(lokiResponses, lokiRes.Limit, lokiRes.Direction),
		},
	}
}

func parseRangeQuery(r *http.Request) (*LokiRequest, error) {
	rangeQuery, err := loghttp.ParseRangeQuery(r)
	if err != nil {
		return nil, err
	}

	parsed, err := syntax.ParseExpr(rangeQuery.Query)
	if err != nil {
		return nil, err
	}

	storeChunks, err := parseStoreChunks(r)
	if err != nil {
		return nil, err
	}

	return &LokiRequest{
		Query:       rangeQuery.Query,
		Limit:       rangeQuery.Limit,
		Direction:   rangeQuery.Direction,
		StartTs:     rangeQuery.Start.UTC(),
		EndTs:       rangeQuery.End.UTC(),
		Step:        rangeQuery.Step.Milliseconds(),
		Interval:    rangeQuery.Interval.Milliseconds(),
		Path:        r.URL.Path,
		Shards:      rangeQuery.Shards,
		StoreChunks: storeChunks,
		Plan: &plan.QueryPlan{
			AST: parsed,
		},
	}, nil
}

func parseInstantQuery(r *http.Request) (*LokiInstantRequest, error) {
	req, err := loghttp.ParseInstantQuery(r)
	if err != nil {
		return nil, err
	}

	parsed, err := syntax.ParseExpr(req.Query)
	if err != nil {
		return nil, err
	}

	storeChunks, err := parseStoreChunks(r)
	if err != nil {
		return nil, err
	}

	return &LokiInstantRequest{
		Query:       req.Query,
		Limit:       req.Limit,
		Direction:   req.Direction,
		TimeTs:      req.Ts.UTC(),
		Path:        r.URL.Path,
		Shards:      req.Shards,
		StoreChunks: storeChunks,
		Plan: &plan.QueryPlan{
			AST: parsed,
		},
	}, nil
}

// escape hatch for including store chunks in the request
func parseStoreChunks(r *http.Request) (*logproto.ChunkRefGroup, error) {
	if s := r.Form.Get("storeChunks"); s != "" {
		storeChunks := &logproto.ChunkRefGroup{}
		if err := storeChunks.Unmarshal([]byte(s)); err != nil {
			return nil, errors.Wrap(err, "unmarshaling storeChunks")
		}
		return storeChunks, nil
	}
	return nil, nil
}

type DetectedFieldsRequest struct {
	logproto.DetectedFieldsRequest
	path string
}

func NewDetectedFieldsRequest(start, end time.Time, lineLimit, fieldLimit uint32, step int64, query, path string) *DetectedFieldsRequest {
	return &DetectedFieldsRequest{
		DetectedFieldsRequest: logproto.DetectedFieldsRequest{
			Start:      start,
			End:        end,
			Query:      query,
			LineLimit:  lineLimit,
			FieldLimit: fieldLimit,
			Step:       step,
		},
		path: path,
	}
}

func (r *DetectedFieldsRequest) AsProto() *logproto.DetectedFieldsRequest {
	return &r.DetectedFieldsRequest
}

func (r *DetectedFieldsRequest) GetEnd() time.Time {
	return r.End
}

func (r *DetectedFieldsRequest) GetEndTs() time.Time {
	return r.End
}

func (r *DetectedFieldsRequest) GetStart() time.Time {
	return r.Start
}

func (r *DetectedFieldsRequest) GetStartTs() time.Time {
	return r.Start
}

func (r *DetectedFieldsRequest) GetStep() int64 {
	return r.Step
}

func (r *DetectedFieldsRequest) GetLineLimit() uint32 {
	return r.LineLimit
}

func (r *DetectedFieldsRequest) GetFieldLimit() uint32 {
	return r.FieldLimit
}

func (r *DetectedFieldsRequest) Path() string {
	return r.path
}

func (r *DetectedFieldsRequest) WithStartEnd(s, e time.Time) queryrangebase.Request {
	clone := *r
	clone.Start = s
	clone.End = e
	return &clone
}

// WithStartEndForCache implements resultscache.Request.
func (r *DetectedFieldsRequest) WithStartEndForCache(s time.Time, e time.Time) resultscache.Request {
	return r.WithStartEnd(s, e).(resultscache.Request)
}

func (r *DetectedFieldsRequest) WithQuery(query string) queryrangebase.Request {
	clone := *r
	clone.Query = query
	return &clone
}

func (r *DetectedFieldsRequest) LogToSpan(sp opentracing.Span) {
	sp.LogFields(
		otlog.String("start", timestamp.Time(r.GetStart().UnixNano()).String()),
		otlog.String("end", timestamp.Time(r.GetEnd().UnixNano()).String()),
		otlog.String("query", r.GetQuery()),
		otlog.Int64("step (ms)", r.GetStep()),
		otlog.Int64("line_limit", int64(r.GetLineLimit())),
		otlog.Int64("field_limit", int64(r.GetFieldLimit())),
		otlog.String("step", fmt.Sprintf("%d", r.GetStep())),
	)
}

func (*DetectedFieldsRequest) GetCachingOptions() (res queryrangebase.CachingOptions) { return }
