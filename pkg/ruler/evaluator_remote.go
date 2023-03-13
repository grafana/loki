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
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/crypto/tls"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/prometheus/promql"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/querier/series"
	"github.com/grafana/loki/pkg/util/build"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

const (
	keepAlive        = time.Second * 10
	keepAliveTimeout = time.Second * 5

	serviceConfig = `{"loadBalancingPolicy": "round_robin"}`

	queryEndpointPath = "/loki/api/v1/query"

	mimeTypeFormPost = "application/x-www-form-urlencoded"
)

const EvalModeRemote = "remote"

var userAgent = fmt.Sprintf("loki-ruler/%s", build.Version)

type RemoteEvaluator struct {
	rq     *remoteQuerier
	logger log.Logger
}

func NewRemoteEvaluator(cfg *EvaluationConfig, logger log.Logger) (*RemoteEvaluator, error) {
	qfClient, err := dialQueryFrontend(cfg.QueryFrontend)
	if err != nil {
		return nil, fmt.Errorf("failed to dial query frontend for remote rule evaluation: %w", err)
	}

	return &RemoteEvaluator{
		rq:     newRemoteQuerier(qfClient, logger, WithOrgIDMiddleware),
		logger: logger,
	}, nil
}

func (r *RemoteEvaluator) Eval(ctx context.Context, qs string, now time.Time) (*logqlmodel.Result, error) {
	res, err := r.rq.Query(ctx, qs, now)
	if err != nil {
		return nil, fmt.Errorf("failed to perform remote evaluation of query %q: %w", qs, err)
	}

	return res, err
}

// dialQueryFrontend creates and initializes a new httpgrpc.HTTPClient taking a QueryFrontendConfig configuration.
func dialQueryFrontend(cfg QueryFrontendConfig) (httpgrpc.HTTPClient, error) {
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
			grpc.WithUnaryInterceptor(
				grpc_middleware.ChainUnaryClient(
					otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
					middleware.ClientUserHeaderInterceptor,
				),
			),
			grpc.WithDefaultServiceConfig(serviceConfig),
		},
		tlsDialOptions...,
	)

	conn, err := grpc.Dial(cfg.Address, dialOptions...)
	if err != nil {
		return nil, err
	}
	return httpgrpc.NewHTTPClient(conn), nil
}

// Middleware provides a mechanism to inspect outgoing remote querier requests.
type Middleware func(ctx context.Context, req *httpgrpc.HTTPRequest) error

// remoteQuerier executes read operations against a httpgrpc.HTTPClient.
type remoteQuerier struct {
	client      httpgrpc.HTTPClient
	middlewares []Middleware
	logger      log.Logger
}

// newRemoteQuerier creates and initializes a new remoteQuerier instance.
func newRemoteQuerier(
	client httpgrpc.HTTPClient,
	logger log.Logger,
	middlewares ...Middleware,
) *remoteQuerier {
	return &remoteQuerier{
		client:      client,
		middlewares: middlewares,
		logger:      logger,
	}
}

// Query performs a query for the given time.
func (q *remoteQuerier) Query(ctx context.Context, qs string, t time.Time) (*logqlmodel.Result, error) {
	logger, ctx := spanlogger.NewWithLogger(ctx, q.logger, "ruler.remoteEvaluation.Query")
	defer logger.Span.Finish()

	return q.query(ctx, qs, t, logger)
}

func (q *remoteQuerier) query(ctx context.Context, query string, ts time.Time, logger log.Logger) (*logqlmodel.Result, error) {
	args := make(url.Values)
	args.Set("query", query)
	args.Set("direction", "forward")
	if !ts.IsZero() {
		args.Set("time", ts.Format(time.RFC3339Nano))
	}
	body := []byte(args.Encode())
	hash := logql.HashedQuery(query)

	req := httpgrpc.HTTPRequest{
		Method: http.MethodPost,
		Url:    queryEndpointPath,
		Body:   body,
		Headers: []*httpgrpc.Header{
			{Key: textproto.CanonicalMIMEHeaderKey("User-Agent"), Values: []string{userAgent}},
			{Key: textproto.CanonicalMIMEHeaderKey("Content-Type"), Values: []string{mimeTypeFormPost}},
			{Key: textproto.CanonicalMIMEHeaderKey("Content-Length"), Values: []string{strconv.Itoa(len(body))}},
		},
	}

	for _, mdw := range q.middlewares {
		if err := mdw(ctx, &req); err != nil {
			return nil, err
		}
	}

	start := time.Now()
	resp, err := q.client.Handle(ctx, &req)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to remotely evaluate query expression", "err", err, "query_hash", hash, "qs", query, "ts", ts, "response_time", time.Since(start).Seconds())
		return nil, err
	}
	if resp.Code/100 != 2 {
		return nil, fmt.Errorf("unexpected response status code %d: %s", resp.Code, string(resp.Body))
	}
	level.Debug(logger).Log("msg", "query expression successfully evaluated", "query_hash", hash, "qs", query, "ts", ts, "response_time", time.Since(start).Seconds())

	return decodeResponse(resp)
}

func decodeResponse(resp *httpgrpc.HTTPResponse) (*logqlmodel.Result, error) {
	var decoded loghttp.QueryResponse
	if err := json.NewDecoder(bytes.NewReader(resp.Body)).Decode(&decoded); err != nil {
		return nil, err
	}
	if decoded.Status == "error" {
		return nil, fmt.Errorf("query response error: %s", decoded.Status)
	}

	switch decoded.Data.ResultType {
	case loghttp.ResultTypeVector:
		var res promql.Vector
		vec := decoded.Data.Result.(loghttp.Vector)

		for _, s := range vec {
			res = append(res, promql.Sample{
				Metric: series.MetricToLabels(s.Metric),
				Point:  promql.Point{V: float64(s.Value), T: int64(s.Timestamp)},
			})
		}

		return &logqlmodel.Result{
			Statistics: decoded.Data.Statistics,
			Data:       res,
		}, nil
	case loghttp.ResultTypeScalar:
		var res promql.Scalar
		scalar := decoded.Data.Result.(loghttp.Scalar)
		res.T = scalar.Timestamp.Unix()
		res.V = float64(scalar.Value)

		return &logqlmodel.Result{
			Statistics: decoded.Data.Statistics,
			Data:       res,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported result type %s", decoded.Data.ResultType)
	}
}

// WithOrgIDMiddleware attaches 'X-Scope-OrgID' header value to the outgoing request by inspecting the passed context.
// In case the expression to evaluate corresponds to a federated rule, the ExtractTenantIDs function will take care
// of normalizing and concatenating source tenants by separating them with a '|' character.
func WithOrgIDMiddleware(ctx context.Context, req *httpgrpc.HTTPRequest) error {
	orgID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return err
	}
	req.Headers = append(req.Headers, &httpgrpc.Header{
		Key:    textproto.CanonicalMIMEHeaderKey(user.OrgIDHeaderName),
		Values: []string{orgID},
	})
	return nil
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
