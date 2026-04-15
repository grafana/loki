package stages

import (
	"fmt"
	"reflect"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/jmespath/go-jmespath"
	json "github.com/json-iterator/go"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
)

// Config Errors
const (
	ErrExpressionsRequired  = "JMES expression is required"
	ErrCouldNotCompileJMES  = "could not compile JMES expression"
	ErrEmptyJSONStageConfig = "empty json stage configuration"
	ErrEmptyJSONStageSource = "empty source"
	ErrMalformedJSON        = "malformed json"
)

// JSONConfig represents a JSON Stage configuration
type JSONConfig struct {
	Expressions   map[string]string `mapstructure:"expressions"`
	Source        *string           `mapstructure:"source"`
	DropMalformed bool              `mapstructure:"drop_malformed"`
}

// validateJSONConfig validates a json config and returns a map of necessary jmespath expressions.
func validateJSONConfig(c *JSONConfig) (map[string]*jmespath.JMESPath, error) {
	if c == nil {
		return nil, errors.New(ErrEmptyJSONStageConfig)
	}

	if len(c.Expressions) == 0 {
		return nil, errors.New(ErrExpressionsRequired)
	}

	if c.Source != nil && *c.Source == "" {
		return nil, errors.New(ErrEmptyJSONStageSource)
	}

	expressions := map[string]*jmespath.JMESPath{}

	for n, e := range c.Expressions {
		var err error
		jmes := e
		// If there is no expression, use the name as the expression.
		if e == "" {
			jmes = n
		}
		expressions[n], err = jmespath.Compile(jmes)
		if err != nil {
			return nil, errors.Wrap(err, ErrCouldNotCompileJMES)
		}
	}
	return expressions, nil
}

// jsonStage sets extracted data using JMESPath expressions
type jsonStage struct {
	cfg         *JSONConfig
	expressions map[string]*jmespath.JMESPath
	logger      log.Logger
}

// newJSONStage creates a new json pipeline stage from a config.
func newJSONStage(logger log.Logger, config interface{}) (Stage, error) {
	cfg, err := parseJSONConfig(config)
	if err != nil {
		return nil, err
	}
	expressions, err := validateJSONConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &jsonStage{
		cfg:         cfg,
		expressions: expressions,
		logger:      log.With(logger, "component", "stage", "type", "json"),
	}, nil
}

func parseJSONConfig(config interface{}) (*JSONConfig, error) {
	cfg := &JSONConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func (j *jsonStage) Run(in chan Entry) chan Entry {
	out := make(chan Entry)
	go func() {
		defer close(out)
		for e := range in {
			err := j.processEntry(e.Extracted, &e.Line)
			if err != nil && j.cfg.DropMalformed {
				continue
			}
			out <- e
		}
	}()
	return out
}

func (j *jsonStage) processEntry(extracted map[string]interface{}, entry *string) error {
	// If a source key is provided, the json stage should process it
	// from the extracted map, otherwise should fallback to the entry
	input := entry

	if j.cfg.Source != nil {
		if _, ok := extracted[*j.cfg.Source]; !ok {
			if Debug {
				level.Debug(j.logger).Log("msg", "source does not exist in the set of extracted values", "source", *j.cfg.Source)
			}
			return nil
		}

		value, err := getString(extracted[*j.cfg.Source])
		if err != nil {
			if Debug {
				level.Debug(j.logger).Log("msg", "failed to convert source value to string", "source", *j.cfg.Source, "err", err, "type", reflect.TypeOf(extracted[*j.cfg.Source]))
			}
			return nil
		}

		input = &value
	}

	if input == nil {
		if Debug {
			level.Debug(j.logger).Log("msg", "cannot parse a nil entry")
		}
		return nil
	}

	var data map[string]interface{}

	if err := json.Unmarshal([]byte(*input), &data); err != nil {
		if Debug {
			level.Debug(j.logger).Log("msg", "failed to unmarshal log line", "err", err)
		}
		return errors.New(ErrMalformedJSON)
	}

	for n, e := range j.expressions {
		r, err := e.Search(data)
		if err != nil {
			if Debug {
				level.Debug(j.logger).Log("msg", "failed to search JMES expression", "err", err)
			}
			continue
		}

		switch r.(type) {
		case float64:
			// All numbers in JSON are unmarshaled to float64.
			extracted[n] = r
		case string:
			extracted[n] = r
		case bool:
			extracted[n] = r
		case nil:
			extracted[n] = nil
		default:
			// If the value wasn't a string or a number, marshal it back to json
			jm, err := json.Marshal(r)
			if err != nil {
				if Debug {
					level.Debug(j.logger).Log("msg", "failed to marshal complex type back to string", "err", err)
				}
				continue
			}
			extracted[n] = string(jm)
		}
	}
	if Debug {
		level.Debug(j.logger).Log("msg", "extracted data debug in json stage", "extracted data", fmt.Sprintf("%v", extracted))
	}
	return nil
}

// Name implements Stage
func (j *jsonStage) Name() string {
	return StageTypeJSON
}

// Cleanup implements Stage.
func (*jsonStage) Cleanup() {
	// no-op
}
