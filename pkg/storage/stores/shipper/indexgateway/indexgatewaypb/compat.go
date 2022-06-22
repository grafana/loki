package indexgatewaypb

import (
	"time"

	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/timestamp"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

// Satisfy queryrangebase.Request

// GetStart returns the start timestamp of the request in milliseconds.
func (m *IndexStatsRequest) GetStart() int64 {
	return int64(m.From)
}

// GetEnd returns the end timestamp of the request in milliseconds.
func (m *IndexStatsRequest) GetEnd() int64 {
	return int64(m.Through)
}

// GetStep returns the step of the request in milliseconds.
func (m *IndexStatsRequest) GetStep() int64 { return 0 }

// GetQuery returns the query of the request.
func (m *IndexStatsRequest) GetQuery() string {
	return m.Matchers
}

// GetCachingOptions returns the caching options.
func (m *IndexStatsRequest) GetCachingOptions() (res queryrangebase.CachingOptions) { return }

// WithStartEnd clone the current request with different start and end timestamp.
func (m *IndexStatsRequest) WithStartEnd(startTime int64, endTime int64) queryrangebase.Request {
	new := *m
	new.From = model.TimeFromUnixNano(startTime * int64(time.Millisecond))
	new.Through = model.TimeFromUnixNano(endTime * int64(time.Millisecond))
	return &new
}

// WithQuery clone the current request with a different query.
func (m *IndexStatsRequest) WithQuery(query string) queryrangebase.Request {
	new := *m
	new.Matchers = query
	return &new
}

// LogToSpan writes information about this request to an OpenTracing span
func (m *IndexStatsRequest) LogToSpan(sp opentracing.Span) {
	sp.LogFields(
		otlog.String("query", m.GetQuery()),
		otlog.String("start", timestamp.Time(m.GetStart()).String()),
		otlog.String("end", timestamp.Time(m.GetEnd()).String()),
	)
}
