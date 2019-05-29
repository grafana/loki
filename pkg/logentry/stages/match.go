package stages

import (
	"time"

	"github.com/go-kit/kit/log"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/loki/pkg/logql"
)

const (
	ErrEmptyMatchStageConfig = "match stage config cannot be empty"
	ErrPipelineNameRequired  = "match stage must specify a pipeline name which is used in the exported metrics"
	ErrSelectorRequired      = "selector statement required for match stage"
	ErrMatchRequiresStages   = "match stage requires at least one additional stage to be defined in '- stages'"
	ErrSelectorSyntax        = "invalid selector syntax for match stage"
)

type MatcherConfig struct {
	PipelineName *string        `mapstructure:"pipeline_name"`
	Selector     *string        `mapstructure:"selector"`
	Stages       PipelineStages `mapstructure:"stages"`
}

func validateMatcherConfig(cfg *MatcherConfig) ([]*labels.Matcher, error) {
	if cfg == nil {
		return nil, errors.New(ErrEmptyMatchStageConfig)
	}
	//FIXME PipelineName does not need to be a pointer
	if cfg.PipelineName == nil || *cfg.PipelineName == "" {
		return nil, errors.New(ErrPipelineNameRequired)
	}
	//FIXME selector does not need to be a pointer
	if cfg.Selector == nil || *cfg.Selector == "" {
		return nil, errors.New(ErrSelectorRequired)
	}
	if cfg.Stages == nil || len(cfg.Stages) == 0 {
		return nil, errors.New(ErrMatchRequiresStages)
	}
	matchers, err := logql.ParseMatchers(*cfg.Selector)
	if err != nil {
		return nil, errors.Wrap(err, ErrSelectorSyntax)
	}
	return matchers, nil
}

func newMatcherStage(logger log.Logger, config interface{}, registerer prometheus.Registerer) (Stage, error) {
	cfg := &MatcherConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	matchers, err := validateMatcherConfig(cfg)
	if err != nil {
		return nil, err
	}

	pl, err := NewPipeline(logger, cfg.Stages, *cfg.PipelineName, registerer)
	if err != nil {
		return nil, errors.Wrapf(err, "match stage %s failed to create pipeline", *cfg.PipelineName)
	}

	return &matcherStage{
		cfgs:     cfg,
		logger:   logger,
		matchers: matchers,
		pipeline: pl,
	}, nil
}

type matcherStage struct {
	cfgs     *MatcherConfig
	matchers []*labels.Matcher
	logger   log.Logger
	pipeline Stage
}

func (m *matcherStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	for _, filter := range m.matchers {
		if !filter.Matches(string(labels[model.LabelName(filter.Name)])) {
			return
		}
	}
	m.pipeline.Process(labels, extracted, t, entry)
}
