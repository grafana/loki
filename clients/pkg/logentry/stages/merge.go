package stages

import (
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

var (
	// ErrEmptyMergeStageConfig is returned if config is empty
	ErrEmptyMergeStageConfig = errors.New("merge stage config cannot be empty")
	// ErrEmptyMergeStageConfig is returned if no source list is specified
	ErrMergeSourcesRequired = errors.New("merge sources are required")
	// ErrEmptyMergeStageConfig is returned if target is unspecified
	ErrMergeTargetRequired = errors.New("merge target is required")
)

// MergeConfig represents a Merge Stage configuration
type MergeConfig struct {
	Sources []string `mapstructure:"sources"`
	Target  string   `mapstructure:"target"`
}

func validateMergeConfig(c *MergeConfig) error {
	if c == nil {
		return ErrEmptyMergeStageConfig
	}
	if len(c.Sources) == 0 {
		return ErrMergeSourcesRequired
	}
	if c.Target == "" {
		return ErrMergeTargetRequired
	}

	return nil
}

func newMergeStage(logger log.Logger, configs interface{}) (Stage, error) {
	cfg := &MergeConfig{}
	err := mapstructure.Decode(configs, cfg)
	if err != nil {
		return nil, err
	}

	err = validateMergeConfig(cfg)
	if err != nil {
		return nil, err
	}

	return toStage(&mergeStage{
		cfg:    *cfg,
		logger: log.With(logger, "component", "stage", "type", "merge"),
	}), nil
}

type mergeStage struct {
	cfg    MergeConfig
	logger log.Logger
}

// Process implements Stage
func (m *mergeStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	result := make(map[string]interface{})
	for _, s := range m.cfg.Sources {
		if e, found := extracted[s]; found {
			switch te := e.(type) {
			case map[string]interface{}:
				// Maps are squashed
				for k, v := range te {
					result[k] = v
				}
			default:
				// Anything else is copied
				result[s] = te
			}
		} else {
			if Debug {
				level.Debug(m.logger).Log("msg", "extracted data did not contain merge source", "source", s)
			}
		}
	}
	extracted[m.cfg.Target] = result
}

// Name implements Stage
func (m *mergeStage) Name() string {
	return StageTypeMerge
}
