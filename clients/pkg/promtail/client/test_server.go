package client

import (
	"math"
	"net/http"
	"net/http/httptest"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util"
)

type receivedReq struct {
	tenantID string
	pushReq  logproto.PushRequest
}

// newTestMoreWriteServer creates a new httpserver.Server that can handle remote write request. When a request is handled,
// the received entries are written to receivedChan, and status is responded.
func newTestRemoteWriteServer(receivedChan chan receivedReq, status int) *httptest.Server {
	server := httptest.NewServer(createServerHandler(receivedChan, status))
	return server
}

func createServerHandler(receivedReqsChan chan receivedReq, receivedOKStatus int) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		// Parse the request
		var pushReq logproto.PushRequest
		if err := util.ParseProtoReader(req.Context(), req.Body, int(req.ContentLength), math.MaxInt32, &pushReq, util.RawSnappy); err != nil {
			rw.WriteHeader(500)
			return
		}

		receivedReqsChan <- receivedReq{
			tenantID: req.Header.Get("X-Scope-OrgID"),
			pushReq:  pushReq,
		}

		rw.WriteHeader(receivedOKStatus)
	}
}
