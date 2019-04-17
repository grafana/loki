package parsers

import (
	"time"

	"github.com/prometheus/common/model"
)

// Parser takes an existing set of labels, timestamp and log entry and returns either a possibly mutated
// timestamp and log entry
type Parser interface {
	//TODO decide on how to handle labels as a pointer or not
	Parse(labels model.LabelSet, time *time.Time, entry *string)
}
