package push

import (
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/push"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/gogo/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/util"
	loki_util "github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/unmarshal"
	unmarshal2 "github.com/grafana/loki/v3/pkg/util/unmarshal/legacy"
)

var (
	contentType   = http.CanonicalHeaderKey("Content-Type")
	contentEnc    = http.CanonicalHeaderKey("Content-Encoding")
	bytesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "distributor_bytes_received_total",
		Help:      "The total number of uncompressed bytes received per tenant. Includes structured metadata bytes.",
	}, []string{"tenant", "retention_hours", "is_internal_stream", "policy", "format"})

	structuredMetadataBytesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "distributor_structured_metadata_bytes_received_total",
		Help:      "The total number of uncompressed bytes received per tenant for entries' structured metadata",
	}, []string{"tenant", "retention_hours", "is_internal_stream", "policy", "format"})
	linesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "distributor_lines_received_total",
		Help:      "The total number of lines received per tenant",
	}, []string{"tenant", "is_internal_stream", "policy", "format"})

	otlpExporterStreams = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "distributor_otlp_exporter_streams_total",
		Help:      "The total number of streams with exporter=OTLP label",
	}, []string{"tenant"})

	distributorLagByUserAgent = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "distributor_lag_ms_total",
		Help:      "The difference in time (in millis) between when a distributor receives a push request and the most recent log timestamp in that request",
	}, []string{"tenant", "userAgent", "format"})

	bytesReceivedStats                   = analytics.NewCounter("distributor_bytes_received")
	structuredMetadataBytesReceivedStats = analytics.NewCounter("distributor_structured_metadata_bytes_received")
	linesReceivedStats                   = analytics.NewCounter("distributor_lines_received")
)

const (
	applicationJSON  = "application/json"
	LabelServiceName = "service_name"
	ServiceUnknown   = "unknown_service"
)

var (
	ErrAllLogsFiltered     = errors.New("all logs lines filtered during parsing")
	ErrRequestBodyTooLarge = errors.New("request body too large")
)

type TenantsRetention interface {
	RetentionPeriodFor(userID string, lbs labels.Labels) time.Duration
}

type Limits interface {
	OTLPConfig(userID string) OTLPConfig
	DiscoverServiceName(userID string) []string
}

type EmptyLimits struct{}

func (EmptyLimits) OTLPConfig(string) OTLPConfig {
	return DefaultOTLPConfig(GlobalOTLPConfig{})
}

func (EmptyLimits) DiscoverServiceName(string) []string {
	return nil
}

func (EmptyLimits) PolicyFor(_ string, _ labels.Labels) string {
	return ""
}

// StreamResolver is a request-scoped interface that provides retention period and policy for a given stream.
// The values returned by the resolver will not chance thought the handling of the request
type StreamResolver interface {
	RetentionPeriodFor(lbs labels.Labels) time.Duration
	RetentionHoursFor(lbs labels.Labels) string
	PolicyFor(lbs labels.Labels) string
}

type (
	RequestParser        func(userID string, r *http.Request, limits Limits, tenantConfigs *runtime.TenantConfigs, maxRecvMsgSize int, tracker UsageTracker, streamResolver StreamResolver, logger log.Logger) (*logproto.PushRequest, *Stats, error)
	RequestParserWrapper func(inner RequestParser) RequestParser
	ErrorWriter          func(w http.ResponseWriter, errorStr string, code int, logger log.Logger)
)

type PolicyWithRetentionWithBytes map[string]map[time.Duration]int64

func NewPushStats() *Stats {
	return &Stats{
		LogLinesBytes:                     map[string]map[time.Duration]int64{},
		StructuredMetadataBytes:           map[string]map[time.Duration]int64{},
		PolicyNumLines:                    map[string]int64{},
		ResourceAndSourceMetadataLabels:   map[string]map[time.Duration]push.LabelsAdapter{},
		MostRecentEntryTimestampPerStream: map[string]time.Time{},
		StreamSizeBytes:                   map[string]int64{},
	}
}

type Stats struct {
	Errs                              []error
	PolicyNumLines                    map[string]int64
	LogLinesBytes                     PolicyWithRetentionWithBytes
	StructuredMetadataBytes           PolicyWithRetentionWithBytes
	ResourceAndSourceMetadataLabels   map[string]map[time.Duration]push.LabelsAdapter
	StreamLabelsSize                  int64
	MostRecentEntryTimestamp          time.Time
	MostRecentEntryTimestampPerStream map[string]time.Time
	StreamSizeBytes                   map[string]int64
	HashOfAllStreams                  uint64
	ContentType                       string
	ContentEncoding                   string

	BodySize int64
	// Extra is a place for a wrapped perser to record any interesting stats as key-value pairs to be logged
	Extra []any

	IsInternalStream bool // True for aggregated metrics or pattern streams
}

func ParseRequest(logger log.Logger, userID string, maxRecvMsgSize int, r *http.Request, limits Limits, tenantConfigs *runtime.TenantConfigs, pushRequestParser RequestParser, tracker UsageTracker, streamResolver StreamResolver, presumedAgentIP, format string) (*logproto.PushRequest, *Stats, error) {
	req, pushStats, err := pushRequestParser(userID, r, limits, tenantConfigs, maxRecvMsgSize, tracker, streamResolver, logger)
	if err != nil && !errors.Is(err, ErrAllLogsFiltered) {
		if errors.Is(err, loki_util.ErrMessageSizeTooLarge) {
			return nil, nil, fmt.Errorf("%w: %s", ErrRequestBodyTooLarge, err.Error())
		}
		return nil, nil, err
	}

	var (
		entriesSize            int64
		structuredMetadataSize int64
	)

	isInternalStream := fmt.Sprintf("%t", pushStats.IsInternalStream)

	for policyName, retentionToSizeMapping := range pushStats.LogLinesBytes {
		for retentionPeriod, size := range retentionToSizeMapping {
			retentionHours := RetentionPeriodToString(retentionPeriod)
			// Add guard clause to prevent negative values from being passed to Prometheus counters
			if size >= 0 {
				bytesIngested.WithLabelValues(userID, retentionHours, isInternalStream, policyName, format).Add(float64(size))
				bytesReceivedStats.Inc(size)
			} else {
				level.Error(logger).Log(
					"msg", "negative log lines bytes received",
					"userID", userID,
					"retentionHours", retentionHours,
					"isInternalStream", isInternalStream,
					"policyName", policyName,
					"size", size)
			}
			entriesSize += size
		}
	}

	for policyName, retentionToSizeMapping := range pushStats.StructuredMetadataBytes {
		for retentionPeriod, size := range retentionToSizeMapping {
			retentionHours := RetentionPeriodToString(retentionPeriod)

			// Add guard clause to prevent negative values from being passed to Prometheus counters
			if size >= 0 {
				structuredMetadataBytesIngested.WithLabelValues(userID, retentionHours, isInternalStream, policyName, format).Add(float64(size))
				bytesIngested.WithLabelValues(userID, retentionHours, isInternalStream, policyName, format).Add(float64(size))
				bytesReceivedStats.Inc(size)
				structuredMetadataBytesReceivedStats.Inc(size)
			} else {
				level.Error(logger).Log(
					"msg", "negative structured metadata bytes received",
					"userID", userID,
					"retentionHours", retentionHours,
					"isInternalStream", isInternalStream,
					"policyName", policyName,
					"size", size)
			}

			entriesSize += size
			structuredMetadataSize += size
		}
	}

	var totalNumLines int64
	// incrementing tenant metrics if we have a tenant.
	for policy, numLines := range pushStats.PolicyNumLines {
		if numLines != 0 && userID != "" {
			linesIngested.WithLabelValues(userID, isInternalStream, policy, format).Add(float64(numLines))
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
		"mostRecentLagMs", mostRecentLagMs,
	}

	if presumedAgentIP != "" {
		logValues = append(logValues, "presumedAgentIp", presumedAgentIP)
	}

	userAgent := r.Header.Get("User-Agent")
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
		distributorLagByUserAgent.WithLabelValues(userID, userAgent, format).Add(float64(mostRecentLagMs))
	}

	if tenantConfigs != nil && tenantConfigs.LogHashOfLabels(userID) {
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

	return req, pushStats, err
}

func ParseLokiRequest(userID string, r *http.Request, limits Limits, tenantConfigs *runtime.TenantConfigs, maxRecvMsgSize int, tracker UsageTracker, streamResolver StreamResolver, logger log.Logger) (*logproto.PushRequest, *Stats, error) {
	// Body
	var body io.Reader
	// bodySize should always reflect the compressed size of the request body
	bodySize := loki_util.NewSizeReader(r.Body)
	contentEncoding := r.Header.Get(contentEnc)
	switch contentEncoding {
	case "":
		body = bodySize
	case "snappy":
		// Snappy-decoding is done by `util.ParseProtoReader(..., util.RawSnappy)` below.
		// Pass on body bytes. Note: HTTP clients do not need to set this header,
		// but they sometimes do. See #3407.
		body = bodySize
	case "gzip":
		gzipReader, err := gzip.NewReader(bodySize)
		if err != nil {
			return nil, nil, err
		}
		defer gzipReader.Close()
		body = gzipReader
	case "deflate":
		flateReader := flate.NewReader(bodySize)
		defer flateReader.Close()
		body = flateReader
	default:
		return nil, nil, fmt.Errorf("Content-Encoding %q not supported", contentEncoding)
	}

	contentType := r.Header.Get(contentType)
	var (
		req       logproto.PushRequest
		pushStats = NewPushStats()
	)

	contentType, _ /* params */, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, nil, err
	}

	switch contentType {
	case applicationJSON:

		var err error

		// todo once https://github.com/weaveworks/common/commit/73225442af7da93ec8f6a6e2f7c8aafaee3f8840 is in Loki.
		// We can try to pass the body as bytes.buffer instead to avoid reading into another buffer.
		if loghttp.GetVersion(r.RequestURI) == loghttp.VersionV1 {
			err = unmarshal.DecodePushRequest(body, &req)
		} else {
			err = unmarshal2.DecodePushRequest(body, &req)
		}

		if err != nil {
			return nil, nil, err
		}

	default:
		// When no content-type header is set or when it is set to
		// `application/x-protobuf`: expect snappy compression.
		if err := util.ParseProtoReader(r.Context(), body, int(r.ContentLength), maxRecvMsgSize, &req, util.RawSnappy); err != nil {
			return nil, nil, err
		}
	}

	pushStats.BodySize = bodySize.Size()
	pushStats.ContentType = contentType
	pushStats.ContentEncoding = contentEncoding

	discoverServiceName := limits.DiscoverServiceName(userID)

	logServiceNameDiscovery := false
	logPushRequestStreams := false
	if tenantConfigs != nil {
		logServiceNameDiscovery = tenantConfigs.LogServiceNameDiscovery(userID)
		logPushRequestStreams = tenantConfigs.LogPushRequestStreams(userID)
	}

	for i := range req.Streams {
		s := req.Streams[i]
		pushStats.StreamLabelsSize += int64(len(s.Labels))

		lbs, err := syntax.ParseLabels(s.Labels)
		if err != nil {
			return nil, nil, fmt.Errorf("couldn't parse labels: %w", err)
		}

		// Check if this is an aggregated metric or pattern stream
		if lbs.Has(constants.AggregatedMetricLabel) || lbs.Has(constants.PatternLabel) {
			pushStats.IsInternalStream = true
		}

		var beforeServiceName string
		if logServiceNameDiscovery {
			beforeServiceName = lbs.String()
		}

		serviceName := ServiceUnknown
		if !lbs.Has(LabelServiceName) && len(discoverServiceName) > 0 && !pushStats.IsInternalStream {
			for _, labelName := range discoverServiceName {
				if labelVal := lbs.Get(labelName); labelVal != "" {
					serviceName = labelVal
					break
				}
			}

			lb := labels.NewBuilder(lbs)
			lbs = lb.Set(LabelServiceName, serviceName).Labels()
			s.Labels = lbs.String()
		}

		if logServiceNameDiscovery {
			level.Debug(logger).Log(
				"msg", "push request stream before service name discovery",
				"labels", beforeServiceName,
				"service_name", serviceName,
			)
		}

		var totalBytesReceived int64
		var retentionPeriod time.Duration
		var policy string
		if streamResolver != nil {
			retentionPeriod = streamResolver.RetentionPeriodFor(lbs)
			policy = streamResolver.PolicyFor(lbs)
		}

		if _, ok := pushStats.LogLinesBytes[policy]; !ok {
			pushStats.LogLinesBytes[policy] = make(map[time.Duration]int64)
		}
		if _, ok := pushStats.StructuredMetadataBytes[policy]; !ok {
			pushStats.StructuredMetadataBytes[policy] = make(map[time.Duration]int64)
		}

		// These two variables are used to track the most recent entry timestamp and the size of the stream.
		// They are only used when logPushRequestStreams is true.
		mostRecentEntryTimestamp := time.Time{}
		streamSizeBytes := int64(0)
		for _, e := range s.Entries {
			pushStats.PolicyNumLines[policy]++
			entryLabelsSize := int64(util.StructuredMetadataSize(e.StructuredMetadata))
			pushStats.LogLinesBytes[policy][retentionPeriod] += int64(len(e.Line))
			streamSizeBytes += int64(len(e.Line)) + entryLabelsSize
			pushStats.StructuredMetadataBytes[policy][retentionPeriod] += entryLabelsSize
			totalBytesReceived += int64(len(e.Line))
			totalBytesReceived += entryLabelsSize

			if e.Timestamp.After(pushStats.MostRecentEntryTimestamp) {
				pushStats.MostRecentEntryTimestamp = e.Timestamp
			}

			if e.Timestamp.After(mostRecentEntryTimestamp) {
				mostRecentEntryTimestamp = e.Timestamp
			}
		}

		// Only populate this map if we are going to log it.
		if logPushRequestStreams {
			pushStats.MostRecentEntryTimestampPerStream[s.Labels] = mostRecentEntryTimestamp
			pushStats.StreamSizeBytes[s.Labels] = streamSizeBytes
		}

		if tracker != nil && !pushStats.IsInternalStream {
			tracker.ReceivedBytesAdd(r.Context(), userID, retentionPeriod, lbs, float64(totalBytesReceived), "loki")
		}

		req.Streams[i] = s
	}

	return &req, pushStats, nil
}

func RetentionPeriodToString(retentionPeriod time.Duration) string {
	if retentionPeriod <= 0 {
		return ""
	}
	return strconv.FormatInt(int64(retentionPeriod/time.Hour), 10)
}

// OTLPError writes an OTLP-compliant error response to the given http.ResponseWriter.
//
// According to the OTLP spec: https://opentelemetry.io/docs/specs/otlp/#failures-1
// Re. the error response format
// > If the processing of the request fails, the server MUST respond with appropriate HTTP 4xx or HTTP 5xx status code.
// > The response body for all HTTP 4xx and HTTP 5xx responses MUST be a Protobuf-encoded Status message that describes the problem.
// > This specification does not use Status.code field and the server MAY omit Status.code field.
// > The clients are not expected to alter their behavior based on Status.code field but MAY record it for troubleshooting purposes.
// > The Status.message field SHOULD contain a developer-facing error message as defined in Status message schema.
//
// Re. retryable errors
// > The requests that receive a response status code listed in following table SHOULD be retried.
// > All other 4xx or 5xx response status codes MUST NOT be retried
// > 429 Too Many Requests
// > 502 Bad Gateway
// > 503 Service Unavailable
// > 504 Gateway Timeout
// In loki, we expect clients to retry on 500 errors, so we map 500 errors to 503.
func OTLPError(w http.ResponseWriter, errorStr string, code int, logger log.Logger) {
	// Map 500 errors to 503. 500 errors are never retried on the client side, but 503 are.
	if code == http.StatusInternalServerError {
		code = http.StatusServiceUnavailable
	}

	// As per the OTLP spec, we send the status code on the http header.
	w.WriteHeader(code)

	// Status 0 because we omit the Status.code field.
	status := grpcstatus.New(0, errorStr).Proto()
	respBytes, err := proto.Marshal(status)
	if err != nil {
		level.Error(logger).Log("msg", "failed to marshal error response", "error", err)
		writeResponseFailedBody, _ := proto.Marshal(grpcstatus.New(
			codes.Internal,
			fmt.Sprintf("failed to marshal error response: %s", err.Error()),
		).Proto())
		_, _ = w.Write(writeResponseFailedBody)
		return
	}

	w.Header().Set(contentType, "application/octet-stream")
	if _, err = w.Write(respBytes); err != nil {
		level.Error(logger).Log("msg", "failed to write error response", "error", err)
		writeResponseFailedBody, _ := proto.Marshal(grpcstatus.New(
			codes.Internal,
			fmt.Sprintf("failed write error: %s", err.Error()),
		).Proto())
		_, _ = w.Write(writeResponseFailedBody)
	}
}

var _ ErrorWriter = OTLPError

func HTTPError(w http.ResponseWriter, errorStr string, code int, _ log.Logger) {
	http.Error(w, errorStr, code)
}

var _ ErrorWriter = HTTPError
