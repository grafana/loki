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

var (
	defaultMetricRecorder = metricRecorderFn(func(data *queryData) {
		logql.RecordMetrics(data.ctx, log.With(util_log.Logger, "component", "frontend"), data.params, data.status, *data.statistics, data.result)
	})
	// StatsHTTPMiddleware is an http middleware to record stats for query_range filter.
	StatsHTTPMiddleware = statsHTTPMiddleware(defaultMetricRecorder)
)

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
			// http middlewares runs for every http request.
			// but we want only to record query_range filters.
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

			// execute the request
			resp, err := next.Do(ctx, req)

			// collect stats and status
			var statistics *stats.Result
			var res promql_parser.Value
			if resp != nil {
				switch r := resp.(type) {
				case *LokiResponse:
					statistics = &r.Statistics
					res = logqlmodel.Streams(r.Data.Result)
				case *LokiPromResponse:
					statistics = &r.Statistics
				default:
					level.Warn(logger).Log("msg", fmt.Sprintf("cannot compute stats, unexpected type: %T", resp))
				}
			}
			if statistics != nil {
				// Re-calculate the summary: the queueTime result is already merged so should not be updated
				// Log and record metrics for the current query
				statistics.ComputeSummary(time.Since(start), 0)
				statistics.Log(level.Debug(logger))
			}
			ctxValue := ctx.Value(ctxKey)
			if data, ok := ctxValue.(*queryData); ok {
				data.recorded = true
				data.statistics = statistics
				data.result = res
				p, errReq := paramsFromRequest(req)
				if errReq != nil {
					return nil, errReq
				}
				data.params = p
			}
			return resp, err
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
