package stages

import (
	"reflect"
	"regexp"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

// Config Errors
const (
	ErrExpressionRequired    = "expression is required"
	ErrCouldNotCompileRegex  = "could not compile regular expression"
	ErrEmptyRegexStageConfig = "empty regex stage configuration"
	ErrEmptyRegexStageSource = "empty source"
)

// RegexConfig contains a regexStage configuration
type RegexConfig struct {
	Expression string  `mapstructure:"expression"`
	Source     *string `mapstructure:"source"`
}

// validateRegexConfig validates the config and return a regex
func validateRegexConfig(c *RegexConfig) (*regexp.Regexp, error) {
	if c == nil {
		return nil, errors.New(ErrEmptyRegexStageConfig)
	}

	if c.Expression == "" {
		return nil, errors.New(ErrExpressionRequired)
	}

	if c.Source != nil && *c.Source == "" {
		return nil, errors.New(ErrEmptyRegexStageSource)
	}

	expr, err := regexp.Compile(c.Expression)
	if err != nil {
		return nil, errors.Wrap(err, ErrCouldNotCompileRegex)
	}

	return expr, nil
}

// regexStage sets extracted data using regular expressions
type regexStage struct {
	cfg        *RegexConfig
	expression *regexp.Regexp
	logger     log.Logger
}

// newRegexStage creates a newRegexStage
func newRegexStage(logger log.Logger, config interface{}) (Stage, error) {
	cfg, err := parseRegexConfig(config)
	if err != nil {
		return nil, err
	}
	expression, err := validateRegexConfig(cfg)
	if err != nil {
		return nil, err
	}
	return toStage(&regexStage{
		cfg:        cfg,
		expression: expression,
		logger:     log.With(logger, "component", "stage", "type", "regex"),
	}), nil
}

// parseRegexConfig processes an incoming configuration into a RegexConfig
func parseRegexConfig(config interface{}) (*RegexConfig, error) {
	cfg := &RegexConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// Process implements Stage
func (r *regexStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	// If a source key is provided, the regex stage should process it
	// from the extracted map, otherwise should fallback to the entry
	input := entry

	if r.cfg.Source != nil {
		if _, ok := extracted[*r.cfg.Source]; !ok {
			if Debug {
				level.Debug(r.logger).Log("msg", "source does not exist in the set of extracted values", "source", *r.cfg.Source)
			}
			return
		}

		value, err := getString(extracted[*r.cfg.Source])
		if err != nil {
			if Debug {
				level.Debug(r.logger).Log("msg", "failed to convert source value to string", "source", *r.cfg.Source, "err", err, "type", reflect.TypeOf(extracted[*r.cfg.Source]))
			}
			return
		}

		input = &value
	}

	if input == nil {
		if Debug {
			level.Debug(r.logger).Log("msg", "cannot parse a nil entry")
		}
		return
	}

	match := r.expression.FindStringSubmatch(*input)
	if match == nil {
		if Debug {
			level.Debug(r.logger).Log("msg", "regex did not match", "input", *input, "regex", r.expression)
		}
		return
	}

	for i, name := range r.expression.SubexpNames() {
		if i != 0 && name != "" {
			extracted[name] = match[i]
		}
	}

}

// Name implements Stage
func (r *regexStage) Name() string {
	return StageTypeRegex
}
