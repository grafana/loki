package push

import (
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/analytics"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/util"
	loki_util "github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/unmarshal"
	unmarshal2 "github.com/grafana/loki/pkg/util/unmarshal/legacy"
)

var (
	contentType   = http.CanonicalHeaderKey("Content-Type")
	contentEnc    = http.CanonicalHeaderKey("Content-Encoding")
	bytesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "distributor_bytes_received_total",
		Help:      "The total number of uncompressed bytes received per tenant. Includes metadata.",
	}, []string{"tenant", "retention_hours"})
	metadataBytesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "distributor_metadata_bytes_received_total",
		Help:      "The total number of uncompressed bytes received per tenant for entries metadata (non-indexed labels)",
	}, []string{"tenant", "retention_hours"})
	linesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "distributor_lines_received_total",
		Help:      "The total number of lines received per tenant",
	}, []string{"tenant"})

	bytesReceivedStats         = analytics.NewCounter("distributor_bytes_received")
	metadataBytesReceivedStats = analytics.NewCounter("distributor_metadata_bytes_received")
	linesReceivedStats         = analytics.NewCounter("distributor_lines_received")
)

const applicationJSON = "application/json"

type TenantsRetention interface {
	RetentionPeriodFor(userID string, lbs labels.Labels) time.Duration
}

func ParseRequest(logger log.Logger, userID string, r *http.Request, tenantsRetention TenantsRetention) (*logproto.PushRequest, error) {
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
			return nil, err
		}
		defer gzipReader.Close()
		body = gzipReader
	case "deflate":
		flateReader := flate.NewReader(bodySize)
		defer flateReader.Close()
		body = flateReader
	default:
		return nil, fmt.Errorf("Content-Encoding %q not supported", contentEncoding)
	}

	contentType := r.Header.Get(contentType)
	var (
		entriesSize          int64
		nonIndexedLabelsSize int64
		streamLabelsSize     int64
		totalEntries         int64
		req                  logproto.PushRequest
	)

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
		} else {
			err = unmarshal2.DecodePushRequest(body, &req)
		}

		if err != nil {
			return nil, err
		}

	default:
		// When no content-type header is set or when it is set to
		// `application/x-protobuf`: expect snappy compression.
		if err := util.ParseProtoReader(r.Context(), body, int(r.ContentLength), math.MaxInt32, &req, util.RawSnappy); err != nil {
			return nil, err
		}
	}

	mostRecentEntry := time.Unix(0, 0)

	for _, s := range req.Streams {
		streamLabelsSize += int64(len(s.Labels))
		var retentionHours string
		if tenantsRetention != nil {
			lbs, err := syntax.ParseLabels(s.Labels)
			if err != nil {
				return nil, fmt.Errorf("couldn't parse labels: %w", err)
			}
			retentionHours = fmt.Sprintf("%d", int64(math.Floor(tenantsRetention.RetentionPeriodFor(userID, lbs).Hours())))
		}
		for _, e := range s.Entries {
			totalEntries++
			var entryLabelsSize int64
			for _, l := range e.NonIndexedLabels {
				entryLabelsSize += int64(len(l.Name) + len(l.Value))
			}
			entrySize := int64(len(e.Line)) + entryLabelsSize
			entriesSize += entrySize
			nonIndexedLabelsSize += entryLabelsSize
			bytesIngested.WithLabelValues(userID, retentionHours).Add(float64(entrySize))
			metadataBytesIngested.WithLabelValues(userID, retentionHours).Add(float64(entryLabelsSize))
			bytesReceivedStats.Inc(entrySize)
			metadataBytesReceivedStats.Inc(entryLabelsSize)
			if e.Timestamp.After(mostRecentEntry) {
				mostRecentEntry = e.Timestamp
			}
		}
	}

	// incrementing tenant metrics if we have a tenant.
	if totalEntries != 0 && userID != "" {
		linesIngested.WithLabelValues(userID).Add(float64(totalEntries))
	}
	linesReceivedStats.Inc(totalEntries)

	level.Debug(logger).Log(
		"msg", "push request parsed",
		"path", r.URL.Path,
		"contentType", contentType,
		"contentEncoding", contentEncoding,
		"bodySize", humanize.Bytes(uint64(bodySize.Size())),
		"streams", len(req.Streams),
		"entries", totalEntries,
		"streamLabelsSize", humanize.Bytes(uint64(streamLabelsSize)),
		"entriesSize", humanize.Bytes(uint64(entriesSize)),
		"nonIndexedLabelsSize", humanize.Bytes(uint64(nonIndexedLabelsSize)),
		"totalSize", humanize.Bytes(uint64(entriesSize+streamLabelsSize)),
		"mostRecentLagMs", time.Since(mostRecentEntry).Milliseconds(),
	)
	return &req, nil
}
