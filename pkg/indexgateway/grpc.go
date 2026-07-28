package indexgateway

import (
	"context"
	"strings"

	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

// indexGatewayServicePrefix is the gRPC method prefix of the IndexGateway service. The
// saturation check interceptors only apply to these methods, so other services sharing
// the same gRPC server (e.g. in single-binary mode) are unaffected.
const indexGatewayServicePrefix = "/indexgatewaypb.IndexGateway/"

// utilizationChecker is satisfied by limiter.UtilizationBasedLimiter.
type utilizationChecker interface {
	// LimitingReason returns a non-empty reason when requests should be rejected.
	LimitingReason() string
}

// NewSaturationCheckInterceptors returns gRPC interceptors that reject IndexGateway
// requests with a saturation error while checker reports a limiting reason.
func NewSaturationCheckInterceptors(r prometheus.Registerer, checker utilizationChecker) (grpc.UnaryServerInterceptor, grpc.StreamServerInterceptor) {
	limitedRequests := promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Subsystem: "index_gateway",
		Name:      "utilization_limited_requests_total",
		Help:      "Total number of requests rejected by the index gateway because of resource utilization based limiting.",
	}, []string{"reason"})

	check := func(fullMethod string) error {
		if !strings.HasPrefix(fullMethod, indexGatewayServicePrefix) {
			return nil
		}
		reason := checker.LimitingReason()
		if reason == "" {
			return nil
		}
		limitedRequests.WithLabelValues(reason).Inc()
		return newSaturatedError(reason)
	}

	unary := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := check(info.FullMethod); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}

	stream := func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := check(info.FullMethod); err != nil {
			return err
		}
		return handler(srv, ss)
	}

	return unary, stream
}

type ServerInterceptors struct {
	reqCount              *prometheus.CounterVec
	PerTenantRequestCount grpc.UnaryServerInterceptor
}

func NewServerInterceptors(r prometheus.Registerer) *ServerInterceptors {
	requestCount := promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Subsystem: "index_gateway",
		Name:      "requests_total",
		Help:      "Total amount of requests served by the index gateway",
	}, []string{"operation", "status", "tenant"})

	perTenantRequestCount := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		tenantID, err := tenant.TenantID(ctx)
		if err != nil {
			// ignore requests without tenantID
			return handler(ctx, req)
		}

		resp, err = handler(ctx, req)
		status := "success"
		if err != nil {
			status = "error"
		}
		requestCount.WithLabelValues(info.FullMethod, status, tenantID).Inc()
		return
	}

	return &ServerInterceptors{
		reqCount:              requestCount,
		PerTenantRequestCount: perTenantRequestCount,
	}
}
