package queryrange

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/middleware"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/loghttp"
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
	// Mark context as frontend so metrics logging can distinguish between frontend and querier
	data.ctx = logql.WithComponentContext(data.ctx, "frontend")

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

	recorded            bool
	estimatedQueryBytes int64
	indexStatsMu        sync.Mutex
	indexStatsBytesSeen map[indexStatsRequestKey]struct{}
}

type indexStatsRequestKey struct {
	from     model.Time
	through  model.Time
	matchers string
}

func (d *queryData) recordEstimatedQueryBytes(req *logproto.IndexStatsRequest, responseBytes uint64) {
	if d == nil || req == nil || responseBytes == 0 {
		return
	}

	d.indexStatsMu.Lock()
	defer d.indexStatsMu.Unlock()

	if d.indexStatsBytesSeen == nil {
		d.indexStatsBytesSeen = make(map[indexStatsRequestKey]struct{})
	}

	key := indexStatsRequestKey{
		from:     req.From,
		through:  req.Through,
		matchers: req.Matchers,
	}

	if _, seen := d.indexStatsBytesSeen[key]; seen {
		return
	}

	d.indexStatsBytesSeen[key] = struct{}{}
	d.estimatedQueryBytes += int64(responseBytes)
}

func IndexStatsContextCollectorMiddleware() queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
			resp, err := next.Do(ctx, req)
			if err != nil {
				return resp, err
			}

			indexStatsReq, ok := req.(*logproto.IndexStatsRequest)
			if !ok {
				return resp, nil
			}

			indexStatsResp, ok := resp.(*IndexStatsResponse)
			if !ok || indexStatsResp == nil || indexStatsResp.Response == nil {
				return resp, nil
			}

			data, _ := ctx.Value(ctxKey).(*queryData)
			if data != nil {
				data.recordEstimatedQueryBytes(indexStatsReq, indexStatsResp.Response.Bytes)
			}

			return resp, nil
		})
	})
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
			logger := spanlogger.FromContext(ctx, util_log.Logger)
			start := time.Now()
			ctxValue := ctx.Value(ctxKey)
			data, _ := ctxValue.(*queryData)

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
					res = logqlmodel.Streams(r.Data.Result)
					totalEntries = int(res.(logqlmodel.Streams).Lines())
					queryType = queryTypeLog
				case *LokiPromResponse:
					responseStats = &r.Statistics

					if r.Response != nil {
						totalEntries = len(r.Response.Data.Result)
						// Convert the response to promql_parser.Value for stats calculation
						switch r.Response.Data.ResultType {
						case loghttp.ResultTypeVector:
							res = sampleStreamToVector(r.Response.Data.Result)
						case loghttp.ResultTypeMatrix:
							res = sampleStreamToMatrix(r.Response.Data.Result)
						case loghttp.ResultTypeScalar:
							// Scalar is represented as a single SampleStream with one sample
							if len(r.Response.Data.Result) > 0 && len(r.Response.Data.Result[0].Samples) > 0 {
								sample := r.Response.Data.Result[0].Samples[0]
								res = promql.Scalar{
									T: sample.TimestampMs,
									V: sample.Value,
								}
							}
						}
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
					if data != nil && r.Response != nil {
						if indexStatsReq, ok := req.(*logproto.IndexStatsRequest); ok {
							data.recordEstimatedQueryBytes(indexStatsReq, r.Response.Bytes)
						}
					}
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
				if data != nil && data.estimatedQueryBytes > responseStats.Summary.EstimatedQueryBytes &&
					(queryType == queryTypeLog || queryType == queryTypeMetric) {
					responseStats.Summary.EstimatedQueryBytes = data.estimatedQueryBytes
				}

				// merge the response's statistics with the stats collected by the middleware
				responseStats.Merge(middlewareStats.Result(time.Since(start), 0, totalEntries))

				// Re-calculate the summary: the queueTime result is already merged so should not be updated
				// Log and record metrics for the current query
				responseStats.ComputeSummary(time.Since(start), 0, totalEntries)
				logger.LogKV(responseStats.KVList()...)
			}
			if data != nil {
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
