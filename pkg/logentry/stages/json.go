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
	"github.com/prometheus/client_golang/prometheus"
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
	Metrics   MetricsConfig         `mapstructure:"metrics"`
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

type jsonStage struct {
	cfg         *JSONConfig
	expressions map[string]*jmespath.JMESPath
	logger      log.Logger
}

// NewJSON creates a new json stage from a config.
func NewJSON(logger log.Logger, config interface{}, registerer prometheus.Registerer) (Stage, error) {
	cfg, err := newJSONConfig(config)
	if err != nil {
		return nil, err
	}
	expressions, err := cfg.validate()
	if err != nil {
		return nil, err
	}
	return withMetric(&jsonStage{
		cfg:         cfg,
		expressions: expressions,
		logger:      log.With(logger, "component", "parser", "type", "json"),
	}, cfg.Metrics, registerer), nil
}

// Process implement a pipeline stage
func (j *jsonStage) Process(labels model.LabelSet, t *time.Time, entry *string) Valuer {
	if entry == nil {
		level.Debug(j.logger).Log("msg", "cannot parse a nil entry")
		return nil
	}
	valuer, err := newJSONValuer(*entry, j.expressions)
	if err != nil {
		level.Debug(j.logger).Log("msg", "failed to create json valuer", "err", err)
		return nil
	}

	// parsing ts
	if j.cfg.Timestamp != nil {
		ts, err := parseTimestamp(valuer, j.cfg.Timestamp)
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
		lValue, err := valuer.getJSONString(src, lName)
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
		output, err := parseOutput(valuer, j.cfg.Output)
		if err != nil {
			level.Debug(j.logger).Log("msg", "failed to parse output", "err", err)
		} else {
			*entry = output
		}
	}
	return valuer
}

type jsonValuer struct {
	exprmap map[string]*jmespath.JMESPath
	data    map[string]interface{}
}

func newJSONValuer(entry string, exprmap map[string]*jmespath.JMESPath) (*jsonValuer, error) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(entry), &data); err != nil {
		return nil, err
	}
	return &jsonValuer{
		data:    data,
		exprmap: exprmap,
	}, nil
}

func (v *jsonValuer) Value(expr *string) (interface{}, error) {
	return v.exprmap[*expr].Search(v.data)
}

func (v *jsonValuer) getJSONString(expr *string, fallback string) (result string, err error) {
	var ok bool
	if expr == nil {
		result, ok = v.data[fallback].(string)
		if !ok {
			return result, fmt.Errorf("%s is not a string but %T", fallback, v.data[fallback])
		}
		return
	}
	var searchResult interface{}
	if searchResult, err = v.Value(expr); err != nil {
		return
	}
	if result, ok = searchResult.(string); !ok {
		return result, fmt.Errorf("%s is not a string but %T", *expr, searchResult)
	}

	return
}

func parseOutput(v Valuer, cfg *JSONOutput) (string, error) {
	jsonObj, err := v.Value(cfg.Source)
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

func parseTimestamp(v *jsonValuer, cfg *JSONTimestamp) (time.Time, error) {
	ts, err := v.getJSONString(cfg.Source, "timestamp")
	if err != nil {
		return time.Time{}, err
	}
	parsedTs, err := time.Parse(cfg.Format, ts)
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "failed to parse time format:%s value:%s", cfg.Format, ts)
	}
	return parsedTs, nil
}
