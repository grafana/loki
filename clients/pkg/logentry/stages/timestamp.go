package stages

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/mitchellh/mapstructure"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/util"
)

const (
	ErrEmptyTimestampStageConfig = "timestamp stage config cannot be empty"
	ErrTimestampSourceRequired   = "timestamp source value is required if timestamp is specified"
	ErrTimestampFormatRequired   = "timestamp format is required"
	ErrInvalidLocation           = "invalid location specified: %v"
	ErrInvalidActionOnFailure    = "invalid action on failure (supported values are %v)"
	ErrTimestampSourceMissing    = "extracted data did not contain a timestamp"
	ErrTimestampConversionFailed = "failed to convert extracted time to string"
	ErrTimestampParsingFailed    = "failed to parse time"

	Unix   = "Unix"
	UnixMs = "UnixMs"
	UnixUs = "UnixUs"
	UnixNs = "UnixNs"

	TimestampActionOnFailureSkip    = "skip"
	TimestampActionOnFailureFudge   = "fudge"
	TimestampActionOnFailureDefault = TimestampActionOnFailureFudge

	// Maximum number of "streams" for which we keep the last known timestamp
	maxLastKnownTimestampsCacheSize = 10000
)

var (
	TimestampActionOnFailureOptions = []string{TimestampActionOnFailureSkip, TimestampActionOnFailureFudge}
)

// TimestampConfig configures timestamp extraction
type TimestampConfig struct {
	Source          string   `mapstructure:"source"`
	Format          string   `mapstructure:"format"`
	FallbackFormats []string `mapstructure:"fallback_formats"`
	Location        *string  `mapstructure:"location"`
	ActionOnFailure *string  `mapstructure:"action_on_failure"`
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

	// Validate the action on failure and enforce the default
	if cfg.ActionOnFailure == nil {
		cfg.ActionOnFailure = util.StringRef(TimestampActionOnFailureDefault)
	} else {
		if !util.StringsContain(TimestampActionOnFailureOptions, *cfg.ActionOnFailure) {
			return nil, fmt.Errorf(ErrInvalidActionOnFailure, TimestampActionOnFailureOptions)
		}
	}

	if len(cfg.FallbackFormats) > 0 {
		multiConvertDateLayout := func(input string) (time.Time, error) {
			orignalTime, originalErr := convertDateLayout(cfg.Format, loc)(input)
			if originalErr == nil {
				return orignalTime, originalErr
			}
			for i := 0; i < len(cfg.FallbackFormats); i++ {
				if t, err := convertDateLayout(cfg.FallbackFormats[i], loc)(input); err == nil {
					return t, err
				}
			}
			return orignalTime, originalErr
		}
		return multiConvertDateLayout, nil
	}

	return convertDateLayout(cfg.Format, loc), nil
}

// newTimestampStage creates a new timestamp extraction pipeline stage.
func newTimestampStage(logger log.Logger, config interface{}) (Stage, error) {
	cfg := &TimestampConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	parser, err := validateTimestampConfig(cfg)
	if err != nil {
		return nil, err
	}

	var lastKnownTimestamps *lru.Cache[string, time.Time]
	if *cfg.ActionOnFailure == TimestampActionOnFailureFudge {
		lastKnownTimestamps, err = lru.New[string, time.Time](maxLastKnownTimestampsCacheSize)
		if err != nil {
			return nil, err
		}
	}

	return toStage(&timestampStage{
		cfg:                 cfg,
		logger:              logger,
		parser:              parser,
		lastKnownTimestamps: lastKnownTimestamps,
	}), nil
}

// timestampStage will set the timestamp using extracted data
type timestampStage struct {
	cfg    *TimestampConfig
	logger log.Logger
	parser parser

	// Stores the last known timestamp for a given "stream id" (guessed, since at this stage
	// there's no reliable way to know it).
	lastKnownTimestamps *lru.Cache[string, time.Time]
}

// Name implements Stage
func (ts *timestampStage) Name() string {
	return StageTypeTimestamp
}

// Process implements Stage
func (ts *timestampStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, _ *string) {
	if ts.cfg == nil {
		return
	}

	parsedTs, err := ts.parseTimestampFromSource(extracted)
	if err != nil {
		ts.processActionOnFailure(labels, t)
		return
	}

	// Update the log entry timestamp with the parsed one
	*t = *parsedTs

	// The timestamp has been correctly parsed, so we should store it in the map
	// containing the last known timestamp used by the "fudge" action on failure.
	if *ts.cfg.ActionOnFailure == TimestampActionOnFailureFudge {
		ts.lastKnownTimestamps.Add(labels.String(), *t)
	}
}

func (ts *timestampStage) parseTimestampFromSource(extracted map[string]interface{}) (*time.Time, error) {
	// Ensure the extracted data contains the timestamp source
	v, ok := extracted[ts.cfg.Source]
	if !ok {
		if Debug {
			level.Debug(ts.logger).Log("msg", ErrTimestampSourceMissing)
		}

		return nil, errors.New(ErrTimestampSourceMissing)
	}

	// Convert the timestamp source to string (if it's not a string yet)
	s, err := getString(v)
	if err != nil {
		if Debug {
			level.Debug(ts.logger).Log("msg", ErrTimestampConversionFailed, "err", err, "type", reflect.TypeOf(v))
		}

		return nil, errors.New(ErrTimestampConversionFailed)
	}

	// Parse the timestamp source according to the configured format
	parsedTs, err := ts.parser(s)
	if err != nil {
		if Debug {
			level.Debug(ts.logger).Log("msg", ErrTimestampParsingFailed, "err", err, "format", ts.cfg.Format, "value", s)
		}

		return nil, errors.New(ErrTimestampParsingFailed)
	}

	return &parsedTs, nil
}

func (ts *timestampStage) processActionOnFailure(labels model.LabelSet, t *time.Time) {
	switch *ts.cfg.ActionOnFailure {
	case TimestampActionOnFailureFudge:
		ts.processActionOnFailureFudge(labels, t)
	case TimestampActionOnFailureSkip:
		// Nothing to do
	}
}

func (ts *timestampStage) processActionOnFailureFudge(labels model.LabelSet, t *time.Time) {
	labelsStr := labels.String()
	lastTimestamp, ok := ts.lastKnownTimestamps.Get(labelsStr)

	// If the last known timestamp is unknown (ie. has not been successfully parsed yet)
	// there's nothing we can do, so we're going to keep the current timestamp
	if !ok {
		return
	}

	// Fudge the timestamp
	*t = lastTimestamp.Add(1 * time.Nanosecond)

	// Store the fudged timestamp, so that a subsequent fudged timestamp will be 1ns after it
	ts.lastKnownTimestamps.Add(labelsStr, *t)
}
