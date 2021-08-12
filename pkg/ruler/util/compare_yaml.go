package util

import (
	"bytes"

	"gopkg.in/yaml.v2"
)

// CompareYAML marshals a and b to YAML and ensures that their contents are
// equal. If either Marshal fails, CompareYAML returns false.
func CompareYAML(a, b interface{}) bool {
	aBytes, err := yaml.Marshal(a)
	if err != nil {
		return false
	}
	bBytes, err := yaml.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(aBytes, bBytes)
}
