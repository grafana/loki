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

	util.Logger.Log("event", "inside push handler")

	req, err := ParseRequest(r)
	if err != nil {
		util.Logger.Log("event", "bad thing happened", "message", err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	util.Logger.Log("event", "pushing request")
	_, err = d.Push(r.Context(), req)
	if err == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	util.Logger.Log("event", "parsing response")
	resp, ok := httpgrpc.HTTPResponseFromError(err)
	if ok {
		util.Logger.Log("event", "ok response", "status", int(resp.Code), "body", string(resp.Body))
		http.Error(w, string(resp.Body), int(resp.Code))
	} else {
		util.Logger.Log("event", "failed response", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func ParseRequest(r *http.Request) (*logproto.PushRequest, error) {
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
			return nil, err
		}

	default:
		if err := util.ParseProtoReader(r.Context(), r.Body, int(r.ContentLength), math.MaxInt32, &req, util.RawSnappy); err != nil {
			return nil, err
		}
	}
	return &req, nil
}
