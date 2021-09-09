package distributor

import (
	"net/http"
	"strings"

	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/dslog"
	"github.com/grafana/dskit/tenant"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/loghttp/push"
	util_log "github.com/grafana/loki/pkg/util/log"
)

// PushHandler reads a snappy-compressed proto from the HTTP body.
func (d *Distributor) PushHandler(w http.ResponseWriter, r *http.Request) {
	logger := dslog.WithContext(r.Context(), util_log.Logger)
	userID, _ := tenant.ID(r.Context())
	req, err := push.ParseRequest(logger, userID, r, d.tenantsRetention)
	if err != nil {
		if d.tenantConfigs.LogPushRequest(userID) {
			level.Debug(logger).Log(
				"msg", "push request failed",
				"code", http.StatusBadRequest,
				"err", err,
			)
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, body, int(resp.Code))
	} else {
		if d.tenantConfigs.LogPushRequest(userID) {
			level.Debug(logger).Log(
				"msg", "push request failed",
				"code", http.StatusInternalServerError,
				"err", err.Error(),
			)
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
