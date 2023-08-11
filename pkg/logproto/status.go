// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/cortexproject/cortex/blob/master/pkg/cortex/status.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Cortex Authors.

package mimir

import (
	_ "embed" // Used to embed html template
	"html/template"
	"net/http"
	"sort"
	"time"

	"github.com/grafana/mimir/pkg/util"
)

//go:embed status.gohtml
var statusPageHTML string
var statusPageTemplate = template.Must(template.New("webpage").Parse(statusPageHTML))

type statusPageContents struct {
	Now      time.Time       `json:"now"`
	Services []renderService `json:"services"`
}

type renderService struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func (t *Mimir) servicesHandler(w http.ResponseWriter, r *http.Request) {
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
	util.RenderHTTPResponse(w, statusPageContents{
		Now:      time.Now(),
		Services: svcs,
	}, statusPageTemplate, r)
}
