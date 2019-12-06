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

// MatcherConfig contains the configuration for a matcherStage
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
		multilines: map[string][]string{},
	}, nil
}

type multilineStage struct {
	firstLine *regexp.Regexp

	mapMu      sync.Mutex
	multilines map[string][]string
}

func (m *multilineStage) Name() string {
	return "multiline"
}

// Process implements Stage
func (m *multilineStage) Process(labels model.LabelSet, extracted map[string]interface{}, time time.Time, entry string, chain StageChain) {
	isFirstLine := m.firstLine.MatchString(entry)
	key := labels.String()

	m.mapMu.Lock()
	previous := m.multilines[key]

	if isFirstLine {
		// flush previous entry, if there is any, and store new one
		if previous != nil {
			delete(m.multilines, key)
			m.mapMu.Unlock()

			chain.NextStage(labels, extracted, time, strings.Join(previous, "\n"))

			m.mapMu.Lock()
		}

		m.multilines[key] = []string{entry} // start new multiline entry
	} else {
		// buffer current entry, add it to previous (if any), and wait for more lines.
		m.multilines[key] = append(previous, entry)
	}
	m.mapMu.Unlock()
}
