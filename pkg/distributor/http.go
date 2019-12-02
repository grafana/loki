package distributor

import (
	"math"
	"net/http"

	"github.com/weaveworks/common/httpgrpc"

	"github.com/cortexproject/cortex/pkg/util"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/unmarshal"
	unmarshal_legacy "github.com/grafana/loki/pkg/logql/unmarshal/legacy"
)

var contentType = http.CanonicalHeaderKey("Content-Type")

const applicationJSON = "application/json"

// PushHandler reads a snappy-compressed proto from the HTTP body.
func (d *Distributor) PushHandler(w http.ResponseWriter, r *http.Request) {
	var req logproto.PushRequest

	switch r.Header.Get(contentType) {
	case applicationJSON:
		var err error

		if loghttp.GetVersion(r.RequestURI) == loghttp.VersionV1 {
			err = unmarshal.DecodePushRequest(r.Body, &req)
		} else {
			err = unmarshal_legacy.DecodePushRequest(r.Body, &req)
		}

		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

	default:
		if _, err := util.ParseProtoReader(r.Context(), r.Body, int(r.ContentLength), math.MaxInt32, &req, util.RawSnappy); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	_, err := d.Push(r.Context(), &req)
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
