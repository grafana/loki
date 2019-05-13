package stages

import (
	"fmt"
	"regexp"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

// Config Errors
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

// validateRegexConfig validates the config and return a regex
func validateRegexConfig(c *StageConfig) (*regexp.Regexp, error) {

	if c.Output == nil && len(c.Labels) == 0 && c.Timestamp == nil && len(c.Metrics) == 0 {
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
			c.Labels[labelName] = &LabelConfig{
				&lName,
			}
		}
	}

	return expr, nil
}

// regexMutator mutates log entries using regex
type regexMutator struct {
	cfg        *StageConfig
	expression *regexp.Regexp
	logger     log.Logger
}

// newRegexMutator creates a new regular expression Mutator.
func newRegexMutator(logger log.Logger, cfg *StageConfig) (Mutator, error) {
	expression, err := validateRegexConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &regexMutator{
		cfg:        cfg,
		expression: expression,
		logger:     log.With(logger, "component", "mutator", "type", "regex"),
	}, nil
}

// Process implements Mutator
func (r *regexMutator) Process(labels model.LabelSet, t *time.Time, entry *string) Extractor {
	if entry == nil {
		level.Debug(r.logger).Log("msg", "cannot parse a nil entry")
		return nil
	}

	extractor, err := newRegexExtractor(*entry, r.expression)
	if err != nil {
		level.Debug(r.logger).Log("msg", "failed to create regex extractor", "err", err)
		return nil
	}

	// Parsing timestamp.
	if r.cfg.Timestamp != nil {
		if ts, ok := extractor.groups[*r.cfg.Timestamp.Source]; ok {
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
		lValue, ok := extractor.groups[*lSrc.Source]
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
		if o, ok := extractor.groups[*r.cfg.Output.Source]; ok {
			*entry = o
		} else {
			level.Debug(r.logger).Log("msg", "regex didn't match output source")
		}
	}

	return extractor
}

// regexExtractor extracts value from regular expressions.
type regexExtractor struct {
	groups map[string]string
}

// newRegexExtractor creates a new regexExtractor
func newRegexExtractor(entry string, expression *regexp.Regexp) (*regexExtractor, error) {
	match := expression.FindStringSubmatch(entry)
	if match == nil {
		return nil, errors.New("regex failed to match")
	}
	groups := make(map[string]string)
	for i, name := range expression.SubexpNames() {
		if i != 0 && name != "" {
			groups[name] = match[i]
		}
	}
	return &regexExtractor{groups: groups}, nil
}

// Value implements Extractor, if expr is nil it returns the count of matched values.
func (e *regexExtractor) Value(expr *string) (interface{}, error) {
	if expr == nil {
		return len(e.groups), nil
	}
	if value, ok := e.groups[*expr]; ok {
		return value, nil
	}
	return nil, fmt.Errorf("group %s not matched", *expr)
}
