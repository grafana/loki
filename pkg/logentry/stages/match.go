package stages

import (
	"time"

	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/go-kit/kit/log"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/util"
)

const (
	ErrEmptyMatchStageConfig = "match stage config cannot be empty"
	ErrPipelineNameRequired  = "match stage pipeline name can be omitted but cannot be an empty string"
	ErrSelectorRequired      = "selector statement required for match stage"
	ErrMatchRequiresStages   = "match stage requires at least one additional stage to be defined in '- stages'"
	ErrSelectorSyntax        = "invalid selector syntax for match stage"
	ErrStagesWithDropLine    = "match stage configured to drop entries cannot contains stages"
	ErrUnknownMatchAction    = "match stage action should be 'keep' or 'drop'"
	MatchActionKeep          = "keep"
	MatchActionDrop          = "drop"
)

// MatcherConfig contains the configuration for a matcherStage
type MatcherConfig struct {
	PipelineName *string        `mapstructure:"pipeline_name"`
	Selector     string         `mapstructure:"selector"`
	Stages       PipelineStages `mapstructure:"stages"`
	Action       string         `mapstructure:"action"`
	DropReason   *string        `mapstructure:"drop_counter_reason"`
}

// validateMatcherConfig validates the MatcherConfig for the matcherStage
func validateMatcherConfig(cfg *MatcherConfig) (logql.LogSelectorExpr, error) {
	if cfg == nil {
		return nil, errors.New(ErrEmptyMatchStageConfig)
	}
	if cfg.PipelineName != nil && *cfg.PipelineName == "" {
		return nil, errors.New(ErrPipelineNameRequired)
	}
	if cfg.Selector == "" {
		return nil, errors.New(ErrSelectorRequired)
	}
	switch cfg.Action {
	case MatchActionKeep, MatchActionDrop:
	case "":
		cfg.Action = MatchActionKeep
	default:
		return nil, errors.New(ErrUnknownMatchAction)
	}

	if cfg.Action == MatchActionKeep && (cfg.Stages == nil || len(cfg.Stages) == 0) {
		return nil, errors.New(ErrMatchRequiresStages)
	}
	if cfg.Action == MatchActionDrop && (cfg.Stages != nil && len(cfg.Stages) != 0) {
		return nil, errors.New(ErrStagesWithDropLine)
	}

	selector, err := logql.ParseLogSelector(cfg.Selector)
	if err != nil {
		return nil, errors.Wrap(err, ErrSelectorSyntax)
	}
	return selector, nil
}

// newMatcherStage creates a new matcherStage from config
func newMatcherStage(logger log.Logger, jobName *string, config interface{}, registerer prometheus.Registerer) (Stage, error) {
	cfg := &MatcherConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	selector, err := validateMatcherConfig(cfg)
	if err != nil {
		return nil, err
	}

	var nPtr *string
	if cfg.PipelineName != nil && jobName != nil {
		name := *jobName + "_" + *cfg.PipelineName
		nPtr = &name
	}

	var pl *Pipeline
	if cfg.Action == MatchActionKeep {
		var err error
		pl, err = NewPipeline(logger, cfg.Stages, nPtr, registerer)
		if err != nil {
			return nil, errors.Wrapf(err, "match stage failed to create pipeline from config: %v", config)
		}
	}

	pipeline, err := selector.Pipeline()
	if err != nil {
		return nil, errors.Wrap(err, "error parsing pipeline")
	}

	dropReason := "match_stage"
	if cfg.DropReason != nil && *cfg.DropReason != "" {
		dropReason = *cfg.DropReason
	}

	return &matcherStage{
		dropReason: dropReason,
		matchers:   selector.Matchers(),
		stage:      pl,
		action:     cfg.Action,
		pipeline:   pipeline,
	}, nil
}

// matcherStage applies Label matchers to determine if the include stages should be run
type matcherStage struct {
	dropReason string
	matchers   []*labels.Matcher
	pipeline   logql.Pipeline
	stage      Stage
	action     string
}

// Process implements Stage
func (m *matcherStage) Process(lbs model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	for _, filter := range m.matchers {
		if !filter.Matches(string(lbs[model.LabelName(filter.Name)])) {
			return
		}
	}

	if newLine, newLabels, ok := m.pipeline.Process([]byte(*entry), labels.FromMap(util.ModelLabelSetToMap(lbs))); ok {
		switch m.action {
		case MatchActionDrop:
			// Adds the drop label to not be sent by the api.EntryHandler
			lbs[dropLabel] = model.LabelValue(m.dropReason)
		case MatchActionKeep:
			*entry = string(newLine)
			for k := range lbs {
				delete(lbs, k)
			}
			for _, l := range newLabels {
				lbs[model.LabelName(l.Name)] = model.LabelValue(l.Value)
			}
			m.stage.Process(lbs, extracted, t, entry)
		}
	}
}

// Name implements Stage
func (m *matcherStage) Name() string {
	return StageTypeMatch
}
