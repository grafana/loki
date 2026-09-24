package distributor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/pkg/push"
	loghttppush "github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

// PushHandler reads a snappy-compressed proto from the HTTP body.
func (d *Distributor) PushHandler(w http.ResponseWriter, r *http.Request) {
	d.pushHandler(w, r, loghttppush.ParseLokiRequest, loghttppush.HTTPError, constants.Loki)
}

func (d *Distributor) OTLPPushHandler(w http.ResponseWriter, r *http.Request) {
	d.pushHandler(w, r, loghttppush.ParseOTLPRequest, loghttppush.OTLPError, constants.OTLP)
}

func (d *Distributor) pushHandler(w http.ResponseWriter, r *http.Request, pushRequestParser loghttppush.RequestParser, errorWriter loghttppush.ErrorWriter, format string) {
	logger := util_log.WithContext(r.Context(), d.logger)
	tenantID, err := tenant.TenantID(r.Context())
	if err != nil {
		level.Error(logger).Log("msg", "error getting tenant id", "err", err)
		errorWriter(w, err.Error(), http.StatusBadRequest, logger)
		return
	}

	recordFail := func(code int, errStr string) {
		if d.tenantConfigs.LogPushRequest(tenantID) {
			level.Debug(logger).Log(
				"msg", "push request failed",
				"code", code,
				"err", errStr,
			)
		}
		errorWriter(w, errStr, code, logger)
	}

	recordSuccess := func(msg string) {
		if d.tenantConfigs.LogPushRequest(tenantID) {
			level.Debug(logger).Log(
				"msg", msg,
			)
		}
		w.WriteHeader(http.StatusNoContent)
	}

	// TODO: In future, we want to be able to compose this with the middleware pattern,
	// but this requires refactor of this file to support next handlers. We will do
	// this at a later time.
	var (
		circuitBreakerOk       bool
		circuitBreakerDoneFunc func(err error)
		circuitBreakerErr      error
	)
	if d.circuitBreaker != nil {
		circuitBreakerOk, circuitBreakerDoneFunc = d.circuitBreaker.Allow()
		// Must be wrapped in a closure so circuitBreakerErr is evaluated when the
		// deferred function runs.
		defer func() { circuitBreakerDoneFunc(circuitBreakerErr) }()
		if !circuitBreakerOk {
			errorWriter(w, "circuit breaker open, request denied", http.StatusServiceUnavailable, logger)
			return
		}
	}

	// If the X-Loki-Backfill-Shard header is set, validate it and stash the shard in the request
	// context so the format parsers (Loki and OTLP) add the internal backfill labels to every stream.
	shard, ok, shardErr := loghttppush.ExtractAndValidateBackfillShard(r)
	if shardErr != nil {
		d.writeFailuresManager.Log(tenantID, fmt.Errorf("couldn't parse push request: %w", shardErr))
		recordFail(http.StatusBadRequest, shardErr.Error())
		return
	}
	if ok {
		r = r.Clone(loghttppush.InjectBackfillShardContext(r.Context(), shard))
	}

	// Create a request-scoped policy and retention resolver that will ensure consistent policy and retention resolution
	// across all parsers for this HTTP request.
	streamResolver := newRequestScopedStreamResolver(tenantID, d.validator.Limits, logger)
	presumedAgentIP := extractPresumedAgentIP(r)
	maxPushSize := d.validator.MaxPushSize(tenantID)

	req, pushStats, err := pushRequestParser(tenantID, r, d.validator.Limits, d.tenantConfigs, maxPushSize, int64(maxPushSize), d.usageTracker, streamResolver, logger)
	if err != nil {
		switch {
		// message too large handling
		case errors.Is(err, util.ErrMessageSizeTooLarge) || errors.Is(err, util.ErrMessageDecompressedSizeTooLarge):
			err = fmt.Errorf("%w: %s", loghttppush.ErrRequestBodyTooLarge, err.Error())
			fallthrough
		case errors.Is(err, loghttppush.ErrRequestBodyTooLarge):
			// count the compressed bytes received. it's the only info we've got
			if r.ContentLength > 0 {
				validation.DiscardedBytes.WithLabelValues(validation.RequestBodyTooLarge, tenantID, "", "", format).Add(float64(r.ContentLength))
			} else {
				level.Error(logger).Log(
					"msg", "negative content length observed",
					"tenantID", tenantID,
					"contentLength", r.ContentLength)
			}
			d.writeFailuresManager.Log(tenantID, fmt.Errorf("couldn't decompress push request: %w", err))
			recordFail(http.StatusRequestEntityTooLarge, err.Error())
			return

		// all logs filtered. a success!
		case errors.Is(err, loghttppush.ErrAllLogsFiltered):
			d.recordParsedRequest(r, logger, tenantID, format, presumedAgentIP, req, pushStats, streamResolver) // record metrics before early exit
			recordSuccess("successful push request filtered all lines")
			return

		// all other errors
		default:
			d.writeFailuresManager.Log(tenantID, fmt.Errorf("couldn't parse push request: %w", err))
			recordFail(http.StatusBadRequest, err.Error())
			return

		}
	}

	d.recordParsedRequest(r, logger, tenantID, format, presumedAgentIP, req, pushStats, streamResolver)

	_, err = d.pushWithResolver(r.Context(), logproto.FromPushRequest(req), streamResolver, format)
	if err != nil {
		circuitBreakerErr = err // assigned for the deferred func above
		resp, ok := httpgrpc.HTTPResponseFromError(err)
		if ok {
			recordFail(int(resp.Code), string(resp.Body))
		} else {
			recordFail(http.StatusInternalServerError, err.Error())
		}

		return
	}

	// success!
	recordSuccess("push request successful")
}

// recordParsedRequest emits all metrics and debug logging for a push request that was successfully parsed
// and is about to be forwarded to ingesters.
func (d *Distributor) recordParsedRequest(
	r *http.Request,
	logger log.Logger,
	tenantID, format, presumedAgentIP string,
	req *logproto.PushRequest,
	pushStats *loghttppush.Stats,
	streamResolver *requestScopedStreamResolver,
) {
	// Not every RequestParser is guaranteed to populate req/pushStats on every error it
	// returns (e.g. loghttppush.ErrAllLogsFiltered), so bail out rather than panic.
	if req == nil || pushStats == nil {
		return
	}

	var (
		entriesSize            int64
		structuredMetadataSize int64
	)

	hasInternalStreams := fmt.Sprintf("%t", pushStats.HasInternalStreams)

	for policyName, retentionToSizeMapping := range pushStats.LogLinesBytes {
		for retentionPeriod, size := range retentionToSizeMapping {
			retentionHours := loghttppush.RetentionPeriodToString(retentionPeriod)
			// Add guard clause to prevent negative values from being passed to Prometheus counters
			if size >= 0 {
				d.m.bytesIngested.WithLabelValues(tenantID, retentionHours, hasInternalStreams, policyName, format).Add(float64(size))
				bytesReceivedStats.Inc(size)
			} else {
				level.Error(logger).Log(
					"msg", "negative log lines bytes received",
					"userID", tenantID,
					"retentionHours", retentionHours,
					"hasInternalStreams", hasInternalStreams,
					"policyName", policyName,
					"size", size)
			}
			entriesSize += size
		}
	}

	for policyName, retentionToSizeMapping := range pushStats.StructuredMetadataBytes {
		for retentionPeriod, size := range retentionToSizeMapping {
			retentionHours := loghttppush.RetentionPeriodToString(retentionPeriod)

			// Add guard clause to prevent negative values from being passed to Prometheus counters
			if size >= 0 {
				d.m.structuredMetadataBytesIngested.WithLabelValues(tenantID, retentionHours, hasInternalStreams, policyName, format).Add(float64(size))
				d.m.bytesIngested.WithLabelValues(tenantID, retentionHours, hasInternalStreams, policyName, format).Add(float64(size))
				bytesReceivedStats.Inc(size)
				structuredMetadataBytesReceivedStats.Inc(size)
			} else {
				level.Error(logger).Log(
					"msg", "negative structured metadata bytes received",
					"userID", tenantID,
					"retentionHours", retentionHours,
					"hasInternalStreams", hasInternalStreams,
					"policyName", policyName,
					"size", size)
			}

			entriesSize += size
			structuredMetadataSize += size
		}
	}

	d.m.expandedBytesIngested.WithLabelValues(tenantID, format).Add(float64(pushStats.TotalExpandedEntriesSize))

	var totalNumLines int64
	// incrementing tenant metrics if we have a tenant.
	for policy, numLines := range pushStats.PolicyNumLines {
		if numLines != 0 && tenantID != "" {
			d.m.linesIngested.WithLabelValues(tenantID, hasInternalStreams, policy, format).Add(float64(numLines))
		}
		totalNumLines += numLines
	}
	linesReceivedStats.Inc(totalNumLines)
	mostRecentLagMs := time.Since(pushStats.MostRecentEntryTimestamp).Milliseconds()

	logValues := []interface{}{
		"msg", "push request parsed",
		"path", r.URL.Path,
		"contentType", pushStats.ContentType,
		"contentEncoding", pushStats.ContentEncoding,
		"bodySize", humanize.Bytes(uint64(pushStats.BodySize)),
		"streams", len(req.Streams),
		"entries", totalNumLines,
		"streamLabelsSize", humanize.Bytes(uint64(pushStats.StreamLabelsSize)),
		"entriesSize", humanize.Bytes(uint64(entriesSize)),
		"structuredMetadataSize", humanize.Bytes(uint64(structuredMetadataSize)),
		"totalSize", humanize.Bytes(uint64(entriesSize + pushStats.StreamLabelsSize)),
		"totalExpandedSize", humanize.Bytes(uint64(pushStats.TotalExpandedEntriesSize + pushStats.StreamLabelsSize)),
		"mostRecentLagMs", mostRecentLagMs,
	}

	if presumedAgentIP != "" {
		logValues = append(logValues, "presumedAgentIp", presumedAgentIP)
	}

	userAgent := r.Header.Get("User-Agent")
	// Sanitize the User-Agent to valid UTF-8 to prevent prometheus from panicking
	// when it's used as a label value in WithLabelValues.
	userAgent = strings.ToValidUTF8(userAgent, "")
	if userAgent != "" {
		logValues = append(logValues, "userAgent", strings.TrimSpace(userAgent))
	}
	// Since we're using a counter (so we can do things w/rate, irate, deriv, etc.) on the lag metrics,
	// dispatch a warning if we ever get a negative value.  This could occur if we start getting logs
	// whose timestamps are in the future (e.g. agents sending logs w/missing or invalid NTP configs).
	// Negative values can't give us much insight into whether-or-not a customer's ingestion is falling
	// behind, so we won't include it in the metrics, and instead will capture the occurrence in the
	// distributor logs.
	// We capture this metric even when the user agent is empty; we want insight into the tenant's
	// ingestion lag no matter what.
	if mostRecentLagMs >= 0 && mostRecentLagMs < 1_000_000_000 {
		// we're filtering out anything over 1B -- the OTLP endpoints often really mess with this metric...
		d.m.distributorLagByUserAgent.WithLabelValues(tenantID, userAgent, format).Add(float64(mostRecentLagMs))
	}

	if d.tenantConfigs != nil && d.tenantConfigs.LogHashOfLabels(tenantID) {
		resultHash := uint64(0)
		for _, stream := range req.Streams {
			// I don't believe a hash will be set, but if it is, use it.
			hash := stream.Hash
			if hash == 0 {
				// calculate an fnv32 hash of the stream labels
				// reusing our query hash function for simplicity
				hash = uint64(util.HashedQuery(stream.Labels))
			}
			// xor the hash with the result hash, this will result in the same hash regardless of the order of the streams
			resultHash ^= hash
		}
		logValues = append(logValues, "hashOfLabels", resultHash)
		pushStats.HashOfAllStreams = resultHash
	}

	logValues = append(logValues, pushStats.Extra...)
	level.Debug(logger).Log(logValues...)

	// Gather information about the different types of push formats Loki receives
	d.m.pushStatsCount.WithLabelValues(tenantID, pushStats.ContentType, pushStats.ContentEncoding, pushStats.ContentVersion, format).Inc()

	// Only reported for tenants that have enabled log_otlp_attribute_expansion in their runtime config.
	d.otlpAttrReporter.Report(logger, tenantID, pushStats.OTLPAttributes)

	if d.shouldLogPushRequestStreams(tenantID, presumedAgentIP) {
		d.logPushRequestStreams(r.Context(), logger, req.Streams, streamResolver, pushStats, presumedAgentIP)
	}
}

// shouldLogPushRequestStreams returns true if streams from the request should
// be logged, otherwise false.
func (d *Distributor) shouldLogPushRequestStreams(tenantID, presumedAgentIP string) bool {
	if !d.tenantConfigs.LogPushRequestStreams(tenantID) {
		return false
	}
	filterPushRequestStreamsIPs := d.tenantConfigs.FilterPushRequestStreamsIPs(tenantID)
	if len(filterPushRequestStreamsIPs) > 0 {
		// If there are filter IPs, we want to log if the presumed agent IP is in the list,
		// this would also then exclude any requests that don't have a presumed agent IP.
		return slices.Contains(filterPushRequestStreamsIPs, presumedAgentIP)
	}
	return true
}

// logPushRequestStreams logs all streams in the push request. This must be enabled
// on a per-tenant basis.
func (d *Distributor) logPushRequestStreams(
	ctx context.Context,
	logger log.Logger,
	streams []push.Stream,
	streamResolver *requestScopedStreamResolver,
	pushStats *loghttppush.Stats,
	presumedAgentIP string,
) {
	for _, s := range streams {
		lbs, err := syntax.ParseLabels(s.Labels)
		if err != nil {
			// We just log the error and continue, we need the parsed labels to log the policy.
			// In this case, the lbs will be empty and the policy will be empty.
			level.Error(logger).Log("msg", "error parsing labels before logging push request", "err", err)
		}

		logValues := []interface{}{
			"msg", "push request streams",
			"stream", s.Labels,
			"streamLabelsHash", util.HashedQuery(s.Labels), // this is to make it easier to do searching and grouping
			"streamSizeBytes", humanize.Bytes(uint64(pushStats.StreamSizeBytes[s.Labels])),
			"policy", streamResolver.PolicyFor(ctx, lbs),
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
