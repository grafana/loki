package stages

import (
	"fmt"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

const (
	// ErrEmptyStaticLabelStageConfig error returned if config is empty
	ErrEmptyStaticLabelStageConfig = "static_labels stage config cannot be empty"
)

// StaticLabelConfig is a slice of static-labels to be included
type StaticLabelConfig map[string]*string

func validateLabelStaticConfig(c StaticLabelConfig) error {
	if c == nil {
		return errors.New(ErrEmptyStaticLabelStageConfig)
	}
	for labelName := range c {
		if !model.UTF8Validation.IsValidLabelName(labelName) {
			return fmt.Errorf(ErrInvalidLabelName, labelName)
		}
	}
	return nil
}

func newStaticLabelsStage(configs interface{}) (Stage, error) {
	cfgs := &StaticLabelConfig{}
	err := mapstructure.Decode(configs, cfgs)
	if err != nil {
		return nil, err
	}

	err = validateLabelStaticConfig(*cfgs)
	if err != nil {
		return nil, err
	}

	return toStage(&StaticLabelStage{
		cfgs: *cfgs,
	}), nil
}

type StaticLabelStage struct {
	cfgs StaticLabelConfig
}

// Process implements Stage
func (l *StaticLabelStage) Process(labels model.LabelSet, _ map[string]interface{}, _ *time.Time, _ *string) {

	for lName, lSrc := range l.cfgs {
		if lSrc == nil || *lSrc == "" {
			continue
		}
		s, err := getString(*lSrc)
		if err != nil {
			continue
		}
		lvalue := model.LabelValue(s)
		if !lvalue.IsValid() {
			continue
		}
		lname := model.LabelName(lName)
		labels[lname] = lvalue
	}
}

// Name implements Stage
func (l *StaticLabelStage) Name() string {
	return StageTypeStaticLabels
}
