package dashboards

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadDashboards(t *testing.T) {
	m, err := ReadDashboards()
	require.NoError(t, err)
	require.Len(t, m, 4)

	// Chunks Dashboard
	require.Contains(t, m, lokiStackChunkDashboardFile)
	require.NotEmpty(t, m[lokiStackChunkDashboardFile])
	// Reads Dasboard
	require.Contains(t, m, lokiStackReadsDashboardFile)
	require.NotEmpty(t, m[lokiStackReadsDashboardFile])
	// Writes  Dashboard
	require.Contains(t, m, lokiStackWritesDashboardFile)
	require.NotEmpty(t, m[lokiStackWritesDashboardFile])
	// Retention Dashboard
	require.Contains(t, m, lokiStackRetentionDashboardFile)
	require.NotEmpty(t, m[lokiStackRetentionDashboardFile])

	require.NotContains(t, m, lokiStackDashboardRulesFile)
}

func TestReadDashboardRules(t *testing.T) {
	b, err := ReadDashboardRules()
	require.NoError(t, err)
	require.NotEmpty(t, b)
}
