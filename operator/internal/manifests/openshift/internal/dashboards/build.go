package dashboards

import (
	"embed"
	"fmt"
	"path"
)

const (
	staticDir                       = "static"
	lokiStackChunkDashboardFile     = "grafana-dashboard-lokistack-chunks.json"
	lokiStackReadsDashboardFile     = "grafana-dashboard-lokistack-reads.json"
	lokiStackWritesDashboardFile    = "grafana-dashboard-lokistack-writes.json"
	lokiStackRetentionDashboardFile = "grafana-dashboard-lokistack-retention.json"
	lokiStackDashboardRulesFile     = "grafana-dashboard-lokistack-rules.json"
)

//go:embed static/*.json
var lokiStackDashboards embed.FS

// ReadDashboards returns the byte slices one for each LokiStack dashboard
// from the embedded filesystem or an error.
func ReadDashboards() (map[string][]byte, error) {
	chunks, err := lokiStackDashboards.ReadFile(path.Join(staticDir, lokiStackChunkDashboardFile))
	if err != nil {
		return nil, fmt.Errorf("failed to read chunks-dashboard file: %w", err)
	}

	reads, err := lokiStackDashboards.ReadFile(path.Join(staticDir, lokiStackReadsDashboardFile))
	if err != nil {
		return nil, fmt.Errorf("failed to read reads-dashboard file: %w", err)
	}

	writes, err := lokiStackDashboards.ReadFile(path.Join(staticDir, lokiStackWritesDashboardFile))
	if err != nil {
		return nil, fmt.Errorf("failed to read writes-dashboard file: %w", err)
	}

	retention, err := lokiStackDashboards.ReadFile(path.Join(staticDir, lokiStackRetentionDashboardFile))
	if err != nil {
		return nil, fmt.Errorf("failed to read retention-dashboard file: %w", err)
	}

	return map[string][]byte{
		lokiStackChunkDashboardFile:     chunks,
		lokiStackReadsDashboardFile:     reads,
		lokiStackWritesDashboardFile:    writes,
		lokiStackRetentionDashboardFile: retention,
	}, nil
}

// ReadDashboardRules returns the byte slice for the recording rules
// from the embedded filesystem or an error.
func ReadDashboardRules() ([]byte, error) {
	rules, err := lokiStackDashboards.ReadFile(path.Join(staticDir, lokiStackDashboardRulesFile))
	if err != nil {
		return nil, fmt.Errorf("failed to read dashboard-rules file: %w", err)
	}

	return rules, nil
}
