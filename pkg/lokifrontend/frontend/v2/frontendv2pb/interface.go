package frontendv2pb

import (
	"time"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache/resultscache"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/prometheus/model/timestamp"
)

func (r *LokiStreamingRequest) GetCachingOptions() (res queryrangebase.CachingOptions) { return }

func (r *LokiStreamingRequest) GetEnd() time.Time {
	return r.EndTs
}

func (r *LokiStreamingRequest) GetStart() time.Time {
	return r.StartTs
}

func (r *LokiStreamingRequest) WithStartEnd(s time.Time, e time.Time) queryrangebase.Request {
	clone := *r
	clone.StartTs = s
	clone.EndTs = e
	return &clone
}

// WithStartEndForCache implements resultscache.Request.
func (r *LokiStreamingRequest) WithStartEndForCache(s time.Time, e time.Time) resultscache.Request {
	return r.WithStartEnd(s, e).(resultscache.Request)
}

func (r *LokiStreamingRequest) WithQuery(query string) queryrangebase.Request {
	clone := *r
	clone.Query = query
	return &clone
}

func (r *LokiStreamingRequest) LogToSpan(sp opentracing.Span) {
	sp.LogFields(
		otlog.String("query", r.GetQuery()),
		otlog.String("ts", timestamp.Time(r.GetStart().UnixMilli()).String()),
		otlog.Int64("limit", int64(r.GetLimit())),
	)
}

func (m *LokiStreamingResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	return nil
}

func (m *LokiStreamingResponse) SetHeader(name, value string) {
	//m.Headers = setHeader(m.Headers, name, value)
}

func (m *LokiStreamingResponse) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	//m.Headers = h
	return m
}
