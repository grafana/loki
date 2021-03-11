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

// validateLabelsConfig validates the Label stage configuration
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

// newLabelStage creates a new label stage to set labels from extracted data
func newLabelStage(logger log.Logger, configs interface{}) (Stage, error) {
	cfgs := &LabelsConfig{}
	err := mapstructure.Decode(configs, cfgs)
	if err != nil {
		return nil, err
	}
	err = validateLabelsConfig(*cfgs)
	if err != nil {
		return nil, err
	}
	return toStage(&labelStage{
		cfgs:   *cfgs,
		logger: logger,
	}), nil
}

// labelStage sets labels from extracted data
type labelStage struct {
	cfgs   LabelsConfig
	logger log.Logger
}

// Process implements Stage
func (l *labelStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	for lName, lSrc := range l.cfgs {
		if lValue, ok := extracted[*lSrc]; ok {
			s, err := getString(lValue)
			if err != nil {
				if Debug {
					level.Debug(l.logger).Log("msg", "failed to convert extracted label value to string", "err", err, "type", reflect.TypeOf(lValue))
				}
				continue
			}
			labelValue := model.LabelValue(s)
			if !labelValue.IsValid() {
				if Debug {
					level.Debug(l.logger).Log("msg", "invalid label value parsed", "value", labelValue)
				}
				continue
			}
			labels[model.LabelName(lName)] = labelValue
		}
	}
}

// Name implements Stage
func (l *labelStage) Name() string {
	return StageTypeLabel
}
