// Package frontend contains provides a frontend service for ingest limits.
// It is responsible for receiving and answering gRPC requests from distributors,
// such as exceeds limits requests, forwarding them to individual limits backends,
// gathering and aggregating their responses (where required), and returning
// the final result.
package frontend

import (
	"context"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// Frontend is the limits-frontend service, and acts a service wrapper for
// all components needed to run the limits-frontend.
type Frontend struct {
	services.Service
	cfg    Config
	logger log.Logger
	limits IngestLimitsService
}

// NewFrontend returns a new Frontend.
func NewFrontend(cfg Config, logger log.Logger, _ prometheus.Registerer, limits IngestLimitsService) (*Frontend, error) {
	f := &Frontend{
		cfg:    cfg,
		logger: logger,
		limits: limits,
	}
	f.Service = services.NewBasicService(f.starting, f.running, f.stopping)
	return f, nil
}

// starting implements services.Service.
func (f *Frontend) starting(_ context.Context) error {
	return nil
}

// running implements services.Service.
func (f *Frontend) running(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// stopping implements services.Service.
func (f *Frontend) stopping(_ error) error {
	return nil
}

// ExceedsLimits implements logproto.IngestLimitsFrontendClient.
func (f *Frontend) ExceedsLimits(ctx context.Context, r *logproto.ExceedsLimitsRequest) (*logproto.ExceedsLimitsResponse, error) {
	return f.limits.ExceedsLimits(ctx, r)
}
