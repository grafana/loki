package stages

import (
	"github.com/go-kit/log"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/clients/pkg/logentry/logql"
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

// MatcherConfig contains the configuration for a matcherStage
type MatcherConfig struct {
	PipelineName *string        `mapstructure:"pipeline_name"`
	Selector     string         `mapstructure:"selector"`
	Stages       PipelineStages `mapstructure:"stages"`
	Action       string         `mapstructure:"action"`
	DropReason   *string        `mapstructure:"drop_counter_reason"`
}

// validateMatcherConfig validates the MatcherConfig for the matcherStage
func validateMatcherConfig(cfg *MatcherConfig) (logql.Expr, error) {
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

	if cfg.Action == MatchActionKeep && (len(cfg.Stages) == 0) {
		return nil, errors.New(ErrMatchRequiresStages)
	}
	if cfg.Action == MatchActionDrop && (len(cfg.Stages) != 0) {
		return nil, errors.New(ErrStagesWithDropLine)
	}

	selector, err := logql.ParseExpr(cfg.Selector)
	if err != nil {
		return nil, errors.Wrap(err, ErrSelectorSyntax)
	}
	return selector, nil
}

// newMatcherStage creates a new matcherStage from config
func newMatcherStage(logger log.Logger, jobName *string, config interface{}, registerer prometheus.Registerer) (Stage, error) {
	cfg := &MatcherConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	selector, err := validateMatcherConfig(cfg)
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
		return nil, errors.Wrap(err, "error parsing pipeline")
	}

	dropReason := "match_stage"
	if cfg.DropReason != nil && *cfg.DropReason != "" {
		dropReason = *cfg.DropReason
	}

	return &matcherStage{
		dropReason: dropReason,
		dropCount:  getDropCountMetric(registerer),
		matchers:   selector.Matchers(),
		stage:      pl,
		action:     cfg.Action,
		filter:     filter,
	}, nil
}

func getDropCountMetric(registerer prometheus.Registerer) *prometheus.CounterVec {
	dropCount := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "logentry",
		Name:      "dropped_lines_total",
		Help:      "A count of all log lines dropped as a result of a pipeline stage",
	}, []string{"reason"})
	err := registerer.Register(dropCount)
	if err != nil {
		if existing, ok := err.(prometheus.AlreadyRegisteredError); ok {
			dropCount = existing.ExistingCollector.(*prometheus.CounterVec)
		} else {
			// Same behavior as MustRegister if the error is not for AlreadyRegistered
			panic(err)
		}
	}
	return dropCount
}

// matcherStage applies Label matchers to determine if the include stages should be run
type matcherStage struct {
	dropReason string
	dropCount  *prometheus.CounterVec
	matchers   []*labels.Matcher
	filter     logql.Filter
	stage      Stage
	action     string
}

func (m *matcherStage) Run(in chan Entry) chan Entry {
	switch m.action {
	case MatchActionDrop:
		return m.runDrop(in)
	case MatchActionKeep:
		return m.runKeep(in)
	}
	panic("unexpected action")
}

func (m *matcherStage) runKeep(in chan Entry) chan Entry {
	next := make(chan Entry)
	out := make(chan Entry)
	outNext := m.stage.Run(next)
	go func() {
		defer close(out)
		for e := range outNext {
			out <- e
		}
	}()
	go func() {
		defer close(next)
		for e := range in {
			e, ok := m.processLogQL(e)
			if !ok {
				out <- e
				continue
			}
			next <- e
		}
	}()
	return out
}

func (m *matcherStage) runDrop(in chan Entry) chan Entry {
	out := make(chan Entry)
	go func() {
		defer close(out)
		for e := range in {
			if e, ok := m.processLogQL(e); !ok {
				out <- e
				continue
			}
			m.dropCount.WithLabelValues(m.dropReason).Inc()
		}
	}()
	return out
}

func (m *matcherStage) processLogQL(e Entry) (Entry, bool) {
	for _, filter := range m.matchers {
		if !filter.Matches(string(e.Labels[model.LabelName(filter.Name)])) {
			return e, false
		}
	}

	if m.filter == nil || m.filter([]byte(e.Line)) {
		return e, true
	}
	return e, false
}

// Name implements Stage
func (m *matcherStage) Name() string {
	return StageTypeMatch
}

// Cleanup implements Stage.
func (*matcherStage) Cleanup() {
	// no-op
}
