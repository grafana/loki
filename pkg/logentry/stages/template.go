package stages

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"text/template"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/mitchellh/mapstructure"
	"github.com/prometheus/common/model"
)

// Config Errors
const (
	ErrEmptyTemplateStageConfig = "template stage config cannot be empty"
	ErrTemplateSourceRequired   = "template source value is required"
)

var (
	functionMap = template.FuncMap{
		"ToLower":    strings.ToLower,
		"ToUpper":    strings.ToUpper,
		"Replace":    strings.Replace,
		"Trim":       strings.Trim,
		"TrimLeft":   strings.TrimLeft,
		"TrimRight":  strings.TrimRight,
		"TrimPrefix": strings.TrimPrefix,
		"TrimSuffix": strings.TrimSuffix,
		"TrimSpace":  strings.TrimSpace,
	}
)

// TemplateConfig configures template value extraction
type TemplateConfig struct {
	Source   string `mapstructure:"source"`
	Template string `mapstructure:"template"`
}

// validateTemplateConfig validates the templateStage config
func validateTemplateConfig(cfg *TemplateConfig) (*template.Template, error) {
	if cfg == nil {
		return nil, errors.New(ErrEmptyTemplateStageConfig)
	}
	if cfg.Source == "" {
		return nil, errors.New(ErrTemplateSourceRequired)
	}

	return template.New("pipeline_template").Funcs(functionMap).Parse(cfg.Template)
}

// newTemplateStage creates a new templateStage
func newTemplateStage(logger log.Logger, config interface{}) (*templateStage, error) {
	cfg := &TemplateConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	t, err := validateTemplateConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &templateStage{
		cfgs:     cfg,
		logger:   logger,
		template: t,
	}, nil
}

type templateData struct {
	Value string
}

// templateStage will mutate the incoming entry and set it from extracted data
type templateStage struct {
	cfgs     *TemplateConfig
	logger   log.Logger
	template *template.Template
}

// Process implements Stage
func (o *templateStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	if o.cfgs == nil {
		return
	}
	if v, ok := extracted[o.cfgs.Source]; ok {
		s, err := getString(v)
		if err != nil {
			if Debug {
				level.Debug(o.logger).Log("msg", "extracted template could not be converted to a string", "err", err, "type", reflect.TypeOf(v).String())
			}
			return
		}
		td := templateData{s}
		buf := &bytes.Buffer{}
		err = o.template.Execute(buf, td)
		if err != nil {
			if Debug {
				level.Debug(o.logger).Log("msg", "failed to execute template on extracted value", "err", err, "value", v)
			}
			return
		}
		st := buf.String()
		// If the template evaluates to an empty string, remove the key from the map
		if st == "" {
			delete(extracted, o.cfgs.Source)
		} else {
			extracted[o.cfgs.Source] = st
		}

	} else {
		td := templateData{}
		buf := &bytes.Buffer{}
		err := o.template.Execute(buf, td)
		if err != nil {
			if Debug {
				level.Debug(o.logger).Log("msg", "failed to execute template on extracted value", "err", err, "value", v)
			}
			return
		}
		st := buf.String()
		// Do not set extracted data with empty values
		if st != "" {
			extracted[o.cfgs.Source] = st
		}
	}
}

// Name implements Stage
func (o *templateStage) Name() string {
	return StageTypeTemplate
}
