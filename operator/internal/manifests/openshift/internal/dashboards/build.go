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
	subDir, err := fs.Sub(lokiStackDashboards, staticDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read embed fs subdir: %w", err)
	}

	jsonFiles, err := fs.Glob(subDir, "*.json")
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	dashboardMap := map[string][]byte{}
	for _, file := range jsonFiles {
		if file == lokiStackDashboardRulesFile {
			continue
		}

		content, err := fs.ReadFile(subDir, file)
		if err != nil {
			return nil, fmt.Errorf("failed to read %q: %w", file, err)
		}

		dashboardMap[file] = content
	}

	return dashboardMap, nil
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
