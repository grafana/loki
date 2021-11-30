package distributor

import (
	"net/http"
	"strings"

	"github.com/cortexproject/cortex/pkg/tenant"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/go-kit/log/level"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/loghttp/push"
	serverutil "github.com/grafana/loki/pkg/util/server"
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
