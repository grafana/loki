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
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	yaml "go.yaml.in/yaml/v4"

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

var userAgent = fmt.Sprintf("loki-ruler/%s", build.Version)

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

	metrics *metrics
}

func NewRemoteEvaluator(client httpgrpc.HTTPClient, overrides RulesLimits, logger log.Logger, registerer prometheus.Registerer) (*RemoteEvaluator, error) {
	return &RemoteEvaluator{
		client:    client,
		overrides: overrides,
		logger:    logger,
		metrics:   newMetrics(registerer),
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
	clientConfig := cfg.ClientConfig
	if clientConfig.MaxRecvMsgSize == 0 {
		clientConfig.MaxRecvMsgSize = 100 << 20
	}
	if clientConfig.MaxSendMsgSize == 0 {
		clientConfig.MaxSendMsgSize = 100 << 20
	}
	dialOptions, err := clientConfig.DialOption(nil, nil, middleware.NoOpInvalidClusterValidationReporter)
	if err != nil {
		return nil, err
	}
	dialOptions = append(dialOptions,
		[]grpc.DialOption{
			grpc.WithKeepaliveParams(
				keepalive.ClientParameters{
					Time:                keepAlive,
					Timeout:             keepAliveTimeout,
					PermitWithoutStream: true,
				},
			),
			grpc.WithChainUnaryInterceptor(
				middleware.ClientUserHeaderInterceptor,
			),
			grpc.WithDefaultServiceConfig(serviceConfig),
			grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		}...,
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
	logger, ctx := spanlogger.NewOTel(ctx, r.logger, tracer, "ruler.remoteEvaluation.Query")
	defer logger.Finish()

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

	r.metrics.successfulEvals.WithLabelValues(orgID).Inc()

	dr, err := r.decodeResponse(ctx, resp, orgID)
	if err != nil {
		return nil, err
	}

	level.Info(logger).Log("msg", "rule evaluation succeeded", "rule_name", ruleName, "rule_type", ruleType, "total", dr.Statistics.Summary.ExecTime, "total_bytes", dr.Statistics.Summary.TotalBytesProcessed)
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

	// ClientConfig allows configuring the gRPC client used to connect to the query-frontend.
	ClientConfig grpcclient.Config `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *QueryFrontendConfig) UnmarshalYAML(value *yaml.Node) error {
	flagext.DefaultValues(&c.ClientConfig)

	type rawQueryFrontendConfig struct {
		Address          string             `yaml:"address"`
		ClientConfig     *grpcclient.Config `yaml:"client_config,omitempty"`
		GRPCClientConfig *grpcclient.Config `yaml:"grpc_client_config,omitempty"`
		Inlined          grpcclient.Config  `yaml:",inline"`
	}

	raw := rawQueryFrontendConfig{
		Address: c.Address,
		Inlined: c.ClientConfig,
	}

	hasGRPCClientConfig := false
	hasClientConfig := false
	if value.Kind == yaml.MappingNode {
		for i := 0; i < len(value.Content); i += 2 {
			if value.Content[i].Value == "grpc_client_config" {
				hasGRPCClientConfig = true
				cfgCopy := c.ClientConfig
				raw.GRPCClientConfig = &cfgCopy
			}
			if value.Content[i].Value == "client_config" {
				hasClientConfig = true
				cfgCopy := c.ClientConfig
				raw.ClientConfig = &cfgCopy
			}
		}
	}

	if err := value.Load(&raw, yaml.WithKnownFields(true)); err != nil {
		return err
	}

	c.Address = raw.Address
	c.ClientConfig = raw.Inlined
	if hasGRPCClientConfig && raw.GRPCClientConfig != nil {
		// Overlay nested grpc_client_config values without discarding inlined fields (e.g. tls_enabled)
		overlayClientConfig(&c.ClientConfig, raw.GRPCClientConfig)
	} else if hasClientConfig && raw.ClientConfig != nil {
		overlayClientConfig(&c.ClientConfig, raw.ClientConfig)
	}

	return nil
}

func (c *QueryFrontendConfig) Validate() error {
	return c.ClientConfig.Validate()
}

func (c *QueryFrontendConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&c.Address, "ruler.evaluation.query-frontend.address", "", "GRPC listen address of the query-frontend(s). Must be a DNS address (prefixed with dns:///) to enable client side load balancing.")

	c.ClientConfig.RegisterFlagsWithPrefix("ruler.evaluation.query-frontend", f)
}

func overlayClientConfig(dst *grpcclient.Config, src *grpcclient.Config) {
	if src == nil {
		return
	}
	if src.MaxRecvMsgSize != 0 {
		dst.MaxRecvMsgSize = src.MaxRecvMsgSize
	}
	if src.MaxSendMsgSize != 0 {
		dst.MaxSendMsgSize = src.MaxSendMsgSize
	}
	if src.GRPCCompression != "" {
		dst.GRPCCompression = src.GRPCCompression
	}
	if src.RateLimit != 0 {
		dst.RateLimit = src.RateLimit
	}
	if src.RateLimitBurst != 0 {
		dst.RateLimitBurst = src.RateLimitBurst
	}
	if src.BackoffOnRatelimits {
		dst.BackoffOnRatelimits = src.BackoffOnRatelimits
	}
	if src.BackoffConfig.MaxRetries != 0 {
		dst.BackoffConfig = src.BackoffConfig
	}
	if src.InitialStreamWindowSize != 0 {
		dst.InitialStreamWindowSize = src.InitialStreamWindowSize
	}
	if src.InitialConnectionWindowSize != 0 {
		dst.InitialConnectionWindowSize = src.InitialConnectionWindowSize
	}
	if src.TLSEnabled {
		dst.TLSEnabled = src.TLSEnabled
	}
	if src.TLS.CertPath != "" || src.TLS.KeyPath != "" || src.TLS.CAPath != "" || src.TLS.ServerName != "" {
		dst.TLS = src.TLS
	}
	if src.ConnectTimeout != 0 {
		dst.ConnectTimeout = src.ConnectTimeout
	}
	if src.ConnectBackoffBaseDelay != 0 {
		dst.ConnectBackoffBaseDelay = src.ConnectBackoffBaseDelay
	}
	if src.ConnectBackoffMaxDelay != 0 {
		dst.ConnectBackoffMaxDelay = src.ConnectBackoffMaxDelay
	}
	if src.KeepaliveTime != 0 {
		dst.KeepaliveTime = src.KeepaliveTime
	}
	if src.KeepaliveTimeout != 0 {
		dst.KeepaliveTimeout = src.KeepaliveTimeout
	}
}
