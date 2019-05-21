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
	Source *string `mapstructure:"source"`
}

func validateOutputConfig(cfg *OutputConfig) error {
	if cfg == nil {
		return errors.New(ErrEmptyOutputStageConfig)
	}
	if cfg.Source == nil || *cfg.Source == "" {
		return errors.New(ErrOutputSourceRequired)
	}
	return nil
}

// newLabel creates a new set of metrics to process for each log entry
func newOutput(logger log.Logger, config interface{}) (*outputStage, error) {
	cfg := &OutputConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	err = validateOutputConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &outputStage{
		cfgs:   cfg,
		logger: logger,
	}, nil
}

type outputStage struct {
	cfgs   *OutputConfig
	logger log.Logger
}

func (o *outputStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	if o.cfgs == nil {
		return
	}
	if v, ok := extracted[*o.cfgs.Source]; ok {
		s, err := getString(v)
		if err != nil {
			level.Debug(o.logger).Log("msg", "extracted output could not be converted to a string", "err", err, "type", reflect.TypeOf(v).String())
			return
		}
		*entry = s
	} else {
		//FIXME should this be warn?
		level.Debug(o.logger).Log("msg", "extracted data did not contain output source")
	}
}
