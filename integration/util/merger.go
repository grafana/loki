package util //nolint:revive

import (
	"fmt"

	"dario.cat/mergo"
	"go.yaml.in/yaml/v4"
)

// YAMLMerger takes a set of given YAML fragments and merges them into a single YAML document.
// The order in which these fragments is supplied is maintained, so subsequent fragments will override preceding ones.
//
// NOTE: Merging round-trips each fragment through a generic map[string]interface{}
// (see yamlToMap) and then re-marshals the result. This means scalars are resolved
// by their YAML core tags rather than by the target Go types, so type-specific
// UnmarshalYAML implementations do not run here. In particular, an unquoted date
// such as `from: 2023-01-01` is resolved via the !!timestamp tag into a time.Time
// and re-marshalled in canonical RFC3339 form (`2023-01-01T00:00:00Z`). When Loki
// later parses the merged config, DayTime.UnmarshalYAML then fails because it only
// accepts the `2006-01-02` layout. To avoid this, keep such dates quoted (e.g.
// `from: "2023-01-01"`) so they are treated as !!str and preserved verbatim.
type YAMLMerger struct {
	fragments [][]byte
}

func NewYAMLMerger() *YAMLMerger {
	return &YAMLMerger{}
}

func (m *YAMLMerger) AddFragment(fragment []byte) {
	m.fragments = append(m.fragments, fragment)
}

func (m *YAMLMerger) Merge() ([]byte, error) {
	merged := make(map[string]interface{})
	for _, fragment := range m.fragments {
		fragmentMap, err := yamlToMap(fragment)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal given fragment %q to map: %w", fragment, err)
		}

		if err = mergo.Merge(&merged, fragmentMap, mergo.WithOverride, mergo.WithTypeCheck, mergo.WithAppendSlice); err != nil {
			return nil, fmt.Errorf("failed to merge fragment %q with base: %w", fragment, err)
		}
	}

	mergedYAML, err := yaml.Marshal(merged)
	if err != nil {
		return nil, err
	}

	return mergedYAML, nil
}

func yamlToMap(fragment []byte) (interface{}, error) {
	var fragmentMap map[string]interface{}

	err := yaml.Unmarshal(fragment, &fragmentMap)
	if err != nil {
		return nil, err
	}

	return fragmentMap, nil
}
