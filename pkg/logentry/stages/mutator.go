package stages

import (
	"fmt"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/common/model"
)

type Extractor interface {
	Value(source *string) (interface{}, error)
}

// return for other part of the stage.
type Mutator interface {
	Process(labels model.LabelSet, time *time.Time, entry *string) Extractor
}

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
