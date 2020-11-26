package stages

import (
	"bytes"
	"fmt"
	"regexp"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
)

const (
	ErrMultilineStageEmptyConfig  = "multiline stage config must define `firstline` regular expression"
	ErrMultilineStageInvalidRegex = "multiline stage first line regex compilation error: %v"
	ErrMultilineStageInvalidMaxWaitTime = "multiline stage `max_wait_time` parse error: %v"
)

// MultilineConfig contains the configuration for a multilineStage
type MultilineConfig struct {
	Expression  *string `mapstructure:"firstline"`
	MaxWaitTime *string  `mapstructure:"max_wait_time"`
	maxWait     time.Duration
	regex       *regexp.Regexp
}

func validateMultilineConfig(cfg *MultilineConfig) error {
	if cfg == nil || cfg.Expression == nil || cfg.MaxWaitTime == nil {
		return errors.New(ErrMultilineStageEmptyConfig)
	}

	expr, err := regexp.Compile(*cfg.Expression)
	if err != nil {
		return errors.Errorf(ErrMultilineStageInvalidRegex, err)
	}
	cfg.regex = expr

	maxWait, err := time.ParseDuration(*cfg.MaxWaitTime)
	if err != nil {
		return errors.Errorf(ErrMultilineStageInvalidMaxWaitTime, err)
	}
	cfg.maxWait = maxWait

	return nil
}

// dropMultiline matches lines to determine whether the following lines belong to a block and should be collapsed
type multilineStage struct {
	logger log.Logger
	cfg    *MultilineConfig
	buffer *bytes.Buffer
	startLineEntry Entry
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

func (m *multilineStage) Run(in chan Entry) chan Entry {
	out := make(chan Entry)
	go func() {
		defer close(out)
		for {
			select {
			case <- time.After(m.cfg.maxWait):
				level.Debug(m.logger).Log("msg", fmt.Sprintf("flush multiline block due to %v timeout", m.cfg.maxWait), "block", m.buffer.String())
				m.flush(out)
			case e, ok := <- in:
				if !ok {
					level.Debug(m.logger).Log("msg", "flush multiline block because inbound closed", "block", m.buffer.String())
					m.flush(out)
					return	
				}

				isFirstLine := m.cfg.regex.MatchString(e.Line)
				if isFirstLine {
					m.flush(out)
					// TODO: we only consider the labels and timestamp from the firt entry. Should merge all entries?
					m.startLineEntry = e
				}

				// Append block line
				if m.buffer.Len() > 0 {
					m.buffer.WriteRune('\n')
				}
				m.buffer.WriteString(e.Line)
			}
		}
	}()
	return out
}

func (m *multilineStage) flush(out chan Entry) {
	if m.buffer.Len() == 0 {
		level.Debug(m.logger).Log("msg", "nothing to flush", "buffer_len", m.buffer.Len())
		return
	}

	collapsed := &Entry{
		Extracted: m.startLineEntry.Extracted,
		Entry: api.Entry{
			Labels: m.startLineEntry.Entry.Labels,
			Entry: logproto.Entry{
				Timestamp: m.startLineEntry.Entry.Entry.Timestamp,
				Line: m.buffer.String(),
			},
		},
	}
	m.buffer.Reset()

	out <- *collapsed
}

// Name implements Stage
func (m *multilineStage) Name() string {
	return StageTypeMultiline
}