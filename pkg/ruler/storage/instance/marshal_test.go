// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package instance

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestUnmarshalConfig_Valid(t *testing.T) {
	validConfig := DefaultConfig
	validConfigContent, err := yaml.Marshal(validConfig)
	require.NoError(t, err)

	_, err = UnmarshalConfig(bytes.NewReader(validConfigContent))
	require.NoError(t, err)
}

func TestUnmarshalConfig_Invalid(t *testing.T) {
	invalidConfigContent := `whyWouldAnyoneThinkThisisAValidConfig: 12345`

	_, err := UnmarshalConfig(strings.NewReader(invalidConfigContent))
	require.Error(t, err)
}
