package queryrange

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/middleware"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
)

type ctxKeyType string

const ctxKey ctxKeyType = "stats"

const (
	queryTypeLog            = "log"
	queryTypeMetric         = "metric"
	queryTypeSeries         = "series"
	queryTypeLabel          = "label"
	queryTypeStats          = "stats"
	queryTypeVolume         = "volume"
	queryTypeShards         = "shards"
	queryTypeDetectedFields = "detected_fields"
	queryTypeQueryPatterns  = "patterns"
	queryTypeDetectedLabels = "detected_labels"
)

var (
	defaultMetricRecorder = metricRecorderFn(func(data *queryData) {
		recordQueryMetrics(data)
	})

	StatsHTTPMiddleware middleware.Interface = statsHTTPMiddleware(defaultMetricRecorder)
)

// recordQueryMetrics will be called from Query Frontend middleware chain for any type of query.
func recordQueryMetrics(data *queryData) {
	logger := log.With(util_log.Logger, "component", "frontend")

	switch data.queryType {
	case queryTypeLog, queryTypeMetric:
		logql.RecordRangeAndInstantQueryMetrics(data.ctx, logger, data.params, data.status, *data.statistics, data.result)
	case queryTypeLabel:
		logql.RecordLabelQueryMetrics(data.ctx, logger, data.params.Start(), data.params.End(), data.label, data.params.QueryString(), data.status, *data.statistics)
	case queryTypeSeries:
		logql.RecordSeriesQueryMetrics(data.ctx, logger, data.params.Start(), data.params.End(), data.match, data.status, []string{}, *data.statistics)
	case queryTypeStats:
		logql.RecordStatsQueryMetrics(data.ctx, logger, data.params.Start(), data.params.End(), data.params.QueryString(), data.status, *data.statistics)
	case queryTypeVolume:
		logql.RecordVolumeQueryMetrics(data.ctx, logger, data.params.Start(), data.params.End(), data.params.QueryString(), data.params.Limit(), data.params.Step(), data.status, *data.statistics)
	case queryTypeDetectedFields:
		logql.RecordDetectedFieldsQueryMetrics(data.ctx, logger, data.params.Start(), data.params.End(), data.params.QueryString(), data.status, *data.statistics)
	case queryTypeDetectedLabels:
		logql.RecordDetectedLabelsQueryMetrics(data.ctx, logger, data.params.Start(), data.params.End(), data.params.QueryString(), data.status, *data.statistics)
	default:
		level.Error(logger).Log("msg", "failed to record query metrics", "err", fmt.Errorf("expected one of the *LokiRequest, *LokiInstantRequest, *LokiSeriesRequest, *LokiLabelNamesRequest, got %s", data.queryType))
	}
}

type metricRecorder interface {
	Record(data *queryData)
}

type metricRecorderFn func(data *queryData)

func (m metricRecorderFn) Record(data *queryData) {
	m(data)
}

type queryData struct {
	ctx        context.Context
	params     logql.Params
	statistics *stats.Result
	result     promql_parser.Value
	status     string
	queryType  string
	match      []string // used in `series` query
	label      string   // used in `labels` query

	recorded bool
}

func statsHTTPMiddleware(recorder metricRecorder) middleware.Interface {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data := &queryData{}
			interceptor := &interceptor{ResponseWriter: w, statusCode: http.StatusOK}
			r = r.WithContext(context.WithValue(r.Context(), ctxKey, data))
			next.ServeHTTP(
				interceptor,
				r,
			)
			if data.recorded {
				if data.statistics == nil {
					data.statistics = &stats.Result{}
				}
				data.ctx = r.Context()
				data.status = strconv.Itoa(interceptor.statusCode)
				recorder.Record(data)
			}
		})
	})
}

// StatsCollectorMiddleware compute the stats summary based on the actual duration of the request and inject it in the request context.
func StatsCollectorMiddleware() queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
			logger := spanlogger.FromContext(ctx)
			start := time.Now()

			// start a new statistics context to be used by middleware, which we will merge with the response's statistics
			middlewareStats, statsCtx := stats.NewContext(ctx)

			// execute the request
			resp, err := next.Do(statsCtx, req)
			if err != nil {
				return resp, err
			}

			// collect stats and status
			var responseStats *stats.Result
			var res promql_parser.Value
			var queryType string
			var totalEntries int

			if resp != nil {
				switch r := resp.(type) {
				case *LokiResponse:
					responseStats = &r.Statistics
					totalEntries = int(logqlmodel.Streams(r.Data.Result).Lines())
					queryType = queryTypeLog
				case *LokiPromResponse:
					responseStats = &r.Statistics
					if r.Response != nil {
						totalEntries = len(r.Response.Data.Result)
					}

					queryType = queryTypeMetric
					if _, ok := req.(*logproto.VolumeRequest); ok {
						queryType = queryTypeVolume
					}
				case *LokiSeriesResponse:
					responseStats = &r.Statistics // TODO: this is always nil. See codec.DecodeResponse
					totalEntries = len(r.Data)
					queryType = queryTypeSeries
				case *LokiLabelNamesResponse:
					responseStats = &r.Statistics // TODO: this is always nil. See codec.DecodeResponse
					totalEntries = len(r.Data)
					queryType = queryTypeLabel
				case *IndexStatsResponse:
					responseStats = &stats.Result{} // TODO: support stats in proto
					totalEntries = 1
					queryType = queryTypeStats
				case *ShardsResponse:
					responseStats = &r.Response.Statistics
					queryType = queryTypeShards
				case *DetectedFieldsResponse:
					responseStats = &stats.Result{} // TODO: support stats in detected fields
					totalEntries = 1
					queryType = queryTypeDetectedFields
				case *QueryPatternsResponse:
					responseStats = &stats.Result{} // TODO: support stats in query patterns
					totalEntries = len(r.Response.Series)
					queryType = queryTypeQueryPatterns
				case *DetectedLabelsResponse:
					responseStats = &stats.Result{}
					totalEntries = 1
					queryType = queryTypeDetectedLabels
				default:
					level.Warn(logger).Log("msg", fmt.Sprintf("cannot compute stats, unexpected type: %T", resp))
				}
			}

			if responseStats != nil {
				// merge the response's statistics with the stats collected by the middleware
				responseStats.Merge(middlewareStats.Result(time.Since(start), 0, totalEntries))

				// Re-calculate the summary: the queueTime result is already merged so should not be updated
				// Log and record metrics for the current query
				responseStats.ComputeSummary(time.Since(start), 0, totalEntries)
				if logger.Span != nil {
					logger.Span.LogKV(responseStats.KVList()...)
				}
			}
			ctxValue := ctx.Value(ctxKey)
			if data, ok := ctxValue.(*queryData); ok {
				data.recorded = true
				data.statistics = responseStats
				data.result = res
				data.queryType = queryType
				p, errReq := ParamsFromRequest(req)
				if errReq != nil {
					return nil, errReq
				}
				data.params = p

				// Record information for metadata queries.
				switch r := req.(type) {
				case *LabelRequest:
					data.label = r.Name
				case *LokiSeriesRequest:
					data.match = r.Match
				}
			}
			return resp, nil
		})
	})
}

// interceptor implements WriteHeader to intercept status codes. WriteHeader
// may not be called on success, so initialize statusCode with the status you
// want to report on success, i.e. http.StatusOK.
//
// interceptor also implements net.Hijacker, to let the downstream Handler
// hijack the connection. This is needed, for example, for working with websockets.
type interceptor struct {
	http.ResponseWriter
	statusCode int
	recorded   bool
}

func (i *interceptor) WriteHeader(code int) {
	if !i.recorded {
		i.statusCode = code
		i.recorded = true
	}
	i.ResponseWriter.WriteHeader(code)
}

func (i *interceptor) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := i.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("interceptor: can't cast parent ResponseWriter to Hijacker")
	}
	return hj.Hijack()
}
