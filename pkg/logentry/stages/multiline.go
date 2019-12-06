package stages

import (
	"fmt"
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
	MaxWait         string  `mapstructure:"max_wait_time"`
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

	maxWait := time.Duration(0)
	if cfg.MaxWait != "" {
		maxWait, err = time.ParseDuration(cfg.MaxWait)
		if err != nil {
			return nil, errors.Wrap(err, "multiline: invalid max_wait_time duration")
		}
	}

	return &multilineStage{
		firstLine:  re,
		maxWait:    maxWait,
		multilines: map[string]*multilineEntry{},
	}, nil
}

type multilineStage struct {
	firstLine *regexp.Regexp
	maxWait   time.Duration

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
	hasPrevious := ent != nil && len(ent.lines) > 0

	switch {
	case isFirstLine && hasPrevious:
		delete(m.multilines, key)

		// put new multiline entry to the map before unlocking
		m.multilines[key] = &multilineEntry{
			lines:     []string{entry},
			labels:    labels.Clone(),
			lastWrite: time.Now(),
		}

		m.mapMu.Unlock()

		// extracted + timestamp are just passed forward
		chain.NextStage(labels, extracted, timestamp, strings.Join(ent.lines, "\n"))

	case isFirstLine && !hasPrevious:
		m.multilines[key] = &multilineEntry{
			lines:     []string{entry}, // start new multiline entry,
			labels:    labels.Clone(),
			lastWrite: time.Now(),
		}

		m.mapMu.Unlock()

	case !isFirstLine && hasPrevious:
		// add it to existing line, and wait for more lines.
		ent.lines = append(ent.lines, entry)
		ent.lastWrite = time.Now()

		m.mapMu.Unlock()

	case !isFirstLine && !hasPrevious:
		m.mapMu.Unlock()

		// no started multiline? just pass it forward then.
		chain.NextStage(labels, extracted, timestamp, entry)
	}
}

func (m *multilineStage) Flush(chain RepeatableStageChain) {
	if m.maxWait <= 0 {
		return
	}

	now := time.Now()

	m.mapMu.Lock()
	for key, e := range m.multilines {
		if now.Sub(e.lastWrite) > m.maxWait {
			delete(m.multilines, key) // remove from map before unlocking
			m.mapMu.Unlock()

			entry := strings.Join(e.lines, "\n")

			fmt.Println("flushing", key)
			// only use chain clones, so that original chain can be reused
			chain.Clone().NextStage(e.labels, map[string]interface{}{}, e.lastWrite, entry)

			m.mapMu.Lock()
		}
	}
	m.mapMu.Unlock()
}
