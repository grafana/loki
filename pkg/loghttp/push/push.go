package push

import (
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"strconv"
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
	}, []string{"tenant", "retention_hours", "aggregated_metric", "policy"})

	structuredMetadataBytesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "distributor_structured_metadata_bytes_received_total",
		Help:      "The total number of uncompressed bytes received per tenant for entries' structured metadata",
	}, []string{"tenant", "retention_hours", "aggregated_metric", "policy"})
	linesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "distributor_lines_received_total",
		Help:      "The total number of lines received per tenant",
	}, []string{"tenant", "aggregated_metric", "policy"})

	bytesReceivedStats                   = analytics.NewCounter("distributor_bytes_received")
	structuredMetadataBytesReceivedStats = analytics.NewCounter("distributor_structured_metadata_bytes_received")
	linesReceivedStats                   = analytics.NewCounter("distributor_lines_received")
)

const (
	applicationJSON       = "application/json"
	LabelServiceName      = "service_name"
	ServiceUnknown        = "unknown_service"
	AggregatedMetricLabel = "__aggregated_metric__"
)

var ErrAllLogsFiltered = errors.New("all logs lines filtered during parsing")

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
	RequestParser        func(userID string, r *http.Request, limits Limits, tracker UsageTracker, streamResolver StreamResolver, logPushRequestStreams bool, logger log.Logger) (*logproto.PushRequest, *Stats, error)
	RequestParserWrapper func(inner RequestParser) RequestParser
	ErrorWriter          func(w http.ResponseWriter, errorStr string, code int, logger log.Logger)
)

type PolicyWithRetentionWithBytes map[string]map[time.Duration]int64

func NewPushStats() *Stats {
	return &Stats{
		LogLinesBytes:                   map[string]map[time.Duration]int64{},
		StructuredMetadataBytes:         map[string]map[time.Duration]int64{},
		PolicyNumLines:                  map[string]int64{},
		ResourceAndSourceMetadataLabels: map[string]map[time.Duration]push.LabelsAdapter{},
	}
}

type Stats struct {
	Errs                            []error
	PolicyNumLines                  map[string]int64
	LogLinesBytes                   PolicyWithRetentionWithBytes
	StructuredMetadataBytes         PolicyWithRetentionWithBytes
	ResourceAndSourceMetadataLabels map[string]map[time.Duration]push.LabelsAdapter
	StreamLabelsSize                int64
	MostRecentEntryTimestamp        time.Time
	ContentType                     string
	ContentEncoding                 string

	BodySize int64
	// Extra is a place for a wrapped perser to record any interesting stats as key-value pairs to be logged
	Extra []any

	IsAggregatedMetric bool
}

func ParseRequest(logger log.Logger, userID string, r *http.Request, limits Limits, pushRequestParser RequestParser, tracker UsageTracker, streamResolver StreamResolver, logPushRequestStreams bool) (*logproto.PushRequest, error) {
	req, pushStats, err := pushRequestParser(userID, r, limits, tracker, streamResolver, logPushRequestStreams, logger)
	if err != nil && !errors.Is(err, ErrAllLogsFiltered) {
		return nil, err
	}

	var (
		entriesSize            int64
		structuredMetadataSize int64
	)

	isAggregatedMetric := fmt.Sprintf("%t", pushStats.IsAggregatedMetric)

	for policyName, retentionToSizeMapping := range pushStats.LogLinesBytes {
		for retentionPeriod, size := range retentionToSizeMapping {
			retentionHours := RetentionPeriodToString(retentionPeriod)
			bytesIngested.WithLabelValues(userID, retentionHours, isAggregatedMetric, policyName).Add(float64(size))
			bytesReceivedStats.Inc(size)
			entriesSize += size
		}
	}

	for policyName, retentionToSizeMapping := range pushStats.StructuredMetadataBytes {
		for retentionPeriod, size := range retentionToSizeMapping {
			retentionHours := RetentionPeriodToString(retentionPeriod)

			structuredMetadataBytesIngested.WithLabelValues(userID, retentionHours, isAggregatedMetric, policyName).Add(float64(size))
			bytesIngested.WithLabelValues(userID, retentionHours, isAggregatedMetric, policyName).Add(float64(size))
			bytesReceivedStats.Inc(size)
			structuredMetadataBytesReceivedStats.Inc(size)

			entriesSize += size
			structuredMetadataSize += size
		}
	}

	var totalNumLines int64
	// incrementing tenant metrics if we have a tenant.
	for policy, numLines := range pushStats.PolicyNumLines {
		if numLines != 0 && userID != "" {
			linesIngested.WithLabelValues(userID, isAggregatedMetric, policy).Add(float64(numLines))
		}
		totalNumLines += numLines
	}
	linesReceivedStats.Inc(totalNumLines)

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
		"mostRecentLagMs", time.Since(pushStats.MostRecentEntryTimestamp).Milliseconds(),
	}
	logValues = append(logValues, pushStats.Extra...)
	level.Debug(logger).Log(logValues...)

	return req, err
}

func ParseLokiRequest(userID string, r *http.Request, limits Limits, tracker UsageTracker, streamResolver StreamResolver, logPushRequestStreams bool, logger log.Logger) (*logproto.PushRequest, *Stats, error) {
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
		if err := util.ParseProtoReader(r.Context(), body, int(r.ContentLength), math.MaxInt32, &req, util.RawSnappy); err != nil {
			return nil, nil, err
		}
	}

	pushStats.BodySize = bodySize.Size()
	pushStats.ContentType = contentType
	pushStats.ContentEncoding = contentEncoding

	discoverServiceName := limits.DiscoverServiceName(userID)
	for i := range req.Streams {
		s := req.Streams[i]
		pushStats.StreamLabelsSize += int64(len(s.Labels))

		lbs, err := syntax.ParseLabels(s.Labels)
		if err != nil {
			return nil, nil, fmt.Errorf("couldn't parse labels: %w", err)
		}

		if lbs.Has(AggregatedMetricLabel) {
			pushStats.IsAggregatedMetric = true
		}

		var beforeServiceName string
		if logPushRequestStreams {
			beforeServiceName = lbs.String()
		}

		serviceName := ServiceUnknown
		if !lbs.Has(LabelServiceName) && len(discoverServiceName) > 0 && !pushStats.IsAggregatedMetric {
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

		if logPushRequestStreams {
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

		for _, e := range s.Entries {
			pushStats.PolicyNumLines[policy]++
			entryLabelsSize := int64(util.StructuredMetadataSize(e.StructuredMetadata))
			pushStats.LogLinesBytes[policy][retentionPeriod] += int64(len(e.Line))
			pushStats.StructuredMetadataBytes[policy][retentionPeriod] += entryLabelsSize
			totalBytesReceived += int64(len(e.Line))
			totalBytesReceived += entryLabelsSize

			if e.Timestamp.After(pushStats.MostRecentEntryTimestamp) {
				pushStats.MostRecentEntryTimestamp = e.Timestamp
			}
		}

		if tracker != nil {
			tracker.ReceivedBytesAdd(r.Context(), userID, retentionPeriod, lbs, float64(totalBytesReceived))
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
