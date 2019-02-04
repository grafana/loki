package distributor

import (
	"encoding/json"
	"net/http"

	"github.com/weaveworks/common/httpgrpc"

	"github.com/cortexproject/cortex/pkg/util"

	"github.com/grafana/loki/pkg/logproto"
)

var contentType = http.CanonicalHeaderKey("Content-Type")

const applicationJSON = "application/json"

// PushHandler reads a snappy-compressed proto from the HTTP body.
func (d *Distributor) PushHandler(w http.ResponseWriter, r *http.Request) {
	var req logproto.PushRequest

	switch r.Header.Get(contentType) {
	case applicationJSON:
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

	default:
		if _, err := util.ParseProtoReader(r.Context(), r.Body, &req, util.RawSnappy); err != nil {
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
