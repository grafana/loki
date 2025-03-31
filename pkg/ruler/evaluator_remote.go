package ruler

// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/grafana/mimir/pull/1536/
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Cortex Authors.

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/crypto/tls"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/user"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/build"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
)

const (
	keepAlive        = time.Second * 10
	keepAliveTimeout = time.Second * 5

	serviceConfig     = `{"loadBalancingPolicy": "round_robin"}`
	queryEndpointPath = "/loki/api/v1/query"
	mimeTypeFormPost  = "application/x-www-form-urlencoded"

	EvalModeRemote = "remote"
)

var (
	userAgent = fmt.Sprintf("loki-ruler/%s", build.Version)
)

type metrics struct {
	reqDurationSecs     *prometheus.HistogramVec
	responseSizeBytes   *prometheus.HistogramVec
	responseSizeSamples *prometheus.HistogramVec

	successfulEvals *prometheus.CounterVec
	failedEvals     *prometheus.CounterVec
}

type RemoteEvaluator struct {
	client    httpgrpc.HTTPClient
	overrides RulesLimits
	logger    log.Logger

	// we don't want/need to log all the additional context, such as
	// caller=spanlogger.go:116 component=ruler evaluation_mode=remote method=ruler.remoteEvaluation.Query
	// in insights logs, so create a new logger
	insightsLogger log.Logger

	metrics *metrics
}

func NewRemoteEvaluator(client httpgrpc.HTTPClient, overrides RulesLimits, logger log.Logger, registerer prometheus.Registerer) (*RemoteEvaluator, error) {
	return &RemoteEvaluator{
		client:         client,
		overrides:      overrides,
		logger:         logger,
		insightsLogger: log.NewLogfmtLogger(os.Stderr),
		metrics:        newMetrics(registerer),
	}, nil
}

func newMetrics(registerer prometheus.Registerer) *metrics {
	reqDurationSecs := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Subsystem: "ruler_remote_eval",
		Name:      "request_duration_seconds",
		// 0.005000, 0.015000, 0.045000, 0.135000, 0.405000, 1.215000, 3.645000, 10.935000, 32.805000
		Buckets: prometheus.ExponentialBuckets(0.005, 3, 9),
	}, []string{"user"})
	responseSizeBytes := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Subsystem: "ruler_remote_eval",
		Name:      "response_bytes",
		// 32, 128, 512, 2K, 8K, 32K, 128K, 512K, 2M, 8M
		Buckets: prometheus.ExponentialBuckets(32, 4, 10),
	}, []string{"user"})
	responseSizeSamples := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Subsystem: "ruler_remote_eval",
		Name:      "response_samples",
		// 1, 4, 16, 64, 256, 1024, 4096, 16384, 65536, 262144
		Buckets: prometheus.ExponentialBuckets(1, 4, 10),
	}, []string{"user"})

	successfulEvals := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Subsystem: "ruler_remote_eval",
		Name:      "success_total",
	}, []string{"user"})
	failedEvals := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Subsystem: "ruler_remote_eval",
		Name:      "failure_total",
	}, []string{"reason", "user"})

	registerer.MustRegister(
		reqDurationSecs,
		responseSizeBytes,
		responseSizeSamples,
		successfulEvals,
		failedEvals,
	)

	return &metrics{
		reqDurationSecs:     reqDurationSecs,
		responseSizeBytes:   responseSizeBytes,
		responseSizeSamples: responseSizeSamples,

		successfulEvals: successfulEvals,
		failedEvals:     failedEvals,
	}
}

type queryResponse struct {
	res *logqlmodel.Result
	err error
}

func (r *RemoteEvaluator) Eval(ctx context.Context, qs string, now time.Time) (*logqlmodel.Result, error) {
	orgID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve tenant ID from context: %w", err)
	}

	ch := make(chan queryResponse, 1)

	timeout := r.overrides.RulerRemoteEvaluationTimeout(orgID)
	tCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	go r.Query(tCtx, ch, orgID, qs, now)

	for {
		select {
		case <-tCtx.Done():
			r.metrics.failedEvals.WithLabelValues("timeout", orgID).Inc()
			return nil, fmt.Errorf("remote rule evaluation exceeded deadline of %fs (defined by ruler_remote_evaluation_timeout): %w", timeout.Seconds(), tCtx.Err())
		case res := <-ch:
			return res.res, res.err
		}
	}
}

// DialQueryFrontend creates and initializes a new httpgrpc.HTTPClient taking a QueryFrontendConfig configuration.
func DialQueryFrontend(cfg *QueryFrontendConfig) (httpgrpc.HTTPClient, error) {
	tlsDialOptions, err := cfg.TLS.GetGRPCDialOptions(cfg.TLSEnabled)
	if err != nil {
		return nil, err
	}
	dialOptions := append(
		[]grpc.DialOption{
			grpc.WithKeepaliveParams(
				keepalive.ClientParameters{
					Time:                keepAlive,
					Timeout:             keepAliveTimeout,
					PermitWithoutStream: true,
				},
			),
			grpc.WithChainUnaryInterceptor(
				otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
				middleware.ClientUserHeaderInterceptor,
			),
			grpc.WithDefaultServiceConfig(serviceConfig),
		},
		tlsDialOptions...,
	)

	// nolint:staticcheck // grpc.Dial() has been deprecated; we'll address it before upgrading to gRPC 2.
	conn, err := grpc.Dial(cfg.Address, dialOptions...)
	if err != nil {
		return nil, err
	}
	return httpgrpc.NewHTTPClient(conn), nil
}

// Middleware provides a mechanism to inspect outgoing remote querier requests.
type Middleware func(ctx context.Context, req *httpgrpc.HTTPRequest) error

// Query performs a query for the given time.
func (r *RemoteEvaluator) Query(ctx context.Context, ch chan<- queryResponse, orgID, qs string, t time.Time) {
	logger, ctx := spanlogger.NewWithLogger(ctx, r.logger, "ruler.remoteEvaluation.Query")
	defer logger.Span.Finish()

	res, err := r.query(ctx, orgID, qs, t, logger)
	ch <- queryResponse{res, err}
}

func (r *RemoteEvaluator) query(ctx context.Context, orgID, query string, ts time.Time, logger log.Logger) (*logqlmodel.Result, error) {
	args := make(url.Values)
	args.Set("query", query)
	args.Set("direction", "forward")
	if !ts.IsZero() {
		args.Set("time", ts.Format(time.RFC3339Nano))
	}
	body := []byte(args.Encode())
	hash := util.HashedQuery(query)

	// Retrieve rule details from context
	ruleName, ruleType := GetRuleDetailsFromContext(ctx)

	// Construct the X-Query-Tags header value
	queryTags := fmt.Sprintf("source=ruler,rule_name=%s,rule_type=%s", ruleName, ruleType)
	req := httpgrpc.HTTPRequest{
		Method: http.MethodPost,
		Url:    queryEndpointPath,
		Body:   body,
		Headers: []*httpgrpc.Header{
			{Key: textproto.CanonicalMIMEHeaderKey("User-Agent"), Values: []string{userAgent}},
			{Key: textproto.CanonicalMIMEHeaderKey("Content-Type"), Values: []string{mimeTypeFormPost}},
			{Key: textproto.CanonicalMIMEHeaderKey("Content-Length"), Values: []string{strconv.Itoa(len(body))}},
			{Key: textproto.CanonicalMIMEHeaderKey(string(httpreq.QueryTagsHTTPHeader)), Values: []string{queryTags}},
			{Key: textproto.CanonicalMIMEHeaderKey(user.OrgIDHeaderName), Values: []string{orgID}},
		},
	}

	start := time.Now()
	resp, err := r.client.Handle(ctx, &req)

	instrument.ObserveWithExemplar(ctx, r.metrics.reqDurationSecs.WithLabelValues(orgID), time.Since(start).Seconds())

	if resp != nil {
		instrument.ObserveWithExemplar(ctx, r.metrics.responseSizeBytes.WithLabelValues(orgID), float64(len(resp.Body)))
	}

	logger = log.With(logger, "query_hash", hash, "query", query, "instant", ts, "response_time", time.Since(start).String())

	if err != nil {
		r.metrics.failedEvals.WithLabelValues("error", orgID).Inc()

		level.Warn(logger).Log("msg", "failed to evaluate rule", "err", err)
		return nil, fmt.Errorf("rule evaluation failed: %w", err)
	}

	fullBody := resp.Body
	// created a limited reader to avoid logging the entire response body should it be very large
	limitedBody := io.LimitReader(bytes.NewReader(fullBody), 128)

	// TODO(dannyk): consider retrying if the rule has a very high interval, or the rule is very sensitive to missing samples
	//   i.e. critical alerts or recording rules producing crucial RemoteEvaluatorMetrics series
	if resp.Code/100 != 2 {
		r.metrics.failedEvals.WithLabelValues("upstream_error", orgID).Inc()

		respBod, _ := io.ReadAll(limitedBody)
		level.Warn(logger).Log("msg", "rule evaluation failed with non-2xx response", "response_code", resp.Code, "response_body", respBod)
		return nil, fmt.Errorf("unsuccessful/unexpected response - status code %d", resp.Code)
	}

	maxSize := r.overrides.RulerRemoteEvaluationMaxResponseSize(orgID)
	if maxSize > 0 && int64(len(fullBody)) >= maxSize {
		r.metrics.failedEvals.WithLabelValues("max_size", orgID).Inc()

		level.Error(logger).Log("msg", "rule evaluation exceeded max size", "max_size", maxSize, "response_size", len(fullBody))
		return nil, fmt.Errorf("%d bytes exceeds response size limit of %d (defined by ruler_remote_evaluation_max_response_size)", len(resp.Body), maxSize)
	}

	level.Debug(logger).Log("msg", "rule evaluation succeeded")
	r.metrics.successfulEvals.WithLabelValues(orgID).Inc()

	dr, err := r.decodeResponse(ctx, resp, orgID)
	if err != nil {
		return nil, err
	}
	level.Info(r.insightsLogger).Log("msg", "request timings", "insight", "true", "source", "loki_ruler", "rule_name", ruleName, "rule_type", ruleType, "total", dr.Statistics.Summary.ExecTime, "total_bytes", dr.Statistics.Summary.TotalBytesProcessed, "query_hash", util.HashedQuery(query))
	return dr, err
}

func (r *RemoteEvaluator) decodeResponse(ctx context.Context, resp *httpgrpc.HTTPResponse, orgID string) (*logqlmodel.Result, error) {
	fullBody := resp.Body
	// created a limited reader to avoid logging the entire response body should it be very large
	limitedBody := io.LimitReader(bytes.NewReader(fullBody), 1024)

	var decoded loghttp.QueryResponse
	if err := json.NewDecoder(bytes.NewReader(fullBody)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("unexpected body encoding, not valid JSON: %w, body: %s", err, limitedBody)
	}
	if decoded.Status != loghttp.QueryStatusSuccess {
		return nil, fmt.Errorf("query response error: status %q, body: %s", decoded.Status, limitedBody)
	}

	switch decoded.Data.ResultType {
	case loghttp.ResultTypeVector:
		var res promql.Vector
		vec := decoded.Data.Result.(loghttp.Vector)

		for _, s := range vec {
			res = append(res, promql.Sample{
				Metric: metricToLabels(s.Metric),
				F:      float64(s.Value),
				T:      int64(s.Timestamp),
			})
		}

		instrument.ObserveWithExemplar(ctx, r.metrics.responseSizeSamples.WithLabelValues(orgID), float64(len(res)))

		return &logqlmodel.Result{
			Statistics: decoded.Data.Statistics,
			Data:       res,
		}, nil
	case loghttp.ResultTypeScalar:
		var res promql.Scalar
		scalar := decoded.Data.Result.(loghttp.Scalar)
		res.T = scalar.Timestamp.Unix()
		res.V = float64(scalar.Value)

		instrument.ObserveWithExemplar(ctx, r.metrics.responseSizeSamples.WithLabelValues(orgID), 1)

		return &logqlmodel.Result{
			Statistics: decoded.Data.Statistics,
			Data:       res,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported result type: %q", decoded.Data.ResultType)
	}
}

func metricToLabels(m model.Metric) labels.Labels {
	b := labels.NewScratchBuilder(len(m))
	for k, v := range m {
		b.Add(string(k), string(v))
	}
	// PromQL expects all labels to be sorted! In general, anyone constructing
	// a labels.Labels list is responsible for sorting it during construction time.
	b.Sort()
	return b.Labels()
}

// QueryFrontendConfig defines query-frontend transport configuration.
type QueryFrontendConfig struct {
	// The address of the remote querier to connect to.
	Address string `yaml:"address"`

	// TLSEnabled tells whether TLS should be used to establish remote connection.
	TLSEnabled bool `yaml:"tls_enabled"`

	// TLS is the config for client TLS.
	TLS tls.ClientConfig `yaml:",inline"`
}

func (c *QueryFrontendConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&c.Address, "ruler.evaluation.query-frontend.address", "", "GRPC listen address of the query-frontend(s). Must be a DNS address (prefixed with dns:///) to enable client side load balancing.")
	f.BoolVar(&c.TLSEnabled, "ruler.evaluation.query-frontend.tls-enabled", false, "Set to true if query-frontend connection requires TLS.")

	c.TLS.RegisterFlagsWithPrefix("ruler.evaluation.query-frontend", f)
}
