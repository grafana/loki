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

// StageFunc is modelled on http.HandlerFunc.
type StageFunc func(labels model.LabelSet, time *time.Time, entry *string)

// Process implements EntryHandler.
func (s StageFunc) Process(labels model.LabelSet, time *time.Time, entry *string) {
	s(labels, time, entry)
}
