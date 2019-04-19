package stages

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/jmespath/go-jmespath"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

// JSONTimestamp configures timestamp extraction
type JSONTimestamp struct {
	Source *string `mapstructure:"source"`
	Format string  `mapstructure:"format"`
}

// JSONLabel configures a labels value extraction
type JSONLabel struct {
	Source *string `mapstructure:"source"`
}

// JSONOutput configures output value extraction
type JSONOutput struct {
	Source *string `mapstructure:"source"`
}

// JSONConfig configures the log entry parser to extract value from json
type JSONConfig struct {
	Timestamp *JSONTimestamp        `mapstructure:"timestamp"`
	Output    *JSONOutput           `mapstructure:"output"`
	Labels    map[string]*JSONLabel `mapstructure:"labels"`
}

func newJSONConfig(config interface{}) (*JSONConfig, error) {
	cfg := &JSONConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate the config and returns a map of necessary jmespath expressions.
func (c *JSONConfig) validate() (map[string]*jmespath.JMESPath, error) {
	if c.Output == nil && len(c.Labels) == 0 && c.Timestamp == nil {
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
	return expressions, nil
}

type jsonStage struct {
	cfg         *JSONConfig
	expressions map[string]*jmespath.JMESPath
	logger      log.Logger
}

// NewJSON creates a new json stage from a config.
func NewJSON(logger log.Logger, config interface{}) (Stage, error) {
	cfg, err := newJSONConfig(config)
	if err != nil {
		return nil, err
	}
	expressions, err := cfg.validate()
	if err != nil {
		return nil, err
	}
	return &jsonStage{
		cfg:         cfg,
		expressions: expressions,
		logger:      log.With(logger, "component", "parser", "type", "json"),
	}, nil
}

func (j *jsonStage) getJSONString(expr *string, fallback string, data map[string]interface{}) (result string, ok bool) {
	if expr == nil {
		result, ok = data[fallback].(string)
		if !ok {
			level.Debug(j.logger).Log("msg", "field is not a string", "field", fallback)
		}
	} else {
		var searchResult interface{}
		searchResult, ok = j.getJSONValue(expr, data)
		if !ok {
			level.Debug(j.logger).Log("msg", "failed to search with jmespath expression", "expr", expr)
			return
		}
		result, ok = searchResult.(string)
		if !ok {
			level.Debug(j.logger).Log("msg", "search result is not a string", "expr", *expr)
		}
	}
	return
}

func (j *jsonStage) getJSONValue(expr *string, data map[string]interface{}) (result interface{}, ok bool) {
	var err error
	ok = true
	result, err = j.expressions[*expr].Search(data)
	if err != nil {
		level.Debug(j.logger).Log("msg", "failed to search with jmespath expression", "expr", expr)
		ok = false
		return
	}
	return
}

// Process implement a pipeline stage
func (j *jsonStage) Process(labels model.LabelSet, t *time.Time, entry *string) {
	if entry == nil {
		level.Debug(j.logger).Log("msg", "cannot parse a nil entry")
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(*entry), &data); err != nil {
		level.Debug(j.logger).Log("msg", "could not unmarshal json", "err", err)
		return
	}

	// parsing ts
	if j.cfg.Timestamp != nil {
		if ts, ok := j.getJSONString(j.cfg.Timestamp.Source, "timestamp", data); ok {
			parsedTs, err := time.Parse(j.cfg.Timestamp.Format, ts)
			if err != nil {
				level.Debug(j.logger).Log("msg", "failed to parse time", "err", err, "format", j.cfg.Timestamp.Format, "value", ts)
			} else {
				*t = parsedTs
			}
		}
	}

	// parsing labels
	for lName, lSrc := range j.cfg.Labels {
		var src *string
		if lSrc != nil {
			src = lSrc.Source
		}
		lValue, ok := j.getJSONString(src, lName, data)
		if !ok {
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
		if jsonObj, ok := j.getJSONValue(j.cfg.Output.Source, data); ok && jsonObj != nil {
			if s, ok := jsonObj.(string); ok {
				*entry = s
				return
			}
			b, err := json.Marshal(jsonObj)
			if err != nil {
				level.Debug(j.logger).Log("msg", "could not marshal output value", "err", err)
				return
			}
			*entry = string(b)
		}
	}
}

// convertDateLayout converts pre-defined date format layout into date format
func convertDateLayout(predef string) string {
	switch predef {
	case "ANSIC":
		return time.ANSIC
	case "UnixDate":
		return time.UnixDate
	case "RubyDate":
		return time.RubyDate
	case "RFC822":
		return time.RFC822
	case "RFC822Z":
		return time.RFC822Z
	case "RFC850":
		return time.RFC850
	case "RFC1123":
		return time.RFC1123
	case "RFC1123Z":
		return time.RFC1123Z
	case "RFC3339":
		return time.RFC3339
	case "RFC3339Nano":
		return time.RFC3339Nano
	default:
		return predef
	}
}
