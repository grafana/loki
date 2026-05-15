package logsobj

import (
	"fmt"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/scratch"
)

// A BuilderFactory is used to create builders.
type BuilderFactory struct {
	cfg          BuilderConfig
	scratchStore scratch.Store
	overrides    TenantOverrides
	logger       log.Logger
}

// SetOverrides configures per-tenant overrides propagated to every builder
// created by this factory.
func (f *BuilderFactory) SetOverrides(overrides TenantOverrides) {
	f.overrides = overrides
}

func NewBuilderFactory(cfg BuilderConfig, scratchStore scratch.Store, logger log.Logger) *BuilderFactory {
	return &BuilderFactory{
		cfg:          cfg,
		scratchStore: scratchStore,
		logger:       logger,
	}
}

// NewBuilder returns a new builder, or an error. The registerer is optional.
// No metrics will be registered if the registerer is nil.
func (f *BuilderFactory) NewBuilder(r prometheus.Registerer) (*Builder, error) {
	b, err := NewBuilder(f.cfg, f.scratchStore)
	if err != nil {
		return nil, err
	}
	if f.overrides != nil {
		b.SetOverrides(f.overrides)
	}
	b.SetLogger(f.logger)
	if r != nil {
		if err = b.RegisterMetrics(r); err != nil {
			return nil, fmt.Errorf("failed to register metrics: %w", err)
		}
	}
	return b, nil
}
