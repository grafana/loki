package stages

import (
	"fmt"
	"reflect"
	"regexp"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/util/flagext"
)

const (
	ErrDropStageEmptyConfig     = "drop stage config must contain at least one of `source`, `expression`, `older_than` or `longer_than`"
	ErrDropStageInvalidDuration = "drop stage invalid duration, %v cannot be converted to a duration: %v"
	ErrDropStageInvalidConfig   = "drop stage config error, `value` and `expression` cannot both be defined at the same time."
	ErrDropStageInvalidRegex    = "drop stage regex compilation error: %v"
	ErrDropStageInvalidByteSize = "drop stage failed to parse longer_than to bytes: %v"
)

var (
	defaultDropReason = "drop_stage"
)

// DropConfig contains the configuration for a dropStage
type DropConfig struct {
	DropReason *string `mapstructure:"drop_counter_reason"`
	Source     *string `mapstructure:"source"`
	Value      *string `mapstructure:"value"`
	Expression *string `mapstructure:"expression"`
	regex      *regexp.Regexp
	OlderThan  *string `mapstructure:"older_than"`
	olderThan  time.Duration
	LongerThan *string `mapstructure:"longer_than"`
	longerThan flagext.ByteSize
}

// validateDropConfig validates the DropConfig for the dropStage
func validateDropConfig(cfg *DropConfig) error {
	if cfg == nil ||
		(cfg.Source == nil && cfg.Expression == nil && cfg.OlderThan == nil && cfg.LongerThan == nil) {
		return errors.New(ErrDropStageEmptyConfig)
	}
	if cfg.DropReason == nil || *cfg.DropReason == "" {
		cfg.DropReason = &defaultDropReason
	}
	if cfg.OlderThan != nil {
		dur, err := time.ParseDuration(*cfg.OlderThan)
		if err != nil {
			return errors.Errorf(ErrDropStageInvalidDuration, *cfg.OlderThan, err)
		}
		cfg.olderThan = dur
	}
	if cfg.Value != nil && cfg.Expression != nil {
		return errors.New(ErrDropStageInvalidConfig)
	}
	if cfg.Expression != nil {
		expr, err := regexp.Compile(*cfg.Expression)
		if err != nil {
			return errors.Errorf(ErrDropStageInvalidRegex, err)
		}
		cfg.regex = expr
	}
	if cfg.LongerThan != nil {
		err := cfg.longerThan.Set(*cfg.LongerThan)
		if err != nil {
			return errors.Errorf(ErrDropStageInvalidByteSize, err)
		}
	}
	return nil
}

// newDropStage creates a DropStage from config
func newDropStage(logger log.Logger, config interface{}, registerer prometheus.Registerer) (Stage, error) {
	cfg := &DropConfig{}
	err := mapstructure.WeakDecode(config, cfg)
	if err != nil {
		return nil, err
	}
	err = validateDropConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &dropStage{
		logger:    log.With(logger, "component", "stage", "type", "drop"),
		cfg:       cfg,
		dropCount: getDropCountMetric(registerer),
	}, nil
}

// dropStage applies Label matchers to determine if the include stages should be run
type dropStage struct {
	logger    log.Logger
	cfg       *DropConfig
	dropCount *prometheus.CounterVec
}

func (m *dropStage) Run(in chan Entry) chan Entry {
	out := make(chan Entry)
	go func() {
		defer close(out)
		for e := range in {
			if !m.shouldDrop(e) {
				out <- e
				continue
			}
			m.dropCount.WithLabelValues(*m.cfg.DropReason)
		}
	}()
	return out
}

func (m *dropStage) shouldDrop(e Entry) bool {
	// There are many options for dropping a log and if multiple are defined it's treated like an AND condition
	// where all drop conditions must be met to drop the log.
	// Therefore if at any point there is a condition which does not match we can return.
	// The order is what I roughly think would be fastest check to slowest check to try to quit early whenever possible

	if m.cfg.LongerThan != nil {
		if len(e.Line) > m.cfg.longerThan.Val() {
			// Too long, drop
			if Debug {
				level.Debug(m.logger).Log("msg", fmt.Sprintf("line met drop criteria for length %v > %v", len(e.Line), m.cfg.longerThan.Val()))
			}
		} else {
			if Debug {
				level.Debug(m.logger).Log("msg", fmt.Sprintf("line will not be dropped, it did not meet criteria for drop length %v is not greater than %v", len(e.Line), m.cfg.longerThan.Val()))
			}
			return false
		}
	}

	if m.cfg.OlderThan != nil {
		ct := time.Now()
		if e.Timestamp.Before(ct.Add(-m.cfg.olderThan)) {
			// Too old, drop
			if Debug {
				level.Debug(m.logger).Log("msg", fmt.Sprintf("line met drop criteria for age; current time=%v, drop before=%v, log timestamp=%v", ct, ct.Add(-m.cfg.olderThan), e.Timestamp))
			}
		} else {
			if Debug {
				level.Debug(m.logger).Log("msg", fmt.Sprintf("line will not be dropped, it did not meet drop criteria for age; current time=%v, drop before=%v, log timestamp=%v", ct, ct.Add(-m.cfg.olderThan), e.Timestamp))
			}
			return false
		}
	}

	if m.cfg.Source != nil && m.cfg.Expression == nil {
		if v, ok := e.Extracted[*m.cfg.Source]; ok {
			if m.cfg.Value == nil {
				// Found in map, no value set meaning drop if found in map
				if Debug {
					level.Debug(m.logger).Log("msg", "line met drop criteria for finding source key in extracted map")
				}
			} else {
				if *m.cfg.Value == v {
					// Found in map with value set for drop
					if Debug {
						level.Debug(m.logger).Log("msg", "line met drop criteria for finding source key in extracted map with value matching desired drop value")
					}
				} else {
					// Value doesn't match, don't drop
					if Debug {
						level.Debug(m.logger).Log("msg", fmt.Sprintf("line will not be dropped, source key was found in extracted map but value '%v' did not match desired value '%v'", v, *m.cfg.Value))
					}
					return false
				}
			}
		} else {
			// Not found in extact map, don't drop
			if Debug {
				level.Debug(m.logger).Log("msg", "line will not be dropped, the provided source was not found in the extracted map")
			}
			return false
		}
	}

	if m.cfg.Expression != nil {
		if m.cfg.Source != nil {
			if v, ok := e.Extracted[*m.cfg.Source]; ok {
				s, err := getString(v)
				if err != nil {
					if Debug {
						level.Debug(m.logger).Log("msg", "Failed to convert extracted map value to string, cannot test regex line will not be dropped.", "err", err, "type", reflect.TypeOf(v))
					}
					return false
				}
				match := m.cfg.regex.FindStringSubmatch(s)
				if match == nil {
					// Not a match to the regex, don't drop
					if Debug {
						level.Debug(m.logger).Log("msg", fmt.Sprintf("line will not be dropped, the provided regular expression did not match the value found in the extracted map for source key: %v", *m.cfg.Source))
					}
					return false
				}
				// regex match, will be dropped
				if Debug {
					level.Debug(m.logger).Log("msg", "line met drop criteria, regex matched the value in the extracted map source key")
				}

			} else {
				// Not found in extact map, don't drop
				if Debug {
					level.Debug(m.logger).Log("msg", "line will not be dropped, the provided source was not found in the extracted map")
				}
				return false
			}
		} else {
			match := m.cfg.regex.FindStringSubmatch(e.Line)
			if match == nil {
				// Not a match to the regex, don't drop
				if Debug {
					level.Debug(m.logger).Log("msg", "line will not be dropped, the provided regular expression did not match the log line")
				}
				return false
			}
			if Debug {
				level.Debug(m.logger).Log("msg", "line met drop criteria, the provided regular expression matched the log line")
			}
		}
	}

	// Everything matched, drop the line
	if Debug {
		level.Debug(m.logger).Log("msg", "all criteria met, line will be dropped")
	}
	return true
}

// Name implements Stage
func (m *dropStage) Name() string {
	return StageTypeDrop
}
