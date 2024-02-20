package push

import (
	"github.com/prometheus/prometheus/model/labels"
)

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
func (c *CustomTrackersConfig) MatchTrackers(lbs labels.Labels) []string {
	trackers := make([]string, 0)
Outer:
	for name, labels := range c.source {
		for _, label := range labels {
			if !lbs.Has(label) {
				continue Outer
			}
		}
		// TODO: add label names
		trackers = append(trackers, name)
	}
	return trackers
}
