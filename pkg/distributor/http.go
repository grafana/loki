package distributor

import (
	"net/http"
	"strings"

	"github.com/go-kit/log/level"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/v2/pkg/util"

	"github.com/grafana/loki/v2/pkg/loghttp/push"
	"github.com/grafana/loki/v2/pkg/tenant"
	util_log "github.com/grafana/loki/v2/pkg/util/log"
	serverutil "github.com/grafana/loki/v2/pkg/util/server"
	"github.com/grafana/loki/v2/pkg/validation"
)

// PushHandler reads a snappy-compressed proto from the HTTP body.
func (d *Distributor) PushHandler(w http.ResponseWriter, r *http.Request) {
	logger := util_log.WithContext(r.Context(), util_log.Logger)
	userID, _ := tenant.TenantID(r.Context())
	req, err := push.ParseRequest(logger, userID, r, d.tenantsRetention)
	if err != nil {
		if d.tenantConfigs.LogPushRequest(userID) {
			level.Debug(logger).Log(
				"msg", "push request failed",
				"code", http.StatusBadRequest,
				"err", err,
			)
		}
		serverutil.JSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	if d.tenantConfigs.LogPushRequestStreams(userID) {
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
	if err == nil {
		if d.tenantConfigs.LogPushRequest(userID) {
			level.Debug(logger).Log(
				"msg", "push request successful",
			)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	resp, ok := httpgrpc.HTTPResponseFromError(err)
	if ok {
		body := string(resp.Body)
		if d.tenantConfigs.LogPushRequest(userID) {
			level.Debug(logger).Log(
				"msg", "push request failed",
				"code", resp.Code,
				"err", body,
			)
		}
		serverutil.JSONError(w, int(resp.Code), body)
	} else {
		if d.tenantConfigs.LogPushRequest(userID) {
			level.Debug(logger).Log(
				"msg", "push request failed",
				"code", http.StatusInternalServerError,
				"err", err.Error(),
			)
		}
		serverutil.JSONError(w, http.StatusInternalServerError, err.Error())
	}
}

// ServeHTTP implements the distributor ring status page.
//
// If the rate limiting strategy is local instead of global, no ring is used by
// the distributor and as such, no ring status is returned from this function.
func (d *Distributor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if d.rateLimitStrat == validation.GlobalIngestionRateStrategy {
		d.distributorsRing.ServeHTTP(w, r)
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
