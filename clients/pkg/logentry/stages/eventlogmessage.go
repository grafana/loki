package stages

import (
	"fmt"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/mitchellh/mapstructure"
	"github.com/prometheus/common/model"
)

const (
	defaultSource                    = "message"
	ErrInvalidMessageSourceLabelName = "invalid label name: %s"
)

type EventLogMessageConfig struct {
	Source *string `mapstructure:"source"`
}

type eventLogMessageStage struct {
	cfg    *EventLogMessageConfig
	logger log.Logger
}

func validateEventLogMessageStage(c *EventLogMessageConfig) error {
	if c == nil {
		// An empty config is allowed, use defaults (denoted by a nil Source
		c = &EventLogMessageConfig{Source: nil}
		return nil
	}
	if c.Source == nil {
		// A nil Source is also allowed, will use default value
		return nil
	}
	if !model.LabelName(*c.Source).IsValid() {
		return fmt.Errorf(ErrInvalidMessageSourceLabelName, *c.Source)
	}
	return nil
}

func newEventLogMessageStage(logger log.Logger, config interface{}) (Stage, error) {
	cfg, err := parseEventLogMessageStage(config)
	if err != nil {
		return nil, err
	}
	// validate config (i.e., check that source is a valid log label)
	validateEventLogMessageStage(cfg)
	if err != nil {
		return nil, err
	}
	return &eventLogMessageStage{
		cfg:    cfg,
		logger: log.With(logger, "component", "stage", "type", "event_log_message"),
	}, nil
}

func parseEventLogMessageStage(config interface{}) (*EventLogMessageConfig, error) {
	cfg := &EventLogMessageConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func (m *eventLogMessageStage) Run(in chan Entry) chan Entry {
	out := make(chan Entry)
	key := defaultSource
	if m.cfg.Source != nil {
		key = *m.cfg.Source
	}
	go func() {
		defer close(out)
		for e := range in {
			err := m.processEntry(e.Extracted, key)
			if err != nil {
				continue
			}
			out <- e
		}
	}()
	return out
}

func (m *eventLogMessageStage) processEntry(extracted map[string]interface{}, key string) error {
	value, ok := extracted[key]
	if !ok {
		if Debug {
			level.Debug(m.logger).Log("msg", "source does not exist in the set of extracted values", "source", key)
		}
		return nil
	}
	s, err := getString(value)
	if err != nil {
		if Debug {
			level.Debug(m.logger).Log("msg", "invalid label value parsed", "value", value)
		}
		return err
	}
	lines := strings.Split(s, "\r\n")
	for _, line := range lines {
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) < 2 {
			if Debug {
				level.Debug(m.logger).Log("msg", "invalid line parsed from message", "line", line)
			}
			continue
		}
		mkey := parts[0]
		if !model.LabelName(mkey).IsValid() {
			if Debug {
				level.Debug(m.logger).Log("msg", "invalid key parsed from message", "key", mkey)
			}
			continue
		}
		if _, ok := extracted[mkey]; ok {
			if Debug {
				level.Debug(m.logger).Log("msg", "message contained key that already existed, ignoring", "key", mkey)
			}
			continue
		}
		mval := parts[1]
		if !model.LabelValue(mval).IsValid() {
			if Debug {
				level.Debug(m.logger).Log("msg", "invalid value parsed from message", "value", mval)
			}
			continue
		}
		extracted[mkey] = mval
	}
	return nil
}

func (m *eventLogMessageStage) Name() string {
	return StageTypeEventLogMessage
}
