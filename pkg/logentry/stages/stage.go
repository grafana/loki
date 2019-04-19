package stages

import (
	"time"

	"github.com/prometheus/common/model"
)

// Stage takes an existing set of labels, timestamp and log entry and returns either a possibly mutated
// timestamp and log entry
type Stage interface {
	Process(labels model.LabelSet, time *time.Time, entry *string)
}
