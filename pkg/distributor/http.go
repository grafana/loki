package distributor

import (
	"math"
	"net/http"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/dustin/go-humanize"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/unmarshal"
	unmarshal_legacy "github.com/grafana/loki/pkg/logql/unmarshal/legacy"
	lokiutil "github.com/grafana/loki/pkg/util"
)

var (
	contentType = http.CanonicalHeaderKey("Content-Type")

	bytesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "distributor_bytes_received_total",
		Help:      "The total number of uncompressed bytes received per tenant",
	}, []string{"tenant"})
	linesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "distributor_lines_received_total",
		Help:      "The total number of lines received per tenant",
	}, []string{"tenant"})
)

const applicationJSON = "application/json"

// PushHandler reads a snappy-compressed proto from the HTTP body.
func (d *Distributor) PushHandler(w http.ResponseWriter, r *http.Request) {
	req, err := ParseRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err = d.Push(r.Context(), req)
	if err == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	resp, ok := httpgrpc.HTTPResponseFromError(err)
	if ok {
		http.Error(w, string(resp.Body), int(resp.Code))
	} else {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func ParseRequest(r *http.Request) (*logproto.PushRequest, error) {
	userID, _ := user.ExtractOrgID(r.Context())
	logger := util_log.WithContext(r.Context(), util_log.Logger)
	body := lokiutil.NewSizeReader(r.Body)
	contentType := r.Header.Get(contentType)
	var req logproto.PushRequest

	defer func() {
		var (
			entriesSize      int64
			streamLabelsSize int64
			totalEntries     int64
		)

		mostRecentEntry := time.Unix(0, 0)

		for _, s := range req.Streams {
			streamLabelsSize += int64(len(s.Labels))
			for _, e := range s.Entries {
				totalEntries++
				entriesSize += int64(len(e.Line))
				if e.Timestamp.After(mostRecentEntry) {
					mostRecentEntry = e.Timestamp
				}
			}
		}

		// incrementing tenant metrics if we have a tenant.
		if totalEntries != 0 && userID != "" {
			bytesIngested.WithLabelValues(userID).Add(float64(entriesSize))
			linesIngested.WithLabelValues(userID).Add(float64(totalEntries))
		}

		level.Debug(logger).Log(
			"msg", "push request parsed",
			"path", r.URL.Path,
			"contentType", contentType,
			"bodySize", humanize.Bytes(uint64(body.Size())),
			"streams", len(req.Streams),
			"entries", totalEntries,
			"streamLabelsSize", humanize.Bytes(uint64(streamLabelsSize)),
			"entriesSize", humanize.Bytes(uint64(entriesSize)),
			"totalSize", humanize.Bytes(uint64(entriesSize+streamLabelsSize)),
			"mostRecentLagMs", time.Since(mostRecentEntry).Milliseconds(),
		)
	}()

	switch contentType {
	case applicationJSON:
		var err error

		// todo once https://github.com/weaveworks/common/commit/73225442af7da93ec8f6a6e2f7c8aafaee3f8840 is in Loki.
		// We can try to pass the body as bytes.buffer instead to avoid reading into another buffer.
		if loghttp.GetVersion(r.RequestURI) == loghttp.VersionV1 {
			err = unmarshal.DecodePushRequest(body, &req)
		} else {
			err = unmarshal_legacy.DecodePushRequest(body, &req)
		}

		if err != nil {
			return nil, err
		}

	default:
		if err := util.ParseProtoReader(r.Context(), body, int(r.ContentLength), math.MaxInt32, &req, util.RawSnappy); err != nil {
			return nil, err
		}
	}
	return &req, nil
}
