package push

import (
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
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
		Help:      "The total number of uncompressed bytes received per tenant",
	}, []string{"tenant", "retention_hours"})
	linesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "distributor_lines_received_total",
		Help:      "The total number of lines received per tenant",
	}, []string{"tenant"})
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
	default:
		return nil, fmt.Errorf("Content-Encoding %q not supported", contentEncoding)
	}

	contentType := r.Header.Get(contentType)
	var (
		entriesSize      int64
		streamLabelsSize int64
		totalEntries     int64
		req              logproto.PushRequest
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
			lbs, err := logql.ParseLabels(s.Labels)
			if err != nil {
				return nil, err
			}
			retentionHours = fmt.Sprintf("%d", int64(math.Floor(tenantsRetention.RetentionPeriodFor(userID, lbs).Hours())))
		}
		for _, e := range s.Entries {
			totalEntries++
			entriesSize += int64(len(e.Line))
			bytesIngested.WithLabelValues(userID, retentionHours).Add(float64(int64(len(e.Line))))
			if e.Timestamp.After(mostRecentEntry) {
				mostRecentEntry = e.Timestamp
			}
		}
	}

	// incrementing tenant metrics if we have a tenant.
	if totalEntries != 0 && userID != "" {
		linesIngested.WithLabelValues(userID).Add(float64(totalEntries))
	}

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
		"totalSize", humanize.Bytes(uint64(entriesSize+streamLabelsSize)),
		"mostRecentLagMs", time.Since(mostRecentEntry).Milliseconds(),
	)
	return &req, nil
}
