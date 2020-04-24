package api

import (
	"net/http"

	"github.com/go-kit/kit/log/level"
	"gopkg.in/yaml.v2"

	"github.com/cortexproject/cortex/pkg/util"
)

// TODO: Update this content to be a template that is dynamic based on how Cortex is run.
const indexPageContent = `
<!DOCTYPE html>
<html>
	<head>
		<meta charset="UTF-8">
		<title>Cortex</title>
	</head>
	<body>
		<h1>Cortex</h1>
		<p>Admin Endpoints:</p>
		<ul>
			<li><a href="/config">Current Config</a></li>
			<li><a href="/distributor/all_user_stats">Usage Statistics</a></li>
			<li><a href="/distributor/ha_tracker">HA Tracking Status</a></li>
			<li><a href="/multitenant_alertmanager/status">Alertmanager Status</a></li>
			<li><a href="/ingester/ring">Ingester Ring Status</a></li>
			<li><a href="/ruler/ring">Ruler Ring Status</a></li>
			<li><a href="/services">Service Status</a></li>
			<li><a href="/compactor/ring">Compactor Ring Status (experimental blocks storage)</a></li
			<li><a href="/store-gateway/ring">Store Gateway Ring (experimental blocks storage)</a></li>
		</ul>

		<p>Dangerous:</p>
		<ul>
			<li><a href="/ingester/flush">Trigger a Flush</a></li>
			<li><a href="/ingester/shutdown">Trigger Ingester Shutdown</a></li>
		</ul>
	</body>
</html>`

func indexHandler(w http.ResponseWriter, _ *http.Request) {
	if _, err := w.Write([]byte(indexPageContent)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func configHandler(cfg interface{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := yaml.Marshal(cfg)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/yaml")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(out); err != nil {
			level.Error(util.Logger).Log("msg", "error writing response", "err", err)
		}
	}
}
