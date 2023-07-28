package queryrange

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/logproto"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	promql_parser "github.com/prometheus/prometheus/promql/parser"
	"github.com/weaveworks/common/middleware"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

type ctxKeyType string

const ctxKey ctxKeyType = "stats"

const (
	queryTypeLog    = "log"
	queryTypeMetric = "metric"
	queryTypeSeries = "series"
	queryTypeLabel  = "label"
	queryTypeVolume = "volume"
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
		logql.RecordLabelQueryMetrics(data.ctx, logger, data.params.Start(), data.params.End(), data.label, data.params.Query(), data.status, *data.statistics)
	case queryTypeSeries:
		logql.RecordSeriesQueryMetrics(data.ctx, logger, data.params.Start(), data.params.End(), data.match, data.status, *data.statistics)
	case queryTypeVolume:
		logql.RecordVolumeQueryMetrics(data.ctx, logger, data.params.Start(), data.params.End(), data.status, *data.statistics)
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
	match      []string // used in `series` query.
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
				responseStats.Log(level.Debug(logger))
			}
			ctxValue := ctx.Value(ctxKey)
			if data, ok := ctxValue.(*queryData); ok {
				data.recorded = true
				data.statistics = responseStats
				data.result = res
				data.queryType = queryType
				p, errReq := paramsFromRequest(req)
				if errReq != nil {
					return nil, errReq
				}
				data.params = p

				// Record information for metadata queries.
				switch r := req.(type) {
				case *LokiLabelNamesRequest:
					data.label = getLabelNameFromLabelsQuery(r.Path)
				case *LokiSeriesRequest:
					data.match = r.Match
				}
			}
			return resp, nil
		})
	})
}

func getLabelNameFromLabelsQuery(path string) string {
	if strings.HasSuffix(path, "/values") {

		toks := strings.FieldsFunc(path, func(r rune) bool {
			return r == '/'
		})

		// now assuming path has suffix `/values` label name should be second last to the suffix
		// **if** there exists the second last.
		length := len(toks)
		if length >= 2 {
			return toks[length-2]
		}

	}

	return ""
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
