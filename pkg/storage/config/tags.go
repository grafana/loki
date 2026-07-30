package config

import (
	yaml "go.yaml.in/yaml/v4"
)

// Tags is a string-string map to be added to all managed tables.
type Tags map[string]string

// UnmarshalYAML implements yaml.Unmarshaler.
func (ts *Tags) UnmarshalYAML(value *yaml.Node) error {
	var m map[string]string
	if err := value.Decode(&m); err != nil {
		return err
	}
	*ts = Tags(m)
	return nil
}
