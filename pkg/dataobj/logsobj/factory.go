package logsobj

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/scratch"
)

// A BuilderFactory is used to create builders.
type BuilderFactory struct {
	cfg          BuilderBaseConfig
	scratchStore scratch.Store
	overrides    TenantOverrides
	logger       log.Logger
	metrics      *BuilderMetrics
}

// NewBuilderFactory validates the config, reports related metrics, and returns a factory that is prepared to create new
// builders.
func NewBuilderFactory(
	cfg BuilderBaseConfig,
	scratchStore scratch.Store,
	metrics *BuilderMetrics,
	logger log.Logger,
	overrides TenantOverrides,
) (*BuilderFactory, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	metrics.ObserveConfig(cfg)

	if overrides != nil {
		level.Info(logger).Log("msg", "tenant overrides wired to builder factory at construction")
	} else {
		level.Info(logger).Log("msg", "no tenant overrides at construction")
	}

	return &BuilderFactory{
		cfg:          cfg,
		scratchStore: scratchStore,
		metrics:      metrics,
		logger:       logger,
		overrides:    overrides,
	}, nil
}

// NewBuilder returns a new builder, or an error. The returned builder shares
// the factory's [BuilderMetrics].
func (f *BuilderFactory) NewBuilder() (*Builder, error) {
	return NewBuilder(f.cfg, f.scratchStore, f.metrics, f.logger, f.overrides)
}
