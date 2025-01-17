package transport

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/dskit/flagext"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	querier_stats "github.com/grafana/loki/v3/pkg/querier/stats"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/server"
)

const (
	// StatusClientClosedRequest is the status code for when a client request cancellation of an http request
	StatusClientClosedRequest = 499
	ServiceTimingHeaderName   = "Server-Timing"
)

var (
	errCanceled              = httpgrpc.Errorf(StatusClientClosedRequest, "%s", context.Canceled.Error())
	errDeadlineExceeded      = httpgrpc.Errorf(http.StatusGatewayTimeout, "%s", context.DeadlineExceeded.Error())
	errRequestEntityTooLarge = httpgrpc.Errorf(http.StatusRequestEntityTooLarge, "http: request body too large")
)

// Config for a Handler.
type HandlerConfig struct {
	LogQueriesLongerThan   time.Duration          `yaml:"log_queries_longer_than"`
	LogQueryRequestHeaders flagext.StringSliceCSV `yaml:"log_query_request_headers"`
	MaxBodySize            int64                  `yaml:"max_body_size"`
	QueryStatsEnabled      bool                   `yaml:"query_stats_enabled"`
}

func (cfg *HandlerConfig) RegisterFlags(f *flag.FlagSet) {
	f.DurationVar(&cfg.LogQueriesLongerThan, "frontend.log-queries-longer-than", 0, "Log queries that are slower than the specified duration. Set to 0 to disable. Set to < 0 to enable on all queries.")
	f.Var(&cfg.LogQueryRequestHeaders, "frontend.log-query-request-headers", "Comma-separated list of request header names to include in query logs. Applies to both query stats and slow queries logs.")
	f.Int64Var(&cfg.MaxBodySize, "frontend.max-body-size", 10*1024*1024, "Max body size for downstream prometheus.")
	f.BoolVar(&cfg.QueryStatsEnabled, "frontend.query-stats-enabled", false, "True to enable query statistics tracking. When enabled, a message with some statistics is logged for every query.")
}

// Handler accepts queries and forwards them to RoundTripper. It can log slow queries,
// but all other logic is inside the RoundTripper.
type Handler struct {
	cfg          HandlerConfig
	log          log.Logger
	roundTripper http.RoundTripper

	// Metrics.
	querySeconds *prometheus.CounterVec
	querySeries  *prometheus.CounterVec
	queryBytes   *prometheus.CounterVec
	activeUsers  *util.ActiveUsersCleanupService
}

// NewHandler creates a new frontend handler.
func NewHandler(cfg HandlerConfig, roundTripper http.RoundTripper, log log.Logger, reg prometheus.Registerer, metricsNamespace string) http.Handler {
	h := &Handler{
		cfg:          cfg,
		log:          log,
		roundTripper: roundTripper,
	}

	if cfg.QueryStatsEnabled {
		h.querySeconds = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "query_seconds_total",
			Help:      "Total amount of wall clock time spend processing queries.",
		}, []string{"user"})

		h.querySeries = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "query_fetched_series_total",
			Help:      "Number of series fetched to execute a query.",
		}, []string{"user"})

		h.queryBytes = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "query_fetched_chunks_bytes_total",
			Help:      "Size of all chunks fetched to execute a query in bytes.",
		}, []string{"user"})

		h.activeUsers = util.NewActiveUsersCleanupWithDefaultValues(func(user string) {
			h.querySeconds.DeleteLabelValues(user)
			h.querySeries.DeleteLabelValues(user)
			h.queryBytes.DeleteLabelValues(user)
		})
		// If cleaner stops or fail, we will simply not clean the metrics for inactive users.
		_ = h.activeUsers.StartAsync(context.Background())
	}

	return h
}

func (f *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var (
		stats       *querier_stats.Stats
		queryString url.Values
	)

	// Initialise the stats in the context and make sure it's propagated
	// down the request chain.
	if f.cfg.QueryStatsEnabled {
		var ctx context.Context
		stats, ctx = querier_stats.ContextWithEmptyStats(r.Context())
		r = r.WithContext(ctx)
	}

	defer func() {
		_ = r.Body.Close()
	}()

	// Buffer the body for later use to track slow queries.
	var buf bytes.Buffer
	r.Body = http.MaxBytesReader(w, r.Body, f.cfg.MaxBodySize)
	r.Body = io.NopCloser(io.TeeReader(r.Body, &buf))

	startTime := time.Now()
	resp, err := f.roundTripper.RoundTrip(r)
	queryResponseTime := time.Since(startTime)

	if err != nil {
		server.WriteError(err, w)
		return
	}

	hs := w.Header()
	for h, vs := range resp.Header {
		hs[h] = vs
	}

	if f.cfg.QueryStatsEnabled {
		writeServiceTimingHeader(queryResponseTime, hs, stats)
	}

	w.WriteHeader(resp.StatusCode)
	// we don't check for copy error as there is no much we can do at this point
	_, _ = io.Copy(w, resp.Body)

	// Check whether we should parse the query string.
	shouldReportSlowQuery := f.cfg.LogQueriesLongerThan > 0 && queryResponseTime > f.cfg.LogQueriesLongerThan
	if shouldReportSlowQuery || f.cfg.QueryStatsEnabled {
		queryString = f.parseRequestQueryString(r, buf)
	}

	if shouldReportSlowQuery {
		f.reportSlowQuery(r, queryString, queryResponseTime)
	}
	if f.cfg.QueryStatsEnabled {
		f.reportQueryStats(r, queryString, queryResponseTime, stats)
	}
}

// reportSlowQuery reports slow queries.
func (f *Handler) reportSlowQuery(r *http.Request, queryString url.Values, queryResponseTime time.Duration) {
	logMessage := append([]interface{}{
		"msg", "slow query detected",
		"method", r.Method,
		"host", r.Host,
		"path", r.URL.Path,
		"time_taken", queryResponseTime.String(),
	}, formatQueryString(queryString)...)

	level.Info(util_log.WithContext(r.Context(), f.log)).Log(logMessage...)
}

func (f *Handler) reportQueryStats(r *http.Request, queryString url.Values, queryResponseTime time.Duration, stats *querier_stats.Stats) {
	tenantIDs, err := tenant.TenantIDs(r.Context())
	if err != nil {
		return
	}
	userID := tenant.JoinTenantIDs(tenantIDs)
	wallTime := stats.LoadWallTime()
	numSeries := stats.LoadFetchedSeries()
	numBytes := stats.LoadFetchedChunkBytes()

	// Track stats.
	f.querySeconds.WithLabelValues(userID).Add(wallTime.Seconds())
	f.querySeries.WithLabelValues(userID).Add(float64(numSeries))
	f.queryBytes.WithLabelValues(userID).Add(float64(numBytes))
	f.activeUsers.UpdateUserTimestamp(userID, time.Now())

	// Log stats.
	logMessage := append([]interface{}{
		"msg", "query stats",
		"component", "query-frontend",
		"method", r.Method,
		"path", r.URL.Path,
		"response_time", queryResponseTime,
		"query_wall_time_seconds", wallTime.Seconds(),
		"fetched_series_count", numSeries,
		"fetched_chunks_bytes", numBytes,
	}, formatQueryString(queryString)...)

	if len(f.cfg.LogQueryRequestHeaders) != 0 {
		logMessage = append(logMessage, formatRequestHeaders(&r.Header, f.cfg.LogQueryRequestHeaders)...)
	}

	level.Info(util_log.WithContext(r.Context(), f.log)).Log(logMessage...)
}

func formatRequestHeaders(h *http.Header, headersToLog []string) (fields []interface{}) {
	for _, s := range headersToLog {
		if v := h.Get(s); v != "" {
			fields = append(fields, fmt.Sprintf("header_%s", strings.ReplaceAll(strings.ToLower(s), "-", "_")), v)
		}
	}
	return fields
}

func (f *Handler) parseRequestQueryString(r *http.Request, bodyBuf bytes.Buffer) url.Values {
	// Use previously buffered body.
	r.Body = io.NopCloser(&bodyBuf)

	// Ensure the form has been parsed so all the parameters are present
	err := r.ParseForm()
	if err != nil {
		level.Warn(util_log.WithContext(r.Context(), f.log)).Log("msg", "unable to parse request form", "err", err)
		return nil
	}

	return r.Form
}

func formatQueryString(queryString url.Values) (fields []interface{}) {
	for k, v := range queryString {
		fields = append(fields, fmt.Sprintf("param_%s", k), strings.Join(v, ","))
	}
	return fields
}

func writeServiceTimingHeader(queryResponseTime time.Duration, headers http.Header, stats *querier_stats.Stats) {
	if stats != nil {
		parts := make([]string, 0)
		parts = append(parts, statsValue("querier_wall_time", stats.LoadWallTime()))
		parts = append(parts, statsValue("response_time", queryResponseTime))
		headers.Set(ServiceTimingHeaderName, strings.Join(parts, ", "))
	}
}

func statsValue(name string, d time.Duration) string {
	durationInMs := strconv.FormatFloat(float64(d)/float64(time.Millisecond), 'f', -1, 64)
	return name + ";dur=" + durationInMs
}

func AdaptGrpcRoundTripperToHandler(r GrpcRoundTripper, codec Codec) queryrangebase.Handler {
	return &grpcRoundTripperToHandlerAdapter{roundTripper: r, codec: codec}
}

// This adapter wraps GrpcRoundTripper and converts it into a queryrangebase.Handler
type grpcRoundTripperToHandlerAdapter struct {
	roundTripper GrpcRoundTripper
	codec        Codec
}

func (a *grpcRoundTripperToHandlerAdapter) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	httpReq, err := a.codec.EncodeRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("cannot convert request to HTTP request: %w", err)
	}
	if err := user.InjectOrgIDIntoHTTPRequest(ctx, httpReq); err != nil {
		return nil, err
	}

	grpcReq, err := httpgrpc.FromHTTPRequest(httpReq)
	if err != nil {
		return nil, fmt.Errorf("cannot convert HTTP request to gRPC request: %w", err)
	}

	grpcResp, err := a.roundTripper.RoundTripGRPC(ctx, grpcReq)
	if err != nil {
		return nil, err
	}

	return a.codec.DecodeHTTPGrpcResponse(grpcResp, req)
}
