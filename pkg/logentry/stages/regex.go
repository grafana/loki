package stages

import (
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
)

// RegexConfig contains a regexStage configuration
type RegexConfig struct {
	Expression string `mapstructure:"expression"`
}

// validateRegexConfig validates the config and return a regex
func validateRegexConfig(c *RegexConfig) (*regexp.Regexp, error) {
	if c == nil {
		return nil, errors.New(ErrEmptyRegexStageConfig)
	}

	if c.Expression == "" {
		return nil, errors.New(ErrExpressionRequired)
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
	return &regexStage{
		cfg:        cfg,
		expression: expression,
		logger:     log.With(logger, "component", "stage", "type", "regex"),
	}, nil
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
	if entry == nil {
		level.Debug(r.logger).Log("msg", "cannot parse a nil entry")
		return
	}

	match := r.expression.FindStringSubmatch(*entry)
	if match == nil {
		level.Debug(r.logger).Log("msg", "regex did not match")
		return
	}

	for i, name := range r.expression.SubexpNames() {
		if i != 0 && name != "" {
			extracted[name] = match[i]
		}
	}

}
