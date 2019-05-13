package stages

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/jmespath/go-jmespath"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

// validateJSONConfig validates a json config and returns a map of necessary jmespath expressions.
func validateJSONConfig(c *StageConfig) (map[string]*jmespath.JMESPath, error) {
	if c.Output == nil && len(c.Labels) == 0 && c.Timestamp == nil && len(c.Metrics) == 0 {
		return nil, errors.New("empty json parser configuration")
	}

	if c.Output != nil && (c.Output.Source == nil || (c.Output.Source != nil && *c.Output.Source == "")) {
		return nil, errors.New("output source value is required")
	}

	if c.Timestamp != nil {
		if c.Timestamp.Source != nil && *c.Timestamp.Source == "" {
			return nil, errors.New("timestamp source value is required")
		}
		if c.Timestamp.Format == "" {
			return nil, errors.New("timestamp format is required")
		}
		c.Timestamp.Format = convertDateLayout(c.Timestamp.Format)
	}

	expressions := map[string]*jmespath.JMESPath{}
	var err error
	if c.Timestamp != nil && c.Timestamp.Source != nil {
		expressions[*c.Timestamp.Source], err = jmespath.Compile(*c.Timestamp.Source)
		if err != nil {
			return nil, errors.Wrap(err, "could not compile timestamp source jmespath expression")
		}
	}
	if c.Output != nil && c.Output.Source != nil {
		expressions[*c.Output.Source], err = jmespath.Compile(*c.Output.Source)
		if err != nil {
			return nil, errors.Wrap(err, "could not compile output source jmespath expression")
		}
	}
	for labelName, labelSrc := range c.Labels {
		if !model.LabelName(labelName).IsValid() {
			return nil, fmt.Errorf("invalid label name: %s", labelName)
		}
		if labelSrc != nil && labelSrc.Source != nil {
			expressions[*labelSrc.Source], err = jmespath.Compile(*labelSrc.Source)
			if err != nil {
				return nil, errors.Wrapf(err, "could not compile label source jmespath expression: %s", labelName)
			}
		}
	}

	// metrics expressions.
	for _, mcfg := range c.Metrics {
		if mcfg.Source != nil {
			expressions[*mcfg.Source], err = jmespath.Compile(*mcfg.Source)
			if err != nil {
				return nil, errors.Wrap(err, "could not compile output source jmespath expression")
			}
		}
	}
	return expressions, nil
}

// jsonMutator mutates log entries via json parsing.
type jsonMutator struct {
	cfg         *StageConfig
	expressions map[string]*jmespath.JMESPath
	logger      log.Logger
}

// newJSONMutator creates a new json mutator from a config.
func newJSONMutator(logger log.Logger, cfg *StageConfig) (*jsonMutator, error) {
	expressions, err := validateJSONConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &jsonMutator{
		cfg:         cfg,
		expressions: expressions,
		logger:      log.With(logger, "component", "mutator", "type", "json"),
	}, nil
}

// Process implements Mutator
func (j *jsonMutator) Process(labels model.LabelSet, t *time.Time, entry *string) Extractor {
	if entry == nil {
		level.Debug(j.logger).Log("msg", "cannot parse a nil entry")
		return nil
	}
	extractor, err := newJSONExtractor(*entry, j.expressions)
	if err != nil {
		level.Debug(j.logger).Log("msg", "failed to create json extractor", "err", err)
		return nil
	}

	// parsing ts
	if j.cfg.Timestamp != nil {
		ts, err := parseTimestamp(extractor, j.cfg.Timestamp)
		if err != nil {
			level.Debug(j.logger).Log("msg", "failed to parse timestamp", "err", err)
		} else {
			*t = ts
		}
	}

	// parsing labels
	for lName, lSrc := range j.cfg.Labels {
		var src *string
		if lSrc != nil {
			src = lSrc.Source
		}
		lValue, err := extractor.getJSONString(src, lName)
		if err != nil {
			level.Debug(j.logger).Log("msg", "failed to get json string", "err", err)
			continue
		}
		labelValue := model.LabelValue(lValue)
		// seems impossible as the json.Unmarshal would yield an error first.
		if !labelValue.IsValid() {
			level.Debug(j.logger).Log("msg", "invalid label value parsed", "value", labelValue)
			continue
		}
		labels[model.LabelName(lName)] = labelValue
	}

	// parsing output
	if j.cfg.Output != nil {
		output, err := parseOutput(extractor, j.cfg.Output)
		if err != nil {
			level.Debug(j.logger).Log("msg", "failed to parse output", "err", err)
		} else {
			*entry = output
		}
	}
	return extractor
}

// jsonExtractor extracts value from log line via http://jmespath.org/ .
type jsonExtractor struct {
	exprmap map[string]*jmespath.JMESPath
	data    map[string]interface{}
}

// newJSONExtractor creates a new Extractor.
func newJSONExtractor(entry string, exprmap map[string]*jmespath.JMESPath) (*jsonExtractor, error) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(entry), &data); err != nil {
		return nil, err
	}
	return &jsonExtractor{
		data:    data,
		exprmap: exprmap,
	}, nil
}

// Value implements Extractor
func (e *jsonExtractor) Value(expr *string) (interface{}, error) {
	return e.exprmap[*expr].Search(e.data)
}

func (e *jsonExtractor) getJSONString(expr *string, fallback string) (result string, err error) {
	var ok bool
	if expr == nil {
		result, ok = e.data[fallback].(string)
		if !ok {
			return result, fmt.Errorf("%s is not a string but %T", fallback, e.data[fallback])
		}
		return
	}
	var searchResult interface{}
	if searchResult, err = e.Value(expr); err != nil {
		return
	}
	if result, ok = searchResult.(string); !ok {
		return result, fmt.Errorf("%s is not a string but %T", *expr, searchResult)
	}

	return
}

func parseOutput(e Extractor, cfg *OutputConfig) (string, error) {
	jsonObj, err := e.Value(cfg.Source)
	if err != nil {
		return "", errors.Wrap(err, "failed to fetch json value")
	}
	if jsonObj == nil {
		return "", errors.New("json value is nil")
	}
	if s, ok := jsonObj.(string); ok {
		return s, nil
	}
	b, err := json.Marshal(jsonObj)
	if err != nil {
		return "", errors.Wrap(err, "could not marshal output value")
	}
	return string(b), nil
}

func parseTimestamp(e *jsonExtractor, cfg *TimestampConfig) (time.Time, error) {
	ts, err := e.getJSONString(cfg.Source, "timestamp")
	if err != nil {
		return time.Time{}, err
	}
	parsedTs, err := time.Parse(cfg.Format, ts)
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "failed to parse time format:%s value:%s", cfg.Format, ts)
	}
	return parsedTs, nil
}
