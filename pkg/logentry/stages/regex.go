package stages

import (
	"fmt"
	"regexp"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

// RegexTimestamp configures timestamp extraction
type RegexTimestamp struct {
	Source *string `mapstructure:"source"`
	Format string  `mapstructure:"format"`
}

// RegexLabel configures a labels value extraction
type RegexLabel struct {
	Source *string `mapstructure:"source"`
}

// RegexOutput configures output value extraction
type RegexOutput struct {
	Source *string `mapstructure:"source"`
}

// RegexConfig configures the log entry parser to extract value from regex
type RegexConfig struct {
	Timestamp  *RegexTimestamp        `mapstructure:"timestamp"`
	Expression string                 `mapstructure:"expression"`
	Output     *RegexOutput           `mapstructure:"output"`
	Labels     map[string]*RegexLabel `mapstructure:"labels"`
}

func newRegexConfig(config interface{}) (*RegexConfig, error) {
	cfg := &RegexConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

const (
	ErrExpressionRequired      = "expression is required"
	ErrCouldNotCompileRegex    = "could not compile regular expression"
	ErrEmptyRegexStageConfig   = "empty regex parser configuration"
	ErrOutputSourceRequired    = "output source value is required if output is specified"
	ErrTimestampSourceRequired = "timestamp source value is required if timestamp is specified"
	ErrTimestampGroupRequired  = "regex must contain a named group to match the timestamp with name: %s"
	ErrTimestampFormatRequired = "timestamp format is required"
	ErrInvalidLabelName        = "invalid label name: %s"
)

// validate the config and return a
func (c *RegexConfig) validate() (*regexp.Regexp, error) {

	if c.Output == nil && len(c.Labels) == 0 && c.Timestamp == nil {
		return nil, errors.New(ErrEmptyRegexStageConfig)
	}

	if c.Expression == "" {
		return nil, errors.New(ErrExpressionRequired)
	}

	expr, err := regexp.Compile(c.Expression)
	if err != nil {
		return nil, errors.Wrap(err, ErrCouldNotCompileRegex)
	}

	if c.Output != nil && (c.Output.Source == nil || (c.Output.Source != nil && *c.Output.Source == "")) {
		return nil, errors.New(ErrOutputSourceRequired)
	}

	if c.Timestamp != nil {
		if c.Timestamp.Source == nil || *c.Timestamp.Source == "" {
			return nil, errors.New(ErrTimestampSourceRequired)
		}
		if c.Timestamp.Format == "" {
			return nil, errors.New(ErrTimestampFormatRequired)
		}
		foundName := false
		for _, n := range expr.SubexpNames() {
			if n == *c.Timestamp.Source {
				foundName = true
			}
		}
		if !foundName {
			return nil, errors.Errorf(ErrTimestampGroupRequired, *c.Timestamp.Source)
		}
		c.Timestamp.Format = convertDateLayout(c.Timestamp.Format)
	}

	for labelName, labelSrc := range c.Labels {
		if !model.LabelName(labelName).IsValid() {
			return nil, fmt.Errorf(ErrInvalidLabelName, labelName)
		}
		if labelSrc == nil || *labelSrc.Source == "" {
			lName := labelName
			c.Labels[labelName] = &RegexLabel{
				&lName,
			}
		}
	}

	return expr, nil
}

type regexStage struct {
	cfg        *RegexConfig
	expression *regexp.Regexp
	logger     log.Logger
}

func NewRegex(logger log.Logger, config interface{}) (Stage, error) {
	cfg, err := newRegexConfig(config)
	if err != nil {
		return nil, err
	}
	expression, err := cfg.validate()
	if err != nil {
		return nil, err
	}
	return &regexStage{
		cfg:        cfg,
		expression: expression,
		logger:     log.With(logger, "component", "parser", "type", "regex"),
	}, nil
}

func (r *regexStage) Process(labels model.LabelSet, t *time.Time, entry *string) {
	if entry == nil {
		level.Debug(r.logger).Log("msg", "cannot parse a nil entry")
		return
	}

	match := r.expression.FindStringSubmatch(*entry)
	if match == nil {
		level.Debug(r.logger).Log("msg", "regex failed to match")
		return
	}
	groups := make(map[string]string)
	for i, name := range r.expression.SubexpNames() {
		if i != 0 && name != "" {
			groups[name] = match[i]
		}
	}

	// Parsing timestamp.
	if r.cfg.Timestamp != nil {
		if ts, ok := groups[*r.cfg.Timestamp.Source]; ok {
			parsedTs, err := time.Parse(r.cfg.Timestamp.Format, ts)
			if err != nil {
				level.Debug(r.logger).Log("msg", "failed to parse time", "err", err, "format", r.cfg.Timestamp.Format, "value", ts)
			} else {
				*t = parsedTs
			}
		} else {
			level.Debug(r.logger).Log("msg", "regex didn't match timestamp source")
		}
	}

	// Parsing labels.
	for lName, lSrc := range r.cfg.Labels {
		lValue, ok := groups[*lSrc.Source]
		if !ok {
			continue
		}
		labelValue := model.LabelValue(lValue)
		if !labelValue.IsValid() {
			level.Debug(r.logger).Log("msg", "invalid label value parsed", "value", labelValue)
			continue
		}
		labels[model.LabelName(lName)] = labelValue
	}

	// Parsing output.
	if r.cfg.Output != nil {
		if o, ok := groups[*r.cfg.Output.Source]; ok {
			*entry = o
		} else {
			level.Debug(r.logger).Log("msg", "regex didn't match output source")
		}
	}

}
