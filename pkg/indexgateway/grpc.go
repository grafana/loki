package indexgateway

import (
	"context"

	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

type ServerInterceptors struct {
	reqCount               *prometheus.CounterVec
	PerTenantRequestCount  grpc.UnaryServerInterceptor
	PerTenantStreamRequest grpc.StreamServerInterceptor
}

func NewServerInterceptors(r prometheus.Registerer) *ServerInterceptors {
	requestCount := promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Subsystem: "index_gateway",
		Name:      "requests_total",
		Help:      "Total amount of requests served by the index gateway",
	}, []string{"operation", "status", "tenant"})

	recordMetric := func(ctx context.Context, method string, err error) {
		tenantID, tenantErr := tenant.TenantID(ctx)
		if tenantErr != nil {
			return
		}
		status := "success"
		if err != nil {
			status = "error"
		}
		requestCount.WithLabelValues(method, status, tenantID).Inc()
	}

	perTenantRequestCount := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		resp, err = handler(ctx, req)
		recordMetric(ctx, info.FullMethod, err)
		return
	}

	perTenantStreamRequest := func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		err := handler(srv, ss)
		recordMetric(ss.Context(), info.FullMethod, err)
		return err
	}

	return &ServerInterceptors{
		reqCount:               requestCount,
		PerTenantRequestCount:  perTenantRequestCount,
		PerTenantStreamRequest: perTenantStreamRequest,
	}
}
