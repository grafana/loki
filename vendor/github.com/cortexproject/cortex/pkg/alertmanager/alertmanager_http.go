package alertmanager

import (
	"net/http"
	"text/template"

	"github.com/go-kit/kit/log/level"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/cortexproject/cortex/pkg/util/services"
)

var (
	statusPageTemplate = template.Must(template.New("main").Parse(`
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
)

func writeMessage(w http.ResponseWriter, message string) {
	w.WriteHeader(http.StatusOK)
	err := statusPageTemplate.Execute(w, struct {
		Message string
	}{Message: message})

	if err != nil {
		level.Error(util_log.Logger).Log("msg", "unable to serve alertmanager ring page", "err", err)
	}
}

func (am *MultitenantAlertmanager) RingHandler(w http.ResponseWriter, req *http.Request) {
	if !am.cfg.ShardingEnabled {
		writeMessage(w, "Alertmanager has no ring because sharding is disabled.")
		return
	}

	if am.State() != services.Running {
		// we cannot read the ring before the alertmanager is in Running state,
		// because that would lead to race condition.
		writeMessage(w, "Alertmanager is not running yet.")
		return
	}

	am.ring.ServeHTTP(w, req)
}
