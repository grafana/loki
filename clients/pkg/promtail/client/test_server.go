package client

import (
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util"
	"math"
	"net/http"
	"net/http/httptest"
)

type receivedReq struct {
	tenantID string
	pushReq  logproto.PushRequest
}

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
