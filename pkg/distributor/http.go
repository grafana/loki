package distributor

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"

	"github.com/grafana/loki/v3/pkg/util"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/loghttp/push"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

// PushHandler reads a snappy-compressed proto from the HTTP body.
func (d *Distributor) PushHandler(w http.ResponseWriter, r *http.Request) {
	d.pushHandler(w, r, push.ParseLokiRequest, false)
}

func (d *Distributor) OTLPPushHandler(w http.ResponseWriter, r *http.Request) {
	d.pushHandler(w, r, push.ParseOTLPRequest, true)
}

func (d *Distributor) pushHandler(w http.ResponseWriter, r *http.Request, pushRequestParser push.RequestParser, isOtel bool) {
	logger := util_log.WithContext(r.Context(), util_log.Logger)
	tenantID, err := tenant.TenantID(r.Context())
	if err != nil {
		level.Error(logger).Log("msg", "error getting tenant id", "err", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if d.RequestParserWrapper != nil {
		pushRequestParser = d.RequestParserWrapper(pushRequestParser)
	}

	req, err := push.ParseRequest(logger, tenantID, r, d.tenantsRetention, d.validator.Limits, pushRequestParser, d.usageTracker)
	if err != nil {
		if d.tenantConfigs.LogPushRequest(tenantID) {
			level.Debug(logger).Log(
				"msg", "push request failed",
				"code", http.StatusBadRequest,
				"err", err,
			)
		}
		d.writeFailuresManager.Log(tenantID, fmt.Errorf("couldn't parse push request: %w", err))

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
	if err == nil {
		if d.tenantConfigs.LogPushRequest(tenantID) {
			level.Debug(logger).Log(
				"msg", "push request successful",
			)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var body string
	var statusCode int
	resp, ok := httpgrpc.HTTPResponseFromError(err)
	if ok {
		statusCode = int(resp.Code)
		body = string(resp.Body)
	} else {
		statusCode = http.StatusInternalServerError
		body = err.Error()
	}

	// OTel spec won't retry 500 errors. So we map them to 503 which will be retried.
	if isOtel && statusCode == http.StatusInternalServerError {
		statusCode = http.StatusServiceUnavailable
	}

	if d.tenantConfigs.LogPushRequest(tenantID) {
		level.Debug(logger).Log(
			"msg", "push request failed",
			"code", http.StatusInternalServerError,
			"err", err.Error(),
		)
	}
	http.Error(w, body, statusCode)
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
