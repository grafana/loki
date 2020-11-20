package stages

import (
	"bytes"
	"regexp"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/go-kit/kit/log"
	"github.com/prometheus/common/model"
)

const (
	ErrMultilineStageEmptyConfig  = "multiline stage config must define `firstline` regular expression"
	ErrMultilineStageInvalidRegex = "multiline stage first line regex compilation error: %v"
)

const MultilineDropReason = "multiline collapse"

// MultilineConfig contains the configuration for a multilineStage 
type MultilineConfig struct {
	Expression *string `mapstructure:"firstline"`
	regex      *regexp.Regexp
}

func validateMultilineConfig(cfg *MultilineConfig) error {
	if cfg == nil ||
		(cfg.Expression == nil) {
		return errors.New(ErrMultilineStageEmptyConfig)
	}
	expr, err := regexp.Compile(*cfg.Expression)
	if err != nil {
		return errors.Errorf(ErrMultilineStageInvalidRegex, err)
	}
		cfg.regex = expr

	return nil
}

// newMulitlineStage creates a MulitlineStage from config
func newMultilineStage(logger log.Logger, config interface{}) (Stage, error) {
	cfg := &MultilineConfig{}
	err := mapstructure.WeakDecode(config, cfg)
	if err != nil {
		return nil, err
	}
	err = validateMultilineConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &multilineStage{
		logger: log.With(logger, "component", "stage", "type", "multiline"),
		cfg:    cfg,
		buffer: new(bytes.Buffer),
	}, nil
}

// dropMultiline matches lines to determine whether the following lines belong to a block and should be collapsed
type multilineStage struct {
	logger log.Logger
	cfg    *MultilineConfig
	buffer *bytes.Buffer
}

// Process implements Stage
func (m *multilineStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	isFirstLine := m.cfg.regex.MatchString(*entry)

	if isFirstLine {
		previous := m.buffer.String()

		m.buffer.Reset()
		m.buffer.WriteString(*entry)

		*entry = previous
	} else {
		// Append block line
		if m.buffer.Len() > 0 {
			m.buffer.WriteRune('\n')
		}
		m.buffer.WriteString(*entry)

		// Adds the drop label to not be sent by the api.EntryHandler
		labels[dropLabel] = model.LabelValue(MultilineDropReason)
	}
}

// Name implements Stage
func (m *multilineStage) Name() string {
	return StageTypeMultiline
}