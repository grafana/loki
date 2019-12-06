package stages

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

const (
	ErrEmptyMultilineStageConfig = "multiline stage config cannot be empty"
	ErrMultilineNameRequired     = "multiline stage pipeline name can be omitted but cannot be an empty string"
	ErrFirstLineRequired         = "firstLine regular expression required"
)

// MultilineConfig contains the configuration for a multilineStage
type MultilineConfig struct {
	PipelineName    *string `mapstructure:"pipeline_name"`
	FirstLineRegexp string  `mapstructure:"firstline"`
}

func validateMultilineConfig(cfg *MultilineConfig) (*regexp.Regexp, error) {
	if cfg == nil {
		return nil, errors.New(ErrEmptyMultilineStageConfig)
	}
	if cfg.PipelineName != nil && *cfg.PipelineName == "" {
		return nil, errors.New(ErrMultilineNameRequired)
	}
	if cfg.FirstLineRegexp == "" {
		return nil, errors.New(ErrFirstLineRequired)
	}

	re, err := regexp.Compile(cfg.FirstLineRegexp)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse firstline regex")
	}

	return re, nil
}

// newMatcherStage creates a new matcherStage from config
func newMultilineStage(logger log.Logger, config interface{}) (*multilineStage, error) {
	cfg := &MultilineConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	re, err := validateMultilineConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &multilineStage{
		firstLine:  re,
		multilines: map[string]*multilineEntry{},
	}, nil
}

type multilineStage struct {
	firstLine *regexp.Regexp

	mapMu      sync.Mutex
	multilines map[string]*multilineEntry
}

type multilineEntry struct {
	lines     []string
	labels    model.LabelSet
	lastWrite time.Time
}

func (m *multilineStage) Name() string {
	return "multiline"
}

// Process implements Stage
func (m *multilineStage) Process(labels model.LabelSet, extracted map[string]interface{}, timestamp time.Time, entry string, chain StageChain) {
	isFirstLine := m.firstLine.MatchString(entry)
	key := labels.String()

	m.mapMu.Lock()

	ent := m.multilines[key]

	if isFirstLine {
		// flush previous entry, if there is any, and store new one
		if ent != nil && len(ent.lines) > 0 {
			delete(m.multilines, key)
			m.mapMu.Unlock()

			// extracted + timestamp are just passed forward
			chain.NextStage(labels, extracted, timestamp, strings.Join(ent.lines, "\n"))

			m.mapMu.Lock()
		}

		m.multilines[key] = &multilineEntry{
			lines:     []string{entry}, // start new multiline entry,
			labels:    labels.Clone(),
			lastWrite: time.Now(),
		}

		m.mapMu.Unlock()
	} else {
		if ent == nil || len(ent.lines) == 0 {
			m.mapMu.Unlock()

			// no started multiline? just pass it forward then.
			chain.NextStage(labels, extracted, timestamp, entry)
		} else {
			// add it to existing line, and wait for more lines.
			ent.lines = append(ent.lines, entry)
			ent.lastWrite = time.Now()
			m.mapMu.Unlock()
		}
	}
}
