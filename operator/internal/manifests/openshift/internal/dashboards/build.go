package dashboards

import (
	"embed"
	"io/fs"
	"path"
)

const (
	staticDir                   = "static"
	lokiStackDashboardRulesFile = "grafana-dashboard-lokistack-rules.json"
)

var (
	//go:embed static/*.json
	lokiStackDashboards embed.FS
	dashboardMap        map[string][]byte
	dashboardRules      []byte
)

func init() {
	var err error
	subDir, err := fs.Sub(lokiStackDashboards, staticDir)
	if err != nil {
		panic(err)
	}

	jsonFiles, err := fs.Glob(subDir, "*.json")
	if err != nil {
		panic(err)
	}

	dashboardMap = map[string][]byte{}
	for _, file := range jsonFiles {
		if file == lokiStackDashboardRulesFile {
			continue
		}

		var content []byte
		content, err = fs.ReadFile(subDir, file)
		if err != nil {
			panic(err)
		}

		dashboardMap[file] = content
	}

	dashboardRules, err = lokiStackDashboards.ReadFile(path.Join(staticDir, lokiStackDashboardRulesFile))
	if err != nil {
		panic(err)
	}
}

// ReadFiles returns the byte slices one for each LokiStack dashboard
// and a separate byte slice for the recording rules from the embedded filesystem.
func ReadFiles() (map[string][]byte, []byte) {
	return dashboardMap, dashboardRules
}
