package cortex

import (
	"html/template"
	"net/http"
	"sort"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
)

const tpl = `
<!DOCTYPE html>
<html>
	<head>
		<meta charset="UTF-8">
		<title>Cortex Services Status</title>
	</head>
	<body>
		<h1>Cortex Services Status</h1>
		<p>Current time: {{ .Now }}</p>
		<table border="1">
			<thead>
				<tr>
					<th>Service</th>
					<th>Status</th>
				</tr>
			</thead>
			<tbody>
				{{ range .Services }}
				<tr>
					<td>{{ .Name }}</td>
					<td>{{ .Status }}</td>
				</tr>
				{{ end }}
			</tbody>
		</table>
	</body>
</html>`

var tmpl *template.Template

type renderService struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func init() {
	tmpl = template.Must(template.New("webpage").Parse(tpl))
}

func (t *Cortex) servicesHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
	w.Header().Set("Content-Type", "text/plain")

	svcs := make([]renderService, 0)
	for mod, s := range t.ServiceMap {
		svcs = append(svcs, renderService{
			Name:   mod,
			Status: s.State().String(),
		})
	}
	sort.Slice(svcs, func(i, j int) bool {
		return svcs[i].Name < svcs[j].Name
	})

	// TODO: this could be extended to also print sub-services, if given service has any
	util.RenderHTTPResponse(w, struct {
		Now      time.Time       `json:"now"`
		Services []renderService `json:"services"`
	}{
		Now:      time.Now(),
		Services: svcs,
	}, tmpl, r)
}
