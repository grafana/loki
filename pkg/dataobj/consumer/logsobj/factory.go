package logsobj

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/scratch"
)

// A BuilderFactory is used to create builders.
type BuilderFactory struct {
	cfg          BuilderConfig
	scratchStore scratch.Store
}

func NewBuilderFactory(cfg BuilderConfig, scratchStore scratch.Store) *BuilderFactory {
	return &BuilderFactory{
		cfg:          cfg,
		scratchStore: scratchStore,
	}
}

// NewBuilder returns a new builder, or an error. The registerer is optional.
// No metrics will be registered if the registerer is nil.
func (f *BuilderFactory) NewBuilder(r prometheus.Registerer) (*Builder, error) {
	b, err := NewBuilder(f.cfg, f.scratchStore)
	if err != nil {
		return nil, err
	}
	if r != nil {
		if err = b.RegisterMetrics(r); err != nil {
			return nil, fmt.Errorf("failed to register metrics: %w", err)
		}
	}
	return b, nil
}
