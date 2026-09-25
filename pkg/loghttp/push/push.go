package push

import (
	"compress/flate"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/gogo/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/loghttp/push/otlpattrs"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/unmarshal"
	unmarshal2 "github.com/grafana/loki/v3/pkg/util/unmarshal/legacy"
)

var (
	contentType = http.CanonicalHeaderKey("Content-Type")
	contentEnc  = http.CanonicalHeaderKey("Content-Encoding")

	otlpExporterStreams = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "distributor_otlp_exporter_streams_total",
		Help:      "The total number of streams with exporter=OTLP label",
	}, []string{"tenant"})
)

const (
	applicationJSON  = "application/json"
	LabelServiceName = "service_name"
	ServiceUnknown   = "unknown_service"

	// maxStreamLabelsSize is the maximum allowed size of a single stream's labels string.
	// Prometheus' label parser panics when encoding labels that exceed 16MB (2^24 bytes).
	// We check the total labels string size per stream before parsing to prevent this panic.
	// See: https://github.com/prometheus/prometheus/issues/17993
	maxStreamLabelsSize = 1 << 24 // 16MB
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
	MaxPushSize(userID string) int
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
// The values returned by the resolver do not change during the lifetime of the request.
type StreamResolver interface {
	RetentionPeriodFor(lbs labels.Labels) time.Duration
	RetentionHoursFor(lbs labels.Labels) string
	PolicyFor(ctx context.Context, lbs labels.Labels) string
}

type (
	RequestParser func(userID string, r *http.Request, limits Limits, tenantConfigs *runtime.TenantConfigs, maxRecvMsgSize int, maxDecompressedSize int64, tracker UsageTracker, streamResolver StreamResolver, logger log.Logger) (*logproto.PushRequest, *Stats, error)
	ErrorWriter   func(w http.ResponseWriter, errorStr string, code int, logger log.Logger)
)

type PolicyWithRetentionWithBytes map[string]map[time.Duration]int64

func NewPushStats() *Stats {
	return &Stats{
		LogLinesBytes:                     map[string]map[time.Duration]int64{},
		StructuredMetadataBytes:           map[string]map[time.Duration]int64{},
		PolicyNumLines:                    map[string]int64{},
		MostRecentEntryTimestampPerStream: map[string]time.Time{},
		StreamSizeBytes:                   map[string]int64{},
	}
}

type Stats struct {
	Errs           []error
	PolicyNumLines map[string]int64

	// LogLinesBytes holds the total size of all log lines, per policy per retention. Used in billing.
	LogLinesBytes PolicyWithRetentionWithBytes

	// StructuredMetadataBytes holds the size of the original structured metadata (but after it was enriched by OLTP
	// parser) per policy per retention. Used in billing.
	StructuredMetadataBytes PolicyWithRetentionWithBytes

	// StreamLabelsSize holds the total size of stream labels after sanitization (empty labels removed and
	// non-meaningful whitespaces removed). Not used in billing.
	StreamLabelsSize int64

	MostRecentEntryTimestamp          time.Time
	MostRecentEntryTimestampPerStream map[string]time.Time

	// StreamSizeBytes holds the total size of log lines and structured metadata. Is used only when logPushRequestStreams is true.
	StreamSizeBytes map[string]int64

	HashOfAllStreams uint64
	ContentType      string // application/json, application/x-protobuf
	ContentEncoding  string // snappy, gzip, deflate
	ContentVersion   string // v1 for /loki/api/v1/push, v0 for /prom/api/push

	BodySize int64
	// Extra is a place for a wrapped parser to record any interesting stats as key-value pairs to be logged
	Extra []any

	HasInternalStreams bool // True if any of the streams has aggregated metrics or is a pattern stream

	// TotalExpandedEntriesSize is the total size of all entries including the size of resource and scope
	// attributes that are copied into the entry's structured metadata.
	// This is the actual size of data that is being ingested and stored in Loki.
	// For non-OTLP requests, TotalExpandedEntriesSize should be the same as the total size of LogLinesBytes and StructuredMetadataBytes.
	TotalExpandedEntriesSize int64

	// OTLPAttributes breaks TotalExpandedEntriesSize down per resource and scope attribute.
	// Is only populated for OTLP requests when logOTLPAttributeExpansion is true.
	OTLPAttributes *otlpattrs.Accumulator
}

// parsePushRequestBody returns logproto.PushRequest from http.Request body, deserialized according to specified content type.
// It also modifies pushStats.
func parsePushRequestBody(r *http.Request, maxRecvMsgSize int, maxDecompressedSize int64, pushStats *Stats) (*logproto.PushRequest, error) {
	// Body
	var body io.Reader
	// bodySize should always reflect the compressed size of the request body
	bodySizeReader := util.NewSizeReader(r.Body)
	// decompressedSize reflects the decompressed size of the request body. It stays
	// nil when the body is not decompressed here, either because it is uncompressed
	// or because ParseProtoReaderWithLimits does the snappy-decoding (and its own
	// decompressed size check) below.
	var decompressedSizeReader util.SizeReader

	// Apply compressed size limit
	body = bodySizeReader
	if maxRecvMsgSize > 0 {
		body = io.LimitReader(body, int64(maxRecvMsgSize)+1)
	}

	contentEncoding := r.Header.Get(contentEnc)
	switch contentEncoding {
	case "":
	case "snappy":
		// Snappy-decoding is done by `util.ParseProtoReaderWithLimits(..., util.RawSnappy)` below.
		// Pass on body bytes. Note: HTTP clients do not need to set this header,
		// but they sometimes do. See #3407.
	case "gzip":
		gzipReader, err := gzip.NewReader(body)
		if err != nil {
			return nil, err
		}
		defer func(gzipReader *gzip.Reader) {
			_ = gzipReader.Close()
		}(gzipReader)
		decompressedSizeReader = util.NewSizeReader(gzipReader)
		body = decompressedSizeReader
		if maxDecompressedSize > 0 {
			body = io.LimitReader(body, maxDecompressedSize+1)
		}
	case "deflate":
		flateReader := flate.NewReader(body)
		defer func(flateReader io.ReadCloser) {
			_ = flateReader.Close()
		}(flateReader)
		decompressedSizeReader = util.NewSizeReader(flateReader)
		body = decompressedSizeReader
		if maxDecompressedSize > 0 {
			body = io.LimitReader(body, maxDecompressedSize+1)
		}
	default:
		return nil, fmt.Errorf("Content-Encoding %q not supported", contentEncoding)
	}

	contentType := r.Header.Get(contentType)
	var req logproto.PushRequest

	contentType, _ /* params */, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, err
	}

	switch contentType {
	case applicationJSON:

		var err error

		// todo once https://github.com/weaveworks/common/commit/73225442af7da93ec8f6a6e2f7c8aafaee3f8840 is in Loki.
		// We can try to pass the body as bytes.buffer instead to avoid reading into another buffer.
		if loghttp.GetVersion(r.RequestURI) == loghttp.VersionV1 {
			err = unmarshal.DecodePushRequest(body, &req)
			pushStats.ContentVersion = "v1"
		} else {
			err = unmarshal2.DecodePushRequest(body, &req)
			pushStats.ContentVersion = "v0"
		}

		if err != nil {
			// The readers above are limited to max+1 bytes, so an oversized body is
			// truncated and fails to decode. Report that as a size error rather than
			// as a malformed request.
			if sizeErr := checkSizeLimits(bodySizeReader, decompressedSizeReader, maxRecvMsgSize, maxDecompressedSize); sizeErr != nil {
				return nil, sizeErr
			}
			return nil, err
		}

	default:
		// When no content-type header is set or when it is set to
		// `application/x-protobuf`: expect snappy compression.
		if err := util.ParseProtoReaderWithLimits(r.Context(), body, int(r.ContentLength), maxRecvMsgSize, maxDecompressedSize, &req, util.RawSnappy); err != nil {
			return nil, err
		}
	}

	pushStats.BodySize = bodySizeReader.Size()
	pushStats.ContentType = contentType
	pushStats.ContentEncoding = contentEncoding

	if err := checkSizeLimits(bodySizeReader, decompressedSizeReader, maxRecvMsgSize, maxDecompressedSize); err != nil {
		return nil, err
	}
	return &req, nil
}

// checkSizeLimits reports whether the request body exceeded the compressed or the
// decompressed size limit. The readers wrapping the body are limited to max+1 bytes,
// so a size greater than max means the body was truncated. decompressedSize may be
// nil, in which case only the compressed size is checked.
func checkSizeLimits(bodySizeReader, decompressedSizeReader util.SizeReader, maxRecvMsgSize int, maxDecompressedSize int64) error {
	if size := bodySizeReader.Size(); maxRecvMsgSize > 0 && size > int64(maxRecvMsgSize) {
		return fmt.Errorf(messageSizeLargerErrFmt, util.ErrMessageSizeTooLarge, size, maxRecvMsgSize)
	}
	if decompressedSizeReader != nil {
		if size := decompressedSizeReader.Size(); maxDecompressedSize > 0 && size > maxDecompressedSize {
			return fmt.Errorf(messageSizeLargerErrFmt, util.ErrMessageDecompressedSizeTooLarge, size, maxDecompressedSize)
		}
	}
	return nil
}

func ParseLokiRequest(userID string, r *http.Request, limits Limits, tenantConfigs *runtime.TenantConfigs, maxRecvMsgSize int, maxDecompressedSize int64, tracker UsageTracker, streamResolver StreamResolver, logger log.Logger) (*logproto.PushRequest, *Stats, error) {
	pushStats := NewPushStats()

	req, err := parsePushRequestBody(r, maxRecvMsgSize, maxDecompressedSize, pushStats)
	if err != nil {
		return nil, nil, err
	}

	discoverServiceName := limits.DiscoverServiceName(userID)

	logServiceNameDiscovery := false
	if tenantConfigs != nil {
		logServiceNameDiscovery = tenantConfigs.LogServiceNameDiscovery(userID)
	}

	// If this is a backfill push (X-Loki-Backfill-Shard header), every stream gets the internal
	// backfill labels added below.
	backfillShard := ExtractBackfillShardContext(r.Context())

	for i := range req.Streams {
		s := req.Streams[i]

		if len(s.Labels) > maxStreamLabelsSize {
			return nil, nil, fmt.Errorf("%w: stream labels size %s exceeds limit of %s", ErrRequestBodyTooLarge, humanize.Bytes(uint64(len(s.Labels))), humanize.Bytes(maxStreamLabelsSize))
		}

		lbs, err := syntax.ParseLabels(s.Labels)
		if err != nil {
			return nil, nil, fmt.Errorf("couldn't parse labels: %w", err)
		}

		// The backfill labels are reserved for Loki: they may only be added below, from the
		// X-Loki-Backfill-Shard header, so clients cannot spoof them to bypass validation.
		if lbs.Has(constants.BackfillLabel) || lbs.Has(constants.BackfillShardLabel) {
			return nil, nil, errReservedBackfillLabels()
		}

		// Check if this is an aggregated metric or pattern stream
		isInternalStream := false
		if lbs.Has(constants.AggregatedMetricLabel) || lbs.Has(constants.PatternLabel) {
			pushStats.HasInternalStreams = true
			isInternalStream = true
		}

		var beforeServiceName string
		if logServiceNameDiscovery {
			beforeServiceName = lbs.String()
		}

		serviceName := ServiceUnknown
		if !lbs.Has(LabelServiceName) && len(discoverServiceName) > 0 && !isInternalStream {
			for _, labelName := range discoverServiceName {
				if labelVal := lbs.Get(labelName); labelVal != "" {
					serviceName = labelVal
					break
				}
			}

			lb := labels.NewBuilder(lbs)
			lbs = lb.Set(LabelServiceName, serviceName).Labels()
		}

		if backfillShard != "" {
			lbs = labels.NewBuilder(lbs).
				Set(constants.BackfillLabel, "true").
				Set(constants.BackfillShardLabel, backfillShard).
				Labels()
		}

		// Update labels. They were sanitized and potentially with the added service_name label.
		s.Labels = lbs.String()

		if logServiceNameDiscovery {
			level.Debug(logger).Log(
				"msg", "push request stream before service name discovery",
				"labels", beforeServiceName,
				"service_name", serviceName,
			)
		}

		if tracker != nil && !isInternalStream {
			var retentionPeriod time.Duration
			if streamResolver != nil {
				retentionPeriod = streamResolver.RetentionPeriodFor(lbs)
			}
			var totalBytesReceived = int64(util.EntriesTotalSize(s.Entries))
			tracker.ReceivedBytesAdd(r.Context(), userID, retentionPeriod, lbs, float64(totalBytesReceived), "loki")
		}

		req.Streams[i] = s
	}

	err = CalculateStreamsStats(r.Context(), userID, req, streamResolver, tenantConfigs, pushStats)
	if err != nil {
		return nil, nil, err
	}

	return req, pushStats, nil
}

// CalculateStreamsStats modifies pushStats with statistics about all the streams from req.
func CalculateStreamsStats(ctx context.Context, userID string, req *logproto.PushRequest, streamResolver StreamResolver, tenantConfigs *runtime.TenantConfigs, pushStats *Stats) error {
	logPushRequestStreams := false
	if tenantConfigs != nil {
		logPushRequestStreams = tenantConfigs.LogPushRequestStreams(userID)
	}

	for _, s := range req.Streams {
		// Record the new size of labels
		pushStats.StreamLabelsSize += int64(len(s.Labels))

		lbs, err := syntax.ParseLabels(s.Labels)
		if err != nil {
			return fmt.Errorf("couldn't parse labels: %w", err)
		}

		var retentionPeriod time.Duration
		var policy string
		if streamResolver != nil {
			retentionPeriod = streamResolver.RetentionPeriodFor(lbs)
			policy = streamResolver.PolicyFor(ctx, lbs)
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
			entryTotal := int64(util.EntryTotalSize(&e))
			streamSizeBytes += entryTotal
			pushStats.TotalExpandedEntriesSize += entryTotal
			pushStats.StructuredMetadataBytes[policy][retentionPeriod] += entryLabelsSize

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
	}

	return nil
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
