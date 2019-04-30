package targets

import (
	"github.com/prometheus/common/model"
)

// TargetType is the type of target
type TargetType string

const (
	// FileTargetType a file target
	FileTargetType = TargetType("File")
)

// Target is a promtail scrape target
type Target interface {
	// Type of the target
	Type() TargetType
	// Ready tells if the targets is ready
	Ready() bool
	// Labels before any processing.
	DiscoveredLabels() model.LabelSet
	// Any labels that are added to this target and its stream
	Labels() model.LabelSet
	// Details is additional information about this target specific to its type
	Details() interface{}
}
