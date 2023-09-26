package dashboards

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	lokiStackChunkDashboardFile     = "grafana-dashboard-lokistack-chunks.json"
	lokiStackReadsDashboardFile     = "grafana-dashboard-lokistack-reads.json"
	lokiStackWritesDashboardFile    = "grafana-dashboard-lokistack-writes.json"
	lokiStackRetentionDashboardFile = "grafana-dashboard-lokistack-retention.json"
)

func TestContent(t *testing.T) {
	m, r := Content()
	require.Len(t, m, 4)
	require.Equal(t, dashboardMap, m)
	require.Equal(t, dashboardRules, r)

	require.Contains(t, m, lokiStackChunkDashboardFile)
	require.Contains(t, m, lokiStackReadsDashboardFile)
	require.Contains(t, m, lokiStackWritesDashboardFile)
	require.Contains(t, m, lokiStackRetentionDashboardFile)
	require.NotContains(t, m, lokiStackDashboardRulesFile)
}
