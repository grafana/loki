package push

import (
	"fmt"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logql/syntax"
)

type CustomTrackersConfig struct {
	source map[string]string
	config map[string][]*labels.Matcher
}

var EmptyCustomTrackersConfig = &CustomTrackersConfig{
	source: map[string]string{},
	config: map[string][]*labels.Matcher{},
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
// CustomTrackersConfig are marshaled in yaml as a map[string]string, with matcher names as keys and strings as matchers definitions.
func (c *CustomTrackersConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	stringMap := map[string]string{}
	err := unmarshal(&stringMap)
	if err != nil {
		return err
	}
	tmp, err := NewCustomTrackersConfig(stringMap)
	*c = *tmp
	return err
}

func NewCustomTrackersConfig(m map[string]string) (*CustomTrackersConfig, error) {
	c := &CustomTrackersConfig{
		source: m,
		config: map[string][]*labels.Matcher{},
	}
	for name, selector := range m {
		matchers, err := syntax.ParseMatchers(selector, true)
		if err != nil {
			return c, fmt.Errorf("invalid labels matchers: %w", err)
		}
		c.config[name] = matchers
	}
	return c, nil
}

func MustNewCustomTrackersConfig(m map[string]string) *CustomTrackersConfig {
	c, err := NewCustomTrackersConfig(m)
	if err != nil {
		panic(err)
	}

	return c
}

// MatchTrackers returns a list of names of all trackers that match the given labels.
func (c *CustomTrackersConfig) MatchTrackers(lbs labels.Labels) []string {
	trackers := make([]string, 0)
Outer:
	for name, matchers := range c.config {
		for _, m := range matchers {
			if !m.Matches(lbs.Get(m.Name)) {
				continue Outer
			}
		}
		trackers = append(trackers, name)
	}
	return trackers
}
