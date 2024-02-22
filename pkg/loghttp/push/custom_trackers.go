package push

import (
	"time"

	"github.com/prometheus/prometheus/model/labels"
)

type CustomTracker interface {

	// IngestedBytesAdd records ingested bytes by tenant, retention period and labels.
	IngestedBytesAdd(tenant string, retentionPeriod time.Duration, labels labels.Labels, value float64)

	// DiscardedBytesAdd records discarded bytes by tenant and labels.
	DiscardedBytesAdd(tenant string, labels labels.Labels, value float64)
}

// CustomTrackersConfig defines a map of tracker name to tracked label set.
type CustomTrackersConfig struct {
	source map[string][]string
}

var EmptyCustomTrackersConfig = &CustomTrackersConfig{
	source: map[string][]string{},
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
// CustomTrackersConfig are marshaled in yaml as a map[string]string, with matcher names as keys and strings as matchers definitions.
func (c *CustomTrackersConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	stringMap := map[string][]string{}
	err := unmarshal(&stringMap)
	if err != nil {
		return err
	}
	*c = *NewCustomTrackersConfig(stringMap)
	return nil
}

func NewCustomTrackersConfig(m map[string][]string) *CustomTrackersConfig {
	return &CustomTrackersConfig{
		source: m,
	}
}

// MatchTrackers returns a list of names of all trackers and labels that match the given labels.
// a "tracker" label is added to the matched list.
func (c *CustomTrackersConfig) MatchTrackers(lbs labels.Labels) []labels.Labels {
	trackers := make([]labels.Labels, 0)
Outer:
	for tracker, requiredLbs := range c.source {
		matchedLabels := make(labels.Labels, 0, len(requiredLbs))
		for _, name := range requiredLbs {
			if value := lbs.Get(name); value != "" {
				matchedLabels = append(matchedLabels, labels.Label{Name: name, Value: value})
			} else {
				continue Outer
			}
		}
		matchedLabels = append(matchedLabels, labels.Label{Name: "tracker", Value: tracker})
		trackers = append(trackers, matchedLabels)
	}
	return trackers
}
