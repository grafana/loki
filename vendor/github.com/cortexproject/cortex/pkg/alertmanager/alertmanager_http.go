package alertmanager

import (
	"net/http"
	"text/template"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
)

var (
	ringStatusPageTemplate = template.Must(template.New("ringStatusPage").Parse(`
	<!DOCTYPE html>
	<html>
		<head>
			<meta charset="UTF-8">
			<title>Cortex Alertmanager Ring</title>
		</head>
		<body>
			<h1>Cortex Alertmanager Ring</h1>
			<p>{{ .Message }}</p>
		</body>
	</html>`))

	statusTemplate = template.Must(template.New("statusPage").Parse(`
    <!doctype html>
    <html>
        <head><title>Cortex Alertmanager Status</title></head>
        <body>
            <h1>Cortex Alertmanager Status</h1>
            {{ if not .ClusterInfo }}
                <p>Alertmanager gossip-based clustering is disabled.</p>
            {{ else }}
                <h2>Node</h2>
                <dl>
                    <dt>Name</dt><dd>{{.ClusterInfo.self.Name}}</dd>
                    <dt>Addr</dt><dd>{{.ClusterInfo.self.Addr}}</dd>
                    <dt>Port</dt><dd>{{.ClusterInfo.self.Port}}</dd>
                </dl>
                <h3>Members</h3>
                {{ with .ClusterInfo.members }}
                <table>
                    <tr><th>Name</th><th>Addr</th></tr>
                    {{ range . }}
                    <tr><td>{{ .Name }}</td><td>{{ .Addr }}</td></tr>
                    {{ end }}
                </table>
                {{ else }}
                <p>No peers</p>
                {{ end }}
            {{ end }}
        </body>
    </html>`))
)

func writeRingStatusMessage(w http.ResponseWriter, message string) {
	w.WriteHeader(http.StatusOK)
	err := ringStatusPageTemplate.Execute(w, struct {
		Message string
	}{Message: message})
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "unable to serve alertmanager ring page", "err", err)
	}
}

func (am *MultitenantAlertmanager) RingHandler(w http.ResponseWriter, req *http.Request) {
	if !am.cfg.ShardingEnabled {
		writeRingStatusMessage(w, "Alertmanager has no ring because sharding is disabled.")
		return
	}

	if am.State() != services.Running {
		// we cannot read the ring before the alertmanager is in Running state,
		// because that would lead to race condition.
		writeRingStatusMessage(w, "Alertmanager is not running yet.")
		return
	}

	am.ring.ServeHTTP(w, req)
}

// GetStatusHandler returns the status handler for this multi-tenant
// alertmanager.
func (am *MultitenantAlertmanager) GetStatusHandler() StatusHandler {
	return StatusHandler{
		am: am,
	}
}

// StatusHandler shows the status of the alertmanager.
type StatusHandler struct {
	am *MultitenantAlertmanager
}

// ServeHTTP serves the status of the alertmanager.
func (s StatusHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	var clusterInfo map[string]interface{}
	if s.am.peer != nil {
		clusterInfo = s.am.peer.Info()
	}
	err := statusTemplate.Execute(w, struct {
		ClusterInfo map[string]interface{}
	}{
		ClusterInfo: clusterInfo,
	})
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "unable to serve alertmanager status page", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
