package push

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func Test_Unmarshal(t *testing.T) {
	raw := `
dev:
  - region
  - zone
integrations:
  - job
`
	var out CustomTrackersConfig
	err := yaml.Unmarshal([]byte(raw), &out)
	require.NoError(t, err)
	require.Contains(t, out.source, "dev")
	require.Contains(t, out.source, "integrations")
	require.ElementsMatch(t, out.source["dev"], []string{"region", "zone"})
	require.ElementsMatch(t, out.source["integrations"], []string{"job"})
}
