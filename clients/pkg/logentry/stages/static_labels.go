package stages

import (
	"fmt"
	"reflect"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
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
		if !model.LabelName(labelName).IsValid() {
			return fmt.Errorf(ErrInvalidLabelName, labelName)
		}
	}
	return nil
}

func newStaticLabelsStage(logger log.Logger, configs interface{}) (Stage, error) {
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
		cfgs:   *cfgs,
		logger: logger,
	}), nil
}

type StaticLabelStage struct {
	cfgs   StaticLabelConfig
	logger log.Logger
}

// Process implements Stage
func (l *StaticLabelStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {

	for lName, lSrc := range l.cfgs {
		if lSrc == nil || *lSrc == "" {
			continue
		}
		s, err := getString(*lSrc)
		if err != nil {
			if Debug {
				level.Debug(l.logger).Log("msg", "failed to convert static label value to string", "err", err, "type", reflect.TypeOf(lSrc))
			}
			continue
		}
		lvalue := model.LabelValue(s)
		if !lvalue.IsValid() {
			if Debug {
				level.Debug(l.logger).Log("msg", "invalid label value parsed", "value", lvalue)
			}
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
