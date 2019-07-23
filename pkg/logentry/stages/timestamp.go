package stages

import (
	"errors"
	"fmt"
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
	ErrInvalidLocation           = "invalid location specified: %v"

	Unix   = "Unix"
	UnixMs = "UnixMs"
	UnixNs = "UnixNs"
)

// TimestampConfig configures timestamp extraction
type TimestampConfig struct {
	Source   string  `mapstructure:"source"`
	Format   string  `mapstructure:"format"`
	Location *string `mapstructure:"location"`
}

// parser can convert the time string into a time.Time value
type parser func(string) (time.Time, error)

// validateTimestampConfig validates a timestampStage configuration
func validateTimestampConfig(cfg *TimestampConfig) (parser, error) {
	if cfg == nil {
		return nil, errors.New(ErrEmptyTimestampStageConfig)
	}
	if cfg.Source == "" {
		return nil, errors.New(ErrTimestampSourceRequired)
	}
	if cfg.Format == "" {
		return nil, errors.New(ErrTimestampFormatRequired)
	}
	var loc *time.Location
	var err error
	if cfg.Location != nil {
		loc, err = time.LoadLocation(*cfg.Location)
		if err != nil {
			return nil, fmt.Errorf(ErrInvalidLocation, err)
		}
	}
	return convertDateLayout(cfg.Format, loc), nil

}

// newTimestampStage creates a new timestamp extraction pipeline stage.
func newTimestampStage(logger log.Logger, config interface{}) (*timestampStage, error) {
	cfg := &TimestampConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	parser, err := validateTimestampConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &timestampStage{
		cfgs:   cfg,
		logger: logger,
		parser: parser,
	}, nil
}

// timestampStage will set the timestamp using extracted data
type timestampStage struct {
	cfgs   *TimestampConfig
	logger log.Logger
	parser parser
}

// Process implements Stage
func (ts *timestampStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	if ts.cfgs == nil {
		return
	}
	if v, ok := extracted[ts.cfgs.Source]; ok {
		s, err := getString(v)
		if err != nil {
			level.Debug(ts.logger).Log("msg", "failed to convert extracted time to string", "err", err, "type", reflect.TypeOf(v).String())
		}

		parsedTs, err := ts.parser(s)
		if err != nil {
			level.Debug(ts.logger).Log("msg", "failed to parse time", "err", err, "format", ts.cfgs.Format, "value", s)
		} else {
			*t = parsedTs
		}
	} else {
		level.Debug(ts.logger).Log("msg", "extracted data did not contain a timestamp")
	}
}

// Name implements Stage
func (ts *timestampStage) Name() string {
	return StageTypeTimestamp
}
