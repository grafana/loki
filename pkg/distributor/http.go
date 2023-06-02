package distributor

import (
	"net/http"
	"strings"

	"github.com/go-kit/log/level"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/util"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/pkg/loghttp/push"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

// PushHandler reads a snappy-compressed proto from the HTTP body.
func (d *Distributor) PushHandler(w http.ResponseWriter, r *http.Request) {
	logger := util_log.WithContext(r.Context(), util_log.Logger)
	tenantID, err := tenant.TenantID(r.Context())
	if err != nil {
		level.Error(logger).Log("msg", "error getting tenant id", "err", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req, err := push.ParseRequest(logger, tenantID, r, d.tenantsRetention)
	if err != nil {
		if d.tenantConfigs.LogPushRequest(tenantID) {
			level.Debug(logger).Log(
				"msg", "push request failed",
				"code", http.StatusBadRequest,
				"err", err,
			)
		}
		d.writeFailuresManager.Log(tenantID, err)

		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if d.tenantConfigs.LogPushRequestStreams(tenantID) {
		var sb strings.Builder
		for _, s := range req.Streams {
			sb.WriteString(s.Labels)
		}
		level.Debug(logger).Log(
			"msg", "push request streams",
			"streams", sb.String(),
		)
	}

	_, err = d.Push(r.Context(), req)
	if d.logSender != nil {
		sendErr := d.logSender.Send(r.Context(), tenantID, req, r.Header)
		if sendErr != nil {
			level.Warn(logger).Log(
				"msg", "logSender send log fail",
				"err", "sendErr",
			)
			http.Error(w, sendErr.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err == nil {
		if d.tenantConfigs.LogPushRequest(tenantID) {
			level.Debug(logger).Log(
				"msg", "push request successful",
			)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	level.Error(logger).Log(
		"msg", "debug push request failed",
		"code", http.StatusInternalServerError,
		"err", err.Error(),
	)

	d.writeFailuresManager.Log(tenantID, err)

	resp, ok := httpgrpc.HTTPResponseFromError(err)
	if ok {
		body := string(resp.Body)
		if d.tenantConfigs.LogPushRequest(tenantID) {
			level.Debug(logger).Log(
				"msg", "push request failed",
				"code", resp.Code,
				"err", body,
			)
		}
		http.Error(w, body, int(resp.Code))
	} else {
		if d.tenantConfigs.LogPushRequest(tenantID) {
			level.Debug(logger).Log(
				"msg", "push request failed",
				"code", http.StatusInternalServerError,
				"err", err.Error(),
			)
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ServeHTTP implements the distributor ring status page.
//
// If the rate limiting strategy is local instead of global, no ring is used by
// the distributor and as such, no ring status is returned from this function.
func (d *Distributor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if d.rateLimitStrat == validation.GlobalIngestionRateStrategy {
		d.distributorsLifecycler.ServeHTTP(w, r)
		return
	}

	var noRingPage = `
			<!DOCTYPE html>
			<html>
				<head>
					<meta charset="UTF-8">
					<title>Distributor Ring Status</title>
				</head>
				<body>
					<h1>Distributor Ring Status</h1>
					<p>Not running with Global Rating Limit - ring not being used by the Distributor.</p>
				</body>
			</html>`
	util.WriteHTMLResponse(w, noRingPage)
}
