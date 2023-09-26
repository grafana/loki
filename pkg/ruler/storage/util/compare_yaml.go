// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
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
