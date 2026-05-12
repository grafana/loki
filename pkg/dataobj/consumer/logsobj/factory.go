package logsobj

import (
	"github.com/grafana/loki/v3/pkg/scratch"
)

// A BuilderFactory is used to create builders.
type BuilderFactory struct {
	cfg          BuilderConfig
	scratchStore scratch.Store
	metrics      *BuilderMetrics
}

// NewBuilderFactory validates the config, reports related metrics, and returns a factory that is prepared to create new
// builders.
func NewBuilderFactory(cfg BuilderConfig, scratchStore scratch.Store, metrics *BuilderMetrics) (*BuilderFactory, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	metrics.ObserveConfig(cfg)

	return &BuilderFactory{
		cfg:          cfg,
		scratchStore: scratchStore,
		metrics:      metrics,
	}, nil
}

// NewBuilder returns a new builder, or an error. The returned builder shares
// the factory's [BuilderMetrics].
func (f *BuilderFactory) NewBuilder() (*Builder, error) {
	return NewBuilder(f.cfg, f.scratchStore, f.metrics)
}

// NewSorterBuilder returns a new builder with "fake" non-registered metrics.
// TODO(ivkalita): This is temporary to prevent "sorting" builder metrics from messing up the real builder metrics.
func (f *BuilderFactory) NewSorterBuilder() (*Builder, error) {
	return NewBuilder(f.cfg, f.scratchStore, NewBuilderMetrics())
}
