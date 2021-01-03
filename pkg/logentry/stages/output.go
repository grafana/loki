package stages

import (
	"errors"
	"reflect"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/mitchellh/mapstructure"
	"github.com/prometheus/common/model"
)

// Config Errors
const (
	ErrEmptyOutputStageConfig = "output stage config cannot be empty"
	ErrOutputSourceRequired   = "output source value is required if output is specified"
)

// OutputConfig configures output value extraction
type OutputConfig struct {
	Source string `mapstructure:"source"`
}

// validateOutput validates the outputStage config
func validateOutputConfig(cfg *OutputConfig) error {
	if cfg == nil {
		return errors.New(ErrEmptyOutputStageConfig)
	}
	if cfg.Source == "" {
		return errors.New(ErrOutputSourceRequired)
	}
	return nil
}

// newOutputStage creates a new outputStage
func newOutputStage(logger log.Logger, config interface{}) (Stage, error) {
	cfg := &OutputConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	err = validateOutputConfig(cfg)
	if err != nil {
		return nil, err
	}
	return toStage(&outputStage{
		cfgs:   cfg,
		logger: logger,
	}), nil
}

// outputStage will mutate the incoming entry and set it from extracted data
type outputStage struct {
	cfgs   *OutputConfig
	logger log.Logger
}

// Process implements Stage
func (o *outputStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	if o.cfgs == nil {
		return
	}
	if v, ok := extracted[o.cfgs.Source]; ok {
		s, err := getString(v)
		if err != nil {
			if Debug {
				level.Debug(o.logger).Log("msg", "extracted output could not be converted to a string", "err", err, "type", reflect.TypeOf(v))
			}
			return
		}
		*entry = s
	} else {
		if Debug {
			level.Debug(o.logger).Log("msg", "extracted data did not contain output source")
		}
	}
}

// Name implements Stage
func (o *outputStage) Name() string {
	return StageTypeOutput
}
