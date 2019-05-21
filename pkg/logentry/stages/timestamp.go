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

const (
	ErrEmptyTimestampStageConfig = "timestamp stage config cannot be empty"
	ErrTimestampSourceRequired   = "timestamp source value is required if timestamp is specified"
	ErrTimestampFormatRequired   = "timestamp format is required"
)

// TimestampConfig configures timestamp extraction
type TimestampConfig struct {
	Source *string `mapstructure:"source"`
	Format *string `mapstructure:"format"`
}

func validateTimestampConfig(cfg *TimestampConfig) (string, error) {
	if cfg == nil {
		return "", errors.New(ErrEmptyTimestampStageConfig)
	}
	if cfg.Source == nil || *cfg.Source == "" {
		return "", errors.New(ErrTimestampSourceRequired)
	}
	if cfg.Format == nil || *cfg.Format == "" {
		return "", errors.New(ErrTimestampFormatRequired)
	}
	return convertDateLayout(*cfg.Format), nil

}

// newLabel creates a new set of metrics to process for each log entry
func newTimestamp(logger log.Logger, config interface{}) (*timestampStage, error) {
	cfg := &TimestampConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	format, err := validateTimestampConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &timestampStage{
		cfgs:   cfg,
		logger: logger,
		format: format,
	}, nil
}

type timestampStage struct {
	cfgs   *TimestampConfig
	logger log.Logger
	format string
}

func (ts *timestampStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	if ts.cfgs == nil {
		return
	}
	//FIXME should these be warnings instead of debug??
	if v, ok := extracted[*ts.cfgs.Source]; ok {
		s, err := getString(v)
		if err != nil {
			level.Debug(ts.logger).Log("msg", "failed to convert extracted time to string", "err", err, "type", reflect.TypeOf(v).String())
		}
		parsedTs, err := time.Parse(ts.format, s)
		if err != nil {
			level.Debug(ts.logger).Log("msg", "failed to parse time", "err", err, "format", ts.cfgs.Format, "value", s)
		} else {
			*t = parsedTs
		}
	} else {
		level.Debug(ts.logger).Log("msg", "extracted data did not contain a timestamp")
	}
}
