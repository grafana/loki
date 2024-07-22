package health

import (
	"github.com/grafana/dskit/services"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

type Service interface {
	SetServingStatus(string, grpc_health_v1.HealthCheckResponse_ServingStatus)
}

type noopService struct{}

var NoOpService = noopService{}

func (noopService) SetServingStatus(string, grpc_health_v1.HealthCheckResponse_ServingStatus) {}

func NewGRPCHealthService() *GRPCHealthService {
	s := health.NewServer()
	return &GRPCHealthService{
		Server: s,
		Service: services.NewIdleService(nil, func(error) error {
			s.Shutdown()
			return nil
		}),
	}
}

type GRPCHealthService struct {
	services.Service
	*health.Server
}
