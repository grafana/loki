package push

import (
	"time"

	"github.com/prometheus/prometheus/model/labels"
)

type CustomTracker interface {
	IngestedBytesAdd(tenant string, retentionPeriod time.Duration, labels labels.Labels, value float64)
	DiscardedBytesAdd(tenant string, labels labels.Labels, value float64)
}

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

// MatchTrackers returns a list of names of all trackers that match the given labels.
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
