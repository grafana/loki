package stages

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/util/flagext"
)

const (
	ErrDropStageEmptyConfig       = "drop stage config must contain at least one of `source`, `expression`, `older_than` or `longer_than`"
	ErrDropStageInvalidDuration   = "drop stage invalid duration, %v cannot be converted to a duration: %v"
	ErrDropStageInvalidConfig     = "drop stage config error, `value` and `expression` cannot both be defined at the same time."
	ErrDropStageInvalidRegex      = "drop stage regex compilation error: %v"
	ErrDropStageInvalidByteSize   = "drop stage failed to parse longer_than to bytes: %v"
	ErrDropStageInvalidSource     = "drop stage source invalid type should be string or list of strings"
	ErrDropStageNoSourceWithValue = "drop stage config must contain `source` if `value` is specified"
)

var (
	defaultDropReason = "drop_stage"
	defaultSeparator  = ";"
)

// DropConfig contains the configuration for a dropStage
type DropConfig struct {
	DropReason *string     `mapstructure:"drop_counter_reason"`
	Source     interface{} `mapstructure:"source"`
	source     *[]string
	Value      *string `mapstructure:"value"`
	Separator  *string `mapstructure:"separator"`
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
	if cfg.Source != nil {
		src, err := unifySourceField(cfg.Source)
		if err != nil {
			return err
		}
		cfg.source = &src
	}
	if cfg.DropReason == nil || *cfg.DropReason == "" {
		cfg.DropReason = &defaultDropReason
	}
	if cfg.Separator == nil {
		cfg.Separator = &defaultSeparator
	}
	if cfg.OlderThan != nil {
		dur, err := time.ParseDuration(*cfg.OlderThan)
		if err != nil {
			return errors.Errorf(ErrDropStageInvalidDuration, *cfg.OlderThan, err)
		}
		cfg.olderThan = dur
	}
	if cfg.Value != nil && cfg.Source == nil {
		return errors.New(ErrDropStageNoSourceWithValue)
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
	// The first step to exclude `value` and fully replace it with the `expression`.
	// It will simplify code and less confusing for the end-user on which option to choose.
	if cfg.Value != nil {
		expr, err := regexp.Compile(fmt.Sprintf("^%s$", regexp.QuoteMeta(*cfg.Value)))
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

// unifySourceField unify Source into a slice of strings
func unifySourceField(s interface{}) ([]string, error) {
	switch s := s.(type) {
	case []interface{}:
		r := make([]string, len(s))
		for i := range s {
			r[i] = s[i].(string)
		}
		return r, nil
	case []string:
		return s, nil
	case string:
		return []string{s}, nil
	}
	return nil, errors.New(ErrDropStageInvalidSource)
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
			m.dropCount.WithLabelValues(*m.cfg.DropReason).Inc()
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
	if m.cfg.Source != nil && m.cfg.regex == nil {
		var match bool
		match = true
		for _, src := range *m.cfg.source {
			if _, ok := e.Extracted[src]; !ok {
				match = false
			}
		}
		if match {
			if Debug {
				level.Debug(m.logger).Log("msg", "line met drop criteria for finding source key in extracted map")
			}
		} else {
			// Not found in extact map, don't drop
			if Debug {
				level.Debug(m.logger).Log("msg", "line will not be dropped, the provided source was not found in the extracted map")
			}
			return false
		}
	}

	if m.cfg.Source == nil && m.cfg.regex != nil {
		if !m.cfg.regex.MatchString(e.Line) {
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

	if m.cfg.Source != nil && m.cfg.regex != nil {
		var extractedData []string
		for _, src := range *m.cfg.source {
			if e, ok := e.Extracted[src]; ok {
				s, err := getString(e)
				if err != nil {
					if Debug {
						level.Debug(m.logger).Log("msg", "Failed to convert extracted map value to string, cannot test regex line will not be dropped.", "err", err, "type", reflect.TypeOf(e))
					}
					return false
				}
				extractedData = append(extractedData, s)
			}
		}
		if !m.cfg.regex.MatchString(strings.Join(extractedData, *m.cfg.Separator)) {
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

// Cleanup implements Stage.
func (*dropStage) Cleanup() {
	// no-op
}
