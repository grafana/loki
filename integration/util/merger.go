package util

import (
	"fmt"

	"dario.cat/mergo"
	"gopkg.in/yaml.v2"
)

// YAMLMerger takes a set of given YAML fragments and merges them into a single YAML document.
// The order in which these fragments is supplied is maintained, so subsequent fragments will override preceding ones.
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
	merged := make(map[interface{}]interface{})
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
	var fragmentMap map[interface{}]interface{}

	err := yaml.Unmarshal(fragment, &fragmentMap)
	if err != nil {
		return nil, err
	}

	return fragmentMap, nil
}
