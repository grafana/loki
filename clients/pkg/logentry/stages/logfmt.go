package stages

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/go-logfmt/logfmt"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

// Config Errors
const (
	ErrMappingRequired        = "logfmt mapping is required"
	ErrEmptyLogfmtStageConfig = "empty logfmt stage configuration"
	ErrEmptyLogfmtStageSource = "empty source"
)

// LogfmtConfig represents a logfmt Stage configuration
type LogfmtConfig struct {
	Mapping map[string]string `mapstructure:"mapping"`
	Source  *string           `mapstructure:"source"`
}

// validateLogfmtConfig validates a logfmt stage config and returns an inverse mapping of configured mapping.
// Mapping inverse is done to make lookup easier. The key would be the key from parsed logfmt and
// value would be the key with which the data in extracted map would be set.
func validateLogfmtConfig(c *LogfmtConfig) (map[string]string, error) {
	if c == nil {
		return nil, errors.New(ErrEmptyLogfmtStageConfig)
	}

	if len(c.Mapping) == 0 {
		return nil, errors.New(ErrMappingRequired)
	}

	if c.Source != nil && *c.Source == "" {
		return nil, errors.New(ErrEmptyLogfmtStageSource)
	}

	inverseMapping := make(map[string]string)
	for k, v := range c.Mapping {
		// if value is not set, use the key for setting data in extracted map.
		if v == "" {
			v = k
		}
		inverseMapping[v] = k
	}

	return inverseMapping, nil
}

// logfmtStage sets extracted data using logfmt parser
type logfmtStage struct {
	cfg            *LogfmtConfig
	inverseMapping map[string]string
	logger         log.Logger
}

// newLogfmtStage creates a new logfmt pipeline stage from a config.
func newLogfmtStage(logger log.Logger, config interface{}) (Stage, error) {
	cfg, err := parseLogfmtConfig(config)
	if err != nil {
		return nil, err
	}

	// inverseMapping would hold the mapping in inverse which would make lookup easier.
	// To explain it simply, the key would be the key from parsed logfmt and value would be the key with which the data in extracted map would be set.
	inverseMapping, err := validateLogfmtConfig(cfg)
	if err != nil {
		return nil, err
	}

	return toStage(&logfmtStage{
		cfg:            cfg,
		inverseMapping: inverseMapping,
		logger:         log.With(logger, "component", "stage", "type", "logfmt"),
	}), nil
}

func parseLogfmtConfig(config interface{}) (*LogfmtConfig, error) {
	cfg := &LogfmtConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// Process implements Stage
func (j *logfmtStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	// If a source key is provided, the logfmt stage should process it
	// from the extracted map, otherwise should fallback to the entry
	input := entry

	if j.cfg.Source != nil {
		if _, ok := extracted[*j.cfg.Source]; !ok {
			if Debug {
				level.Debug(j.logger).Log("msg", "source does not exist in the set of extracted values", "source", *j.cfg.Source)
			}
			return
		}

		value, err := getString(extracted[*j.cfg.Source])
		if err != nil {
			if Debug {
				level.Debug(j.logger).Log("msg", "failed to convert source value to string", "source", *j.cfg.Source, "err", err, "type", reflect.TypeOf(extracted[*j.cfg.Source]))
			}
			return
		}

		input = &value
	}

	if input == nil {
		if Debug {
			level.Debug(j.logger).Log("msg", "cannot parse a nil entry")
		}
		return
	}
	decoder := logfmt.NewDecoder(strings.NewReader(*input))
	extractedEntriesCount := 0
	for decoder.ScanRecord() {
		for decoder.ScanKeyval() {
			mapKey, ok := j.inverseMapping[string(decoder.Key())]
			if ok {
				extracted[mapKey] = string(decoder.Value())
				extractedEntriesCount++
			}
		}
	}

	if decoder.Err() != nil {
		level.Error(j.logger).Log("msg", "failed to decode logfmt", "err", decoder.Err())
		return
	}

	if Debug {
		if extractedEntriesCount != len(j.inverseMapping) {
			level.Debug(j.logger).Log("msg", fmt.Sprintf("found only %d out of %d configured mappings in logfmt stage", extractedEntriesCount, len(j.inverseMapping)))
		}
		level.Debug(j.logger).Log("msg", "extracted data debug in logfmt stage", "extracted data", fmt.Sprintf("%v", extracted))
	}
}

// Name implements Stage
func (j *logfmtStage) Name() string {
	return StageTypeLogfmt
}
