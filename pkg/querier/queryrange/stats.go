package queryrange

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log/level"
	"github.com/weaveworks/common/middleware"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/stats"
)

type ctxKeyType string

const ctxKey ctxKeyType = "stats"

var (
	defaultMetricRecorder = metricRecorderFn(func(status, query string, rangeType logql.QueryRangeType, stats stats.Result) {
		logql.RecordMetrics(status, query, rangeType, stats)
	})
	// StatsHTTPMiddleware is an http middleware to record stats for query_range filter.
	StatsHTTPMiddleware middleware.Interface = statsHTTPMiddleware(defaultMetricRecorder)
)

type metricRecorder interface {
	Record(status, query string, rangeType logql.QueryRangeType, stats stats.Result)
}

type metricRecorderFn func(status, query string, rangeType logql.QueryRangeType, stats stats.Result)

func (m metricRecorderFn) Record(status, query string, rangeType logql.QueryRangeType, stats stats.Result) {
	m(status, query, rangeType, stats)
}

type queryData struct {
	query      string
	statistics *stats.Result
	rangeType  logql.QueryRangeType
	recorded   bool
}

func statsHTTPMiddleware(recorder metricRecorder) middleware.Interface {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data := &queryData{}
			interceptor := &interceptor{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(
				interceptor,
				r.WithContext(context.WithValue(r.Context(), ctxKey, data)),
			)
			// http middlewares runs for every http request.
			// but we want only to record query_range filters.
			if data.recorded {
				if data.statistics == nil {
					data.statistics = &stats.Result{}
				}
				recorder.Record(
					strconv.Itoa(interceptor.statusCode),
					data.query,
					data.rangeType,
					*data.statistics,
				)
			}
		})
	})
}

// StatsCollectorMiddleware compute the stats summary based on the actual duration of the request and inject it in the request context.
func StatsCollectorMiddleware() queryrange.Middleware {
	return queryrange.MiddlewareFunc(func(next queryrange.Handler) queryrange.Handler {
		return queryrange.HandlerFunc(func(ctx context.Context, req queryrange.Request) (queryrange.Response, error) {
			logger := spanlogger.FromContext(ctx)
			start := time.Now()

			// execute the request
			resp, err := next.Do(ctx, req)

			// collect stats and status
			var statistics *stats.Result
			if resp != nil {
				switch r := resp.(type) {
				case *LokiResponse:
					statistics = &r.Statistics
				case *LokiPromResponse:
					statistics = &r.Statistics
				default:
					level.Warn(util.Logger).Log("msg", fmt.Sprintf("cannot compute stats, unexpected type: %T", resp))
				}
			}
			if statistics != nil {
				// Re-calculate the summary then log and record metrics for the current query
				statistics.ComputeSummary(time.Since(start))
				statistics.Log(logger)
			}
			ctxValue := ctx.Value(ctxKey)
			if data, ok := ctxValue.(*queryData); ok {
				data.recorded = true
				data.query = req.GetQuery()
				data.statistics = statistics
				data.rangeType = logql.GetRangeType(paramsFromRequest(req))
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
