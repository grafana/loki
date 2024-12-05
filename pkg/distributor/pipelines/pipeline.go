package pipelines

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/distributor/model"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

type Transformer interface {
	Apply(ctx context.Context, streams *model.StreamsBuilder) error
}

var noop Transformer = &NoopProcessor{}

type NoopProcessor struct {
}

// Apply implements Transformer
func (n *NoopProcessor) Apply(_ context.Context, streams *model.StreamsBuilder) error {
	return nil
}

var _ Transformer = &PipelineProcessor{}

type PipelineProcessor struct {
	Pipeline
	tenant   string
	selector labels.Selector
	stages   []Transformer
	compiled bool
}

func (p *PipelineProcessor) Compile() error {
	var err error

	p.selector, err = syntax.ParseMatchers(p.Match, false)
	if err != nil {
		return err
	}

	for _, stage := range p.Stages {
		compiled, err := stage.Compile()
		if err != nil {
			return fmt.Errorf("failed to compile stage %v: %w", stage.Action, err)
		}
		p.stages = append(p.stages, compiled)
	}

	p.compiled = true
	return err
}

// Process implements Transformer
func (p *PipelineProcessor) Apply(ctx context.Context, result *model.StreamsBuilder) error {

	var err error
	for _, stage := range p.stages {
		err = stage.Apply(ctx, result)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *PipelineProcessor) matchesLabels(lbs labels.Labels) bool {
	if !p.compiled {
		return false
	}
	return p.selector.Matches(lbs)
}

type TenantPipelines struct {
	tenant    string
	pipelines []*PipelineProcessor
}

func newTenantPipelines(tenant string, cfg IngestPipelineConfig) *TenantPipelines {
	pipelines := make([]*PipelineProcessor, 0, len(cfg.Pipelines))
	for _, c := range cfg.Pipelines {
		pipeline := &PipelineProcessor{Pipeline: c, tenant: tenant}
		err := pipeline.Compile()
		if err != nil {
			continue
		}
		pipelines = append(pipelines, pipeline)
	}
	return &TenantPipelines{
		tenant:    tenant,
		pipelines: pipelines,
	}
}

func (tp *TenantPipelines) GetMatching(lbs labels.Labels) Transformer {
	for _, p := range tp.pipelines {
		if p.matchesLabels(lbs) {
			return p
		}
	}
	return noop
}

type Factory struct {
	mtx   sync.RWMutex
	rt    RuntimeOverrides
	cache *expirable.LRU[string, *TenantPipelines]
}

func NewFactory(rt RuntimeOverrides) *Factory {
	return &Factory{
		rt:    rt,
		cache: expirable.NewLRU[string, *TenantPipelines](1024, nil, 5*time.Minute),
	}
}

func (f *Factory) Get(tenant string, lbs labels.Labels) Transformer {
	f.mtx.RLock()
	tenantPipelines, ok := f.cache.Get(tenant)
	f.mtx.RUnlock()

	if !ok {
		f.mtx.Lock()
		cfg := f.rt.GetIngestPipelines(tenant)
		tenantPipelines = newTenantPipelines(tenant, cfg)
		f.cache.Add(tenant, tenantPipelines)
		f.mtx.Unlock()
	}

	return tenantPipelines.GetMatching(lbs)
}
