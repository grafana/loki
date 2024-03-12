package push

import (
	"compress/flate"
	"compress/gzip"
	"fmt"
	"github.com/go-kit/log/level"
	"io"
	"math"
	"mime"
	"net/http"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/analytics"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/util"
	loki_util "github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/constants"
	"github.com/grafana/loki/pkg/util/unmarshal"
	unmarshal2 "github.com/grafana/loki/pkg/util/unmarshal/legacy"
)

var (
	contentType   = http.CanonicalHeaderKey("Content-Type")
	contentEnc    = http.CanonicalHeaderKey("Content-Encoding")
	bytesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "distributor_bytes_received_total",
		Help:      "The total number of uncompressed bytes received per tenant. Includes structured metadata bytes.",
	}, []string{"tenant", "retention_hours"})

	structuredMetadataBytesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "distributor_structured_metadata_bytes_received_total",
		Help:      "The total number of uncompressed bytes received per tenant for entries' structured metadata",
	}, []string{"tenant", "retention_hours"})
	linesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "distributor_lines_received_total",
		Help:      "The total number of lines received per tenant",
	}, []string{"tenant"})

	bytesReceivedStats                   = analytics.NewCounter("distributor_bytes_received")
	structuredMetadataBytesReceivedStats = analytics.NewCounter("distributor_structured_metadata_bytes_received")
	linesReceivedStats                   = analytics.NewCounter("distributor_lines_received")
)

const applicationJSON = "application/json"

type TenantsRetention interface {
	RetentionPeriodFor(userID string, lbs labels.Labels) time.Duration
}

type Limits interface {
	OTLPConfig(userID string) OTLPConfig
}

type EmptyLimits struct{}

func (EmptyLimits) OTLPConfig(string) OTLPConfig {
	return DefaultOTLPConfig(GlobalOTLPConfig{})
}

type RequestParser func(userID string, r *http.Request, tenantsRetention TenantsRetention, limits Limits, tracker UsageTracker) (*logproto.PushRequest, *Stats, error)
type RequestParserWrapper func(inner RequestParser) RequestParser

type Stats struct {
	Errs                     []error
	NumLines                 int64
	LogLinesBytes            map[time.Duration]int64
	StructuredMetadataBytes  map[time.Duration]int64
	StreamLabelsSize         int64
	MostRecentEntryTimestamp time.Time
	ContentType              string
	ContentEncoding          string
	BodySize                 int64

	// Extra is a place for a wrapped perser to record any interesting stats as key-value pairs to be logged
	Extra []any
}

func ParseRequest(logger log.Logger, userID string, r *http.Request, tenantsRetention TenantsRetention, limits Limits, pushRequestParser RequestParser, tracker UsageTracker) (*logproto.PushRequest, error) {
	req, pushStats, err := pushRequestParser(userID, r, tenantsRetention, limits, tracker)
	if err != nil {
		return nil, err
	}

	var (
		entriesSize            int64
		structuredMetadataSize int64
	)
	for retentionPeriod, size := range pushStats.LogLinesBytes {
		retentionHours := retentionPeriodToString(retentionPeriod)

		bytesIngested.WithLabelValues(userID, retentionHours).Add(float64(size))
		bytesReceivedStats.Inc(size)
		entriesSize += size
	}

	for retentionPeriod, size := range pushStats.StructuredMetadataBytes {
		retentionHours := retentionPeriodToString(retentionPeriod)

		structuredMetadataBytesIngested.WithLabelValues(userID, retentionHours).Add(float64(size))
		bytesIngested.WithLabelValues(userID, retentionHours).Add(float64(size))
		bytesReceivedStats.Inc(size)
		structuredMetadataBytesReceivedStats.Inc(size)

		entriesSize += size
		structuredMetadataSize += size
	}

	// incrementing tenant metrics if we have a tenant.
	if pushStats.NumLines != 0 && userID != "" {
		linesIngested.WithLabelValues(userID).Add(float64(pushStats.NumLines))
	}
	linesReceivedStats.Inc(pushStats.NumLines)

	logValues := []interface{}{
		"msg", "push request parsed",
		"path", r.URL.Path,
		"contentType", pushStats.ContentType,
		"contentEncoding", pushStats.ContentEncoding,
		"bodySize", humanize.Bytes(uint64(pushStats.BodySize)),
		"streams", len(req.Streams),
		"entries", pushStats.NumLines,
		"streamLabelsSize", humanize.Bytes(uint64(pushStats.StreamLabelsSize)),
		"entriesSize", humanize.Bytes(uint64(entriesSize)),
		"structuredMetadataSize", humanize.Bytes(uint64(structuredMetadataSize)),
		"totalSize", humanize.Bytes(uint64(entriesSize + pushStats.StreamLabelsSize)),
		"mostRecentLagMs", time.Since(pushStats.MostRecentEntryTimestamp).Milliseconds(),
	}
	logValues = append(logValues, pushStats.Extra...)
	level.Debug(logger).Log(logValues...)

	return req, nil
}

func ParseLokiRequest(userID string, r *http.Request, tenantsRetention TenantsRetention, _ Limits, tracker UsageTracker) (*logproto.PushRequest, *Stats, error) {
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
		pushStats = newPushStats()
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

	for _, s := range req.Streams {
		pushStats.StreamLabelsSize += int64(len(s.Labels))

		var lbs labels.Labels
		if tenantsRetention != nil || tracker != nil {
			lbs, err = syntax.ParseLabels(s.Labels)
			if err != nil {
				return nil, nil, fmt.Errorf("couldn't parse labels: %w", err)
			}
		}
		var retentionPeriod time.Duration
		if tenantsRetention != nil {
			retentionPeriod = tenantsRetention.RetentionPeriodFor(userID, lbs)
		}
		for _, e := range s.Entries {
			pushStats.NumLines++
			var entryLabelsSize int64
			for _, l := range e.StructuredMetadata {
				entryLabelsSize += int64(len(l.Name) + len(l.Value))
			}
			pushStats.LogLinesBytes[retentionPeriod] += int64(len(e.Line))
			pushStats.StructuredMetadataBytes[retentionPeriod] += entryLabelsSize

			if tracker != nil {
				tracker.ReceivedBytesAdd(userID, retentionPeriod, lbs, float64(len(e.Line)))
				tracker.ReceivedBytesAdd(userID, retentionPeriod, lbs, float64(entryLabelsSize))
			}

			if e.Timestamp.After(pushStats.MostRecentEntryTimestamp) {
				pushStats.MostRecentEntryTimestamp = e.Timestamp
			}
		}
	}

	return &req, pushStats, nil
}

func retentionPeriodToString(retentionPeriod time.Duration) string {
	var retentionHours string
	if retentionPeriod > 0 {
		retentionHours = fmt.Sprintf("%d", int64(math.Floor(retentionPeriod.Hours())))
	}
	return retentionHours
}
