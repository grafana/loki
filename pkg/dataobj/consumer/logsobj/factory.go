package logsobj

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/scratch"
)

// A BuilderFactory is used to create builders.
type BuilderFactory struct {
	cfg          BuilderConfig
	scratchStore scratch.Store
	overrides    TenantOverrides
	logger       log.Logger
	metrics      *BuilderMetrics
}

// NewBuilderFactory validates the config, reports related metrics, and returns a factory that is prepared to create new
// builders.
func NewBuilderFactory(
	cfg BuilderConfig,
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
		level.Info(logger).Log("msg", "sort schema overrides wired to builder factory at construction")
	} else {
		level.Warn(logger).Log("msg", "sort schema overrides not provided at construction; schema sort will not apply to the initial builder")
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
	return f.newBuilderWithMetrics(f.metrics)
}

// NewSorterBuilder returns a new builder with "fake" non-registered metrics.
// TODO(ivkalita): This is temporary to prevent "sorting" builder metrics from messing up the real builder metrics.
func (f *BuilderFactory) NewSorterBuilder() (*Builder, error) {
	return f.newBuilderWithMetrics(NewBuilderMetrics())
}

func (f *BuilderFactory) newBuilderWithMetrics(metrics *BuilderMetrics) (*Builder, error) {
	b, err := NewBuilder(f.cfg, f.scratchStore, metrics)
	if err != nil {
		return nil, err
	}
	if f.overrides != nil {
		b.SetOverrides(f.overrides)
	}
	b.SetLogger(f.logger)
	return b, nil
}
