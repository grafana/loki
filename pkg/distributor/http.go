package distributor

import (
	"net/http"

	"github.com/cortexproject/cortex/pkg/util"

	"github.com/grafana/loki/pkg/logproto"
)

// PushHandler reads a snappy-compressed proto from the HTTP body.
func (d *Distributor) PushHandler(w http.ResponseWriter, r *http.Request) {
	var req logproto.PushRequest
	if _, err := util.ParseProtoReader(r.Context(), r.Body, &req, util.RawSnappy); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err := d.Push(r.Context(), &req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
