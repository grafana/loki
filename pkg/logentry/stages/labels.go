package stages

import (
	"fmt"
	"reflect"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

const (
	ErrEmptyLabelStageConfig = "label stage config cannot be empty"
	ErrInvalidLabelName      = "invalid label name: %s"
)

// LabelsConfig is a set of labels to be extracted
type LabelsConfig map[string]*string

func validateLabelsConfig(c LabelsConfig) error {
	if c == nil {
		return errors.New(ErrEmptyLabelStageConfig)
	}
	for labelName, labelSrc := range c {
		if !model.LabelName(labelName).IsValid() {
			return fmt.Errorf(ErrInvalidLabelName, labelName)
		}
		// If no label source was specified, use the key name
		if labelSrc == nil || *labelSrc == "" {
			lName := labelName
			c[labelName] = &lName
		}
	}
	return nil
}

// newLabel creates a new label stage to set labels from extracted data
func newLabel(logger log.Logger, configs interface{}) (*labelStage, error) {
	cfgs := &LabelsConfig{}
	err := mapstructure.Decode(configs, cfgs)
	if err != nil {
		return nil, err
	}
	err = validateLabelsConfig(*cfgs)
	if err != nil {
		return nil, err
	}
	return &labelStage{
		cfgs:   *cfgs,
		logger: logger,
	}, nil
}

type labelStage struct {
	cfgs   LabelsConfig
	logger log.Logger
}

func (l *labelStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	for lName, lSrc := range l.cfgs {
		if _, ok := extracted[*lSrc]; ok {
			lValue := extracted[*lSrc]
			s, err := getString(lValue)
			if err != nil {
				level.Debug(l.logger).Log("msg", "failed to convert extracted label value to string", "err", err, "type", reflect.TypeOf(lValue).String())
			}
			labelValue := model.LabelValue(s)
			if !labelValue.IsValid() {
				level.Debug(l.logger).Log("msg", "invalid label value parsed", "value", labelValue)
				continue
			}
			labels[model.LabelName(lName)] = labelValue
		}
	}
}
