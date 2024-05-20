package stages

import (
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
)

const (
	StageTypeJSON            = "json"
	StageTypeLogfmt          = "logfmt"
	StageTypeRegex           = "regex"
	StageTypeReplace         = "replace"
	StageTypeMetric          = "metrics"
	StageTypeLabel           = "labels"
	StageTypeLabelDrop       = "labeldrop"
	StageTypeTimestamp       = "timestamp"
	StageTypeOutput          = "output"
	StageTypeDocker          = "docker"
	StageTypeCRI             = "cri"
	StageTypeMatch           = "match"
	StageTypeTemplate        = "template"
	StageTypePipeline        = "pipeline"
	StageTypeTenant          = "tenant"
	StageTypeDrop            = "drop"
	StageTypeSampling        = "sampling"
	StageTypeLimit           = "limit"
	StageTypeMultiline       = "multiline"
	StageTypePack            = "pack"
	StageTypeLabelAllow      = "labelallow"
	StageTypeStaticLabels    = "static_labels"
	StageTypeDecolorize      = "decolorize"
	StageTypeEventLogMessage = "eventlogmessage"
	StageTypeGeoIP           = "geoip"
	// Deprecated. Renamed to `structured_metadata`. Will be removed after the migration.
	StageTypeNonIndexedLabels   = "non_indexed_labels"
	StageTypeStructuredMetadata = "structured_metadata"
)

// Processor takes an existing set of labels, timestamp and log entry and returns either a possibly mutated
// timestamp and log entry
type Processor interface {
	Process(labels model.LabelSet, extracted map[string]interface{}, time *time.Time, entry *string)
	Name() string
}

type Entry struct {
	Extracted map[string]interface{}
	api.Entry
}

// Stage can receive entries via an inbound channel and forward mutated entries to an outbound channel.
type Stage interface {
	Name() string
	Run(chan Entry) chan Entry
	Cleanup()
}

func (entry *Entry) copy() *Entry {
	out, err := yaml.Marshal(entry)
	if err != nil {
		return nil
	}

	var n *Entry
	err = yaml.Unmarshal(out, &n)
	if err != nil {
		return nil
	}

	return n
}

// stageProcessor Allow to transform a Processor (old synchronous pipeline stage) into an async Stage
type stageProcessor struct {
	Processor

	inspector *inspector
}

func (s stageProcessor) Run(in chan Entry) chan Entry {
	return RunWith(in, func(e Entry) Entry {
		var before *Entry

		if Inspect {
			before = e.copy()
		}

		s.Process(e.Labels, e.Extracted, &e.Timestamp, &e.Line)

		if Inspect {
			s.inspector.inspect(s.Processor.Name(), before, e)
		}

		return e
	})
}

func toStage(p Processor) Stage {
	return &stageProcessor{
		Processor: p,
		inspector: newInspector(os.Stderr, runtime.GOOS == "windows"),
	}
}

type StageCreationParams struct {
	logger     log.Logger
	config     interface{}
	registerer prometheus.Registerer
	jobName    *string
}

type stageCreator func(StageCreationParams) (Stage, error)

var stageCreators map[string]stageCreator

var stageCreatorsInitLock sync.Mutex

// initCreators uses lazyLoading to resolve circular dependencies issue.
func initCreators() {
	if stageCreators != nil {
		return
	}
	stageCreatorsInitLock.Lock()
	defer stageCreatorsInitLock.Unlock()
	if stageCreators != nil {
		return
	}
	stageCreators = map[string]stageCreator{
		StageTypeDocker: func(params StageCreationParams) (Stage, error) {
			return NewDocker(params.logger, params.registerer)
		},
		StageTypeCRI: func(params StageCreationParams) (Stage, error) {
			return NewCRI(params.logger, params.config, params.registerer)
		},
		StageTypeJSON: func(params StageCreationParams) (Stage, error) {
			return newJSONStage(params.logger, params.config)
		},
		StageTypeLogfmt: func(params StageCreationParams) (Stage, error) {
			return newLogfmtStage(params.logger, params.config)
		},
		StageTypeRegex: func(params StageCreationParams) (Stage, error) {
			return newRegexStage(params.logger, params.config)
		},
		StageTypeMetric: func(params StageCreationParams) (Stage, error) {
			return newMetricStage(params.logger, params.config, params.registerer)
		},
		StageTypeLabel: func(params StageCreationParams) (Stage, error) {
			return newLabelStage(params.logger, params.config)
		},
		StageTypeLabelDrop: func(params StageCreationParams) (Stage, error) {
			return newLabelDropStage(params.config)
		},
		StageTypeTimestamp: func(params StageCreationParams) (Stage, error) {
			return newTimestampStage(params.logger, params.config)
		},
		StageTypeOutput: func(params StageCreationParams) (Stage, error) {
			return newOutputStage(params.logger, params.config)
		},
		StageTypeMatch: func(params StageCreationParams) (Stage, error) {
			return newMatcherStage(params.logger, params.jobName, params.config, params.registerer)
		},
		StageTypeTemplate: func(params StageCreationParams) (Stage, error) {
			return newTemplateStage(params.logger, params.config)
		},
		StageTypeTenant: func(params StageCreationParams) (Stage, error) {
			return newTenantStage(params.logger, params.config)
		},
		StageTypeReplace: func(params StageCreationParams) (Stage, error) {
			return newReplaceStage(params.logger, params.config)
		},
		StageTypeDrop: func(params StageCreationParams) (Stage, error) {
			return newDropStage(params.logger, params.config, params.registerer)
		},
		StageTypeSampling: func(params StageCreationParams) (Stage, error) {
			return newSamplingStage(params.logger, params.config, params.registerer)
		},
		StageTypeLimit: func(params StageCreationParams) (Stage, error) {
			return newLimitStage(params.logger, params.config, params.registerer)
		},
		StageTypeMultiline: func(params StageCreationParams) (Stage, error) {
			return newMultilineStage(params.logger, params.config)
		},
		StageTypePack: func(params StageCreationParams) (Stage, error) {
			return newPackStage(params.logger, params.config, params.registerer)
		},
		StageTypeLabelAllow: func(params StageCreationParams) (Stage, error) {
			return newLabelAllowStage(params.config)
		},
		StageTypeStaticLabels: func(params StageCreationParams) (Stage, error) {
			return newStaticLabelsStage(params.logger, params.config)
		},
		StageTypeDecolorize: func(params StageCreationParams) (Stage, error) {
			return newDecolorizeStage(params.config)
		},
		StageTypeEventLogMessage: func(params StageCreationParams) (Stage, error) {
			return newEventLogMessageStage(params.logger, params.config)
		},
		StageTypeGeoIP: func(params StageCreationParams) (Stage, error) {
			return newGeoIPStage(params.logger, params.config)
		},
		StageTypeNonIndexedLabels:   newStructuredMetadataStage,
		StageTypeStructuredMetadata: newStructuredMetadataStage,
	}
}

// New creates a new stage for the given type and configuration.
func New(logger log.Logger, jobName *string, stageType string,
	cfg interface{}, registerer prometheus.Registerer) (Stage, error) {
	initCreators()
	creator, ok := stageCreators[stageType]
	if !ok {
		return nil, errors.Errorf("Unknown stage type: %s", stageType)
	}
	params := StageCreationParams{
		logger:     logger,
		config:     cfg,
		registerer: registerer,
		jobName:    jobName,
	}
	return creator(params)
}

// Cleanup implements Stage.
func (*stageProcessor) Cleanup() {
	// no-op
}
