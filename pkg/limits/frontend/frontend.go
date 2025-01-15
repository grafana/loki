package frontend

import (
	"context"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type Frontend struct {
	services.Service

	cfg    Config
	logger log.Logger
	client IngestLimitsClient
}

func NewFrontend(cfg Config, logger log.Logger) (*Frontend, error) {
	f := &Frontend{
		cfg:    cfg,
		logger: logger,
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

func (f *Frontend) ExceedsLimits(_ context.Context, _ *logproto.ExceedsLimitsRequest) (*logproto.ExceedsLimitsResponse, error) {
	return nil, nil
}
