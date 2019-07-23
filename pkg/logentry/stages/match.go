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
	ErrPipelineNameRequired  = "match stage pipeline name can be omitted but cannot be an empty string"
	ErrSelectorRequired      = "selector statement required for match stage"
	ErrMatchRequiresStages   = "match stage requires at least one additional stage to be defined in '- stages'"
	ErrSelectorSyntax        = "invalid selector syntax for match stage"
)

// MatcherConfig contains the configuration for a matcherStage
type MatcherConfig struct {
	PipelineName *string        `mapstructure:"pipeline_name"`
	Selector     string         `mapstructure:"selector"`
	Stages       PipelineStages `mapstructure:"stages"`
}

// validateMatcherConfig validates the MatcherConfig for the matcherStage
func validateMatcherConfig(cfg *MatcherConfig) ([]*labels.Matcher, error) {
	if cfg == nil {
		return nil, errors.New(ErrEmptyMatchStageConfig)
	}
	if cfg.PipelineName != nil && *cfg.PipelineName == "" {
		return nil, errors.New(ErrPipelineNameRequired)
	}
	if cfg.Selector == "" {
		return nil, errors.New(ErrSelectorRequired)
	}
	if cfg.Stages == nil || len(cfg.Stages) == 0 {
		return nil, errors.New(ErrMatchRequiresStages)
	}
	matchers, err := logql.ParseMatchers(cfg.Selector)
	if err != nil {
		return nil, errors.Wrap(err, ErrSelectorSyntax)
	}
	return matchers, nil
}

// newMatcherStage creates a new matcherStage from config
func newMatcherStage(logger log.Logger, jobName *string, config interface{}, registerer prometheus.Registerer) (Stage, error) {
	cfg := &MatcherConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	matchers, err := validateMatcherConfig(cfg)
	if err != nil {
		return nil, err
	}

	var nPtr *string
	if cfg.PipelineName != nil && jobName != nil {
		name := *jobName + "_" + *cfg.PipelineName
		nPtr = &name
	}

	pl, err := NewPipeline(logger, cfg.Stages, nPtr, registerer)
	if err != nil {
		return nil, errors.Wrapf(err, "match stage failed to create pipeline from config: %v", config)
	}

	return &matcherStage{
		matchers: matchers,
		pipeline: pl,
	}, nil
}

// matcherStage applies Label matchers to determine if the include stages should be run
type matcherStage struct {
	matchers []*labels.Matcher
	pipeline Stage
}

// Process implements Stage
func (m *matcherStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	for _, filter := range m.matchers {
		if !filter.Matches(string(labels[model.LabelName(filter.Name)])) {
			return
		}
	}
	m.pipeline.Process(labels, extracted, t, entry)
}

// Name implements Stage
func (m *matcherStage) Name() string {
	return StageTypeMatch
}
