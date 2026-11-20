package stages

import (
	"regexp"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

const (
	// ErrEmptyLabelDropStageConfig error returned if config is empty
	ErrEmptyLabelDropStageConfig = "labeldrop stage config cannot be empty"
)

// LabelDropConfig is a slice of labels to be dropped
type LabelDropConfig []string
type LabelPatternDropConfig []string

func validateLabelDropConfig(c LabelDropConfig) error {
	if len(c) < 1 {
		return errors.New(ErrEmptyLabelDropStageConfig)
	}

	return nil
}

func newLabelDropStage(configs interface{}, patternConfigs interface{}) (Stage, error) {
	cfgs := &LabelDropConfig{}
	patternCfgs := &LabelPatternDropConfig{}

	err := mapstructure.Decode(configs, cfgs)
	if err != nil {
		return nil, err
	}

	err = mapstructure.Decode(patternConfigs, patternCfgs)
	if err != nil {
		return nil, err
	}

	err = validateLabelDropConfig(*cfgs)
	if err != nil {
		return nil, err
	}

	return toStage(&labelDropStage{
		cfgs: *cfgs,
		patternCfgs: *patternCfgs,
	}), nil
}

type labelDropStage struct {
	cfgs LabelDropConfig
	patternCfgs LabelPatternDropConfig
}

// Process implements Stage
func (l *labelDropStage) Process(labels model.LabelSet, _ map[string]interface{}, _ *time.Time, _ *string) {
	for _, label := range l.cfgs {
		delete(labels, model.LabelName(label))
	}
	for _, pattern := range l.patternCfgs {
		deleteByPattern(labels, pattern)
	}
}

// Name implements Stage
func (l *labelDropStage) Name() string {
	return StageTypeLabelDrop
}

func deleteByPattern(labels model.LabelSet, pattern string) {
	regex := regexp.MustCompile(pattern)
	for label := range labels {
		if regex.MatchString(string(label)) {
			delete(labels, label)
		}
	}
}
