package stages

import (
	"fmt"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/common/model"
)

// Extractor knows how to extract a value from a log line using an expression (e.g. regex and json)
type Extractor interface {
	Value(expression *string) (interface{}, error)
}

// Mutator mutates log entries and returns an Extractor for other part of the stage to extract values.
type Mutator interface {
	Process(labels model.LabelSet, time *time.Time, entry *string) Extractor
}

// newMutator creates a new Mutator for a given stage type.
func newMutator(logger log.Logger, stageType string, cfg *StageConfig) (Mutator, error) {
	switch stageType {
	case StageTypeJSON:
		return newJSONMutator(logger, cfg)
	case StageTypeRegex:
		return newRegexMutator(logger, cfg)
	default:
		return nil, fmt.Errorf("unknown mutator for stage type: %v", stageType)
	}
}
