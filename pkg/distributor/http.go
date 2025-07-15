package distributor

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"

	"github.com/grafana/loki/v3/pkg/util/constants"

	"github.com/grafana/loki/v3/pkg/util"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/loghttp/push"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

// PushHandler reads a snappy-compressed proto from the HTTP body.
func (d *Distributor) PushHandler(w http.ResponseWriter, r *http.Request) {
	d.pushHandler(w, r, push.ParseLokiRequest, push.HTTPError, constants.Loki)
}

func (d *Distributor) OTLPPushHandler(w http.ResponseWriter, r *http.Request) {
	d.pushHandler(w, r, push.ParseOTLPRequest, push.OTLPError, constants.OTLP)
}

func (d *Distributor) pushHandler(w http.ResponseWriter, r *http.Request, pushRequestParser push.RequestParser, errorWriter push.ErrorWriter, format string) {
	logger := util_log.WithContext(r.Context(), util_log.Logger)
	tenantID, err := tenant.TenantID(r.Context())
	if err != nil {
		level.Error(logger).Log("msg", "error getting tenant id", "err", err)
		errorWriter(w, err.Error(), http.StatusBadRequest, logger)
		return
	}

	if d.RequestParserWrapper != nil {
		pushRequestParser = d.RequestParserWrapper(pushRequestParser)
	}

	// Create a request-scoped policy and retention resolver that will ensure consistent policy and retention resolution
	// across all parsers for this HTTP request.
	streamResolver := newRequestScopedStreamResolver(tenantID, d.validator.Limits, logger)

	logPushRequestStreams := d.tenantConfigs.LogPushRequestStreams(tenantID)
	filterPushRequestStreamsIPs := d.tenantConfigs.FilterPushRequestStreamsIPs(tenantID)
	presumedAgentIP := extractPresumedAgentIP(r)
	req, pushStats, err := push.ParseRequest(logger, tenantID, d.cfg.MaxRecvMsgSize, r, d.validator.Limits, d.tenantConfigs,
		pushRequestParser, d.usageTracker, streamResolver, presumedAgentIP, format)
	if err != nil {
		switch {
		case errors.Is(err, push.ErrRequestBodyTooLarge):
			if d.tenantConfigs.LogPushRequest(tenantID) {
				level.Debug(logger).Log(
					"msg", "push request failed",
					"code", http.StatusRequestEntityTooLarge,
					"err", err,
				)
			}
			d.writeFailuresManager.Log(tenantID, fmt.Errorf("couldn't decompress push request: %w", err))

			// We count the compressed request body size here
			// because the request body could not be decompressed
			// and thus we don't know the uncompressed size.
			// In addition we don't add the metric label values for
			// `retention_hours` and `policy` because we don't know the labels.
			// Ensure ContentLength is positive to avoid counter panic
			if r.ContentLength > 0 {
				// Add empty values for retention_hours and policy labels since we don't have
				// that information for request body too large errors
				validation.DiscardedBytes.WithLabelValues(validation.RequestBodyTooLarge, tenantID, "", "", format).Add(float64(r.ContentLength))
			} else {
				level.Error(logger).Log(
					"msg", "negative content length observed",
					"tenantID", tenantID,
					"contentLength", r.ContentLength)
			}
			errorWriter(w, err.Error(), http.StatusRequestEntityTooLarge, logger)
			return

		case !errors.Is(err, push.ErrAllLogsFiltered):
			if d.tenantConfigs.LogPushRequest(tenantID) {
				level.Debug(logger).Log(
					"msg", "push request failed",
					"code", http.StatusBadRequest,
					"err", err,
				)
			}
			d.writeFailuresManager.Log(tenantID, fmt.Errorf("couldn't parse push request: %w", err))

			errorWriter(w, err.Error(), http.StatusBadRequest, logger)
			return

		default:
			if d.tenantConfigs.LogPushRequest(tenantID) {
				level.Debug(logger).Log(
					"msg", "successful push request filtered all lines",
				)
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	if logPushRequestStreams {
		shouldLog := true
		if len(filterPushRequestStreamsIPs) > 0 {
			// if there are filter IP's set, we only want to log if the presumed agent IP is in the list
			// this would also then exclude any requests that don't have a presumed agent IP
			shouldLog = slices.Contains(filterPushRequestStreamsIPs, presumedAgentIP)
		}

		if shouldLog {
			for _, s := range req.Streams {
				logValues := []interface{}{
					"msg", "push request streams",
					"stream", s.Labels,
					"streamLabelsHash", util.HashedQuery(s.Labels), // this is to make it easier to do searching and grouping
					"streamSizeBytes", humanize.Bytes(uint64(pushStats.StreamSizeBytes[s.Labels])),
				}
				if timestamp, ok := pushStats.MostRecentEntryTimestampPerStream[s.Labels]; ok {
					logValues = append(logValues, "mostRecentLagMs", time.Since(timestamp).Milliseconds())
				}
				if presumedAgentIP != "" {
					logValues = append(logValues, "presumedAgentIp", presumedAgentIP)
				}
				if pushStats.HashOfAllStreams != 0 {
					logValues = append(logValues, "hashOfAllStreams", pushStats.HashOfAllStreams)
				}
				level.Debug(logger).Log(logValues...)
			}
		}
	}

	_, err = d.PushWithResolver(r.Context(), req, streamResolver, format)
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
		errorWriter(w, body, int(resp.Code), logger)
	} else {
		if d.tenantConfigs.LogPushRequest(tenantID) {
			level.Debug(logger).Log(
				"msg", "push request failed",
				"code", http.StatusInternalServerError,
				"err", err.Error(),
			)
		}
		errorWriter(w, err.Error(), http.StatusInternalServerError, logger)
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

func extractPresumedAgentIP(r *http.Request) string {
	// X-Forwarded-For header may have 2 or more comma-separated addresses: the 2nd (and additional) are typically appended by proxies which handled the traffic.
	// Therefore, if the header is included, only log the first address
	return strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]
}
