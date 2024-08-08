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
	d.pushHandler(w, r, push.ParseLokiRequest)
}

func (d *Distributor) OTLPPushHandler(w http.ResponseWriter, r *http.Request) {
	interceptor := newOtelErrorHeaderInterceptor(w)
	d.pushHandler(interceptor, r, push.ParseOTLPRequest)
}

// otelErrorHeaderInterceptor maps 500 errors to 503.
// According to the OTLP specification, 500 errors are never retried on the client side, but 503 are.
type otelErrorHeaderInterceptor struct {
	http.ResponseWriter
}

func newOtelErrorHeaderInterceptor(w http.ResponseWriter) *otelErrorHeaderInterceptor {
	return &otelErrorHeaderInterceptor{ResponseWriter: w}
}

func (i *otelErrorHeaderInterceptor) WriteHeader(statusCode int) {
	if statusCode == http.StatusInternalServerError {
		statusCode = http.StatusServiceUnavailable
	}

	i.ResponseWriter.WriteHeader(statusCode)
}

func (d *Distributor) pushHandler(w http.ResponseWriter, r *http.Request, pushRequestParser push.RequestParser) {
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
