package stages

import (
	"regexp"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logql"
)

package stages

import (
"time"

"github.com/prometheus/prometheus/pkg/labels"

"github.com/go-kit/kit/log"
"github.com/mitchellh/mapstructure"
"github.com/pkg/errors"
"github.com/prometheus/client_golang/prometheus"
"github.com/prometheus/common/model"

"github.com/grafana/loki/pkg/logql"
)

const (
	ErrEmptyMatchStageConfig = "match stage config cannot be empty"
	ErrPipelineNameRequired  = "match stage pipeline name can be omitted but cannot be an empty string"
	ErrSelectorRequired      = "selector statement required for match stage"
	ErrMatchRequiresStages   = "match stage requires at least one additional stage to be defined in '- stages'"
	ErrSelectorSyntax        = "invalid selector syntax for match stage"
	ErrStagesWithDropLine    = "match stage configured to drop entries cannot contains stages"
	ErrUnknownMatchAction    = "match stage action should be 'keep' or 'drop'"
	MatchActionKeep          = "keep"
	MatchActionDrop          = "drop"
)

// DropConfig contains the configuration for a dropStage
type DropConfig struct {
	Source     *string `mapstructure:"source"`
	Value      *string `mapstructure:"value"`
	Expression *string `mapstructure:"expression"`
	regex      *regexp.Regexp
	OlderThan  *string `mapstructure:"older_than"`
	olderThan  time.Duration
	LongerThan *string `mapstructure:"longer_than"`
	longerThan int
}

// validateDropConfig validates the DropConfig for the dropStage
func validateDropConfig(cfg *DropConfig) error {
	if cfg == nil {
		return nil, errors.New(ErrEmptyMatchStageConfig)
	}
	if cfg.PipelineName != nil && *cfg.PipelineName == "" {
		return nil, errors.New(ErrPipelineNameRequired)
	}
	if cfg.Selector == "" {
		return nil, errors.New(ErrSelectorRequired)
	}
	switch cfg.Action {
	case MatchActionKeep, MatchActionDrop:
	case "":
		cfg.Action = MatchActionKeep
	default:
		return nil, errors.New(ErrUnknownMatchAction)
	}

	if cfg.Action == MatchActionKeep && (cfg.Stages == nil || len(cfg.Stages) == 0) {
		return nil, errors.New(ErrMatchRequiresStages)
	}
	if cfg.Action == MatchActionDrop && (cfg.Stages != nil && len(cfg.Stages) != 0) {
		return nil, errors.New(ErrStagesWithDropLine)
	}

	selector, err := logql.ParseLogSelector(cfg.Selector)
	if err != nil {
		return nil, errors.Wrap(err, ErrSelectorSyntax)
	}
	return selector, nil
}

// newDropStage creates a DropStage from config
func newDropStage(logger log.Logger, jobName *string, config interface{}, registerer prometheus.Registerer) (Stage, error) {
	cfg := &DropConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	selector, err := validateDropConfig(cfg)
	if err != nil {
		return nil, err
	}

	var nPtr *string
	if cfg.PipelineName != nil && jobName != nil {
		name := *jobName + "_" + *cfg.PipelineName
		nPtr = &name
	}

	var pl *Pipeline
	if cfg.Action == MatchActionKeep {
		var err error
		pl, err = NewPipeline(logger, cfg.Stages, nPtr, registerer)
		if err != nil {
			return nil, errors.Wrapf(err, "match stage failed to create pipeline from config: %v", config)
		}
	}

	filter, err := selector.Filter()
	if err != nil {
		return nil, errors.Wrap(err, "error parsing filter")
	}

	return &dropStage{
		matchers: selector.Matchers(),
		pipeline: pl,
		action:   cfg.Action,
		filter:   filter,
	}, nil
}

// dropStage applies Label matchers to determine if the include stages should be run
type dropStage struct {
	cfg DropConfig
}

// Process implements Stage
func (m *dropStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	// There are many options for dropping a log and if multiple are defined it's treated like an AND condition
	// where all drop conditions must be met to drop the log.
	// Therefore if at any point there is a condition which does not match we can return.
	// The order is what I roughly think would be fastest check to slowest check to try to quit early whenever possible

	if m.cfg.LongerThan != nil {
		if len([]byte(*entry)) > m.cfg.longerThan {
			// Too long, drop
		} else {
			return
		}
	}

	if m.cfg.OlderThan != nil {
		if t.Before(time.Now().Add(-m.cfg.olderThan)) {
			// Too old, drop
		} else {
			return
		}
	}

	if m.cfg.Source != nil {
		if v, ok := extracted[*m.cfg.Source]; ok {
			if m.cfg.Value == nil {
				// Found in map, no value set meaning drop if found in map
			} else {
				if m.cfg.Value == v {
					// Found in map with value set for drop
				} else {
					// Value doesn't match, don't drop
					return
				}
			}
		} else {
			// Not found in extact map, don't drop
			return
		}
	}

	if m.cfg.Expression != nil && entry != nil {
		match := m.cfg.regex.FindStringSubmatch(*entry)
		if match == nil {
			// Not a match to the regex, don't drop
			return
		}
	}

	// Adds the drop label to not be sent by the api.EntryHandler
	labels[dropLabel] = ""
}

// Name implements Stage
func (m *dropStage) Name() string {
	return StageTypeDrop
}
