package stages

import (
	"fmt"
	"reflect"
	"regexp"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/time/rate"
)

const (
	ErrLimitStageEmptyConfig        = "limit stage config must contain at least one of `source`, `expression`, `older_than` or `longer_than`"
	ErrLimitStageInvalidConfig      = "limit stage config error, `value` and `expression` cannot both be defined at the same time."
	ErrLimitStageInvalidRegex       = "limit stage regex compilation error: %v"
	ErrLimitStageInvalidRateOrBurst = "limit stage failed to parse rate or burst"
)

var ratelimitDropReason = "ratelimit_drop_stage"

type LimitConfig struct {
	Source     *string `mapstructure:"source"`
	Rate       float64 `mapstructure:"rate"`
	Burst      int     `mapstructure:"burst"`
	Drop       bool    `mapstructure:"drop"`
	Value      *string `mapstructure:"value"`
	Expression *string `mapstructure:"expression"`
	regex      *regexp.Regexp
}

func newLimitStage(logger log.Logger, config interface{}, registerer prometheus.Registerer) (Stage, error) {
	cfg := &LimitConfig{}

	err := mapstructure.WeakDecode(config, cfg)
	if err != nil {
		return nil, err
	}
	err = validateLimitConfig(cfg)
	if err != nil {
		return nil, err
	}

	r := &limitStage{
		logger:      log.With(logger, "component", "stage", "type", "limit"),
		cfg:         cfg,
		dropCount:   getDropCountMetric(registerer),
		rateLimiter: rate.NewLimiter(rate.Limit(cfg.Rate), cfg.Burst),
	}
	return r, nil
}

func validateLimitConfig(cfg *LimitConfig) error {
	if cfg == nil ||
		(cfg.Source == nil && cfg.Expression == nil) {
		return errors.New(ErrLimitStageEmptyConfig)
	}
	if cfg.Value != nil && cfg.Expression != nil {
		return errors.New(ErrLimitStageInvalidConfig)
	}
	if cfg.Expression != nil {
		expr, err := regexp.Compile(*cfg.Expression)
		if err != nil {
			return errors.Errorf(ErrLimitStageInvalidRegex, err)
		}
		cfg.regex = expr
	}
	if cfg.Rate <= 0 || cfg.Burst <= 0 {
		return errors.Errorf(ErrLimitStageInvalidRateOrBurst)
	}
	return nil
}

// limitStage applies Label matchers to determine if the include stages should be run
type limitStage struct {
	logger      log.Logger
	cfg         *LimitConfig
	rateLimiter *rate.Limiter
	dropCount   *prometheus.CounterVec
}

func (m *limitStage) Run(in chan Entry) chan Entry {
	out := make(chan Entry)
	go func() {
		defer close(out)
		for e := range in {
			if !m.shouldDrop(e) {
				out <- e
				continue
			}
		}
	}()
	return out
}

func (m *limitStage) shouldDrop(e Entry) bool {
	isMatchTag := m.isMatchTag(e)
	if !isMatchTag {
		return false
	}
	if m.cfg.Drop {
		if m.rateLimiter.Allow() {
			return false
		}
		m.dropCount.WithLabelValues(ratelimitDropReason).Inc()
		return true
	}
	_ = m.rateLimiter.Allow()
	return false
}

// Name implements Stage
func (m *limitStage) Name() string {
	return StageTypeLimit
}

func (m *limitStage) isMatchTag(e Entry) bool {
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
