package push

import (
	"context"
	"net/http"

	"github.com/go-kit/kit/log/level"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/cortexproject/cortex/pkg/distributor"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util"
)

// Handler is a http.Handler which accepts WriteRequests.
func Handler(cfg distributor.Config, push func(context.Context, *client.WriteRequest) (*client.WriteResponse, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		compressionType := util.CompressionTypeFor(r.Header.Get("X-Prometheus-Remote-Write-Version"))
		var req client.PreallocWriteRequest
		_, err := util.ParseProtoReader(r.Context(), r.Body, int(r.ContentLength), cfg.MaxRecvMsgSize, &req, compressionType)
		logger := util.WithContext(r.Context(), util.Logger)
		if err != nil {
			level.Error(logger).Log("err", err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Source == 0 {
			req.Source = client.API
		}

		if _, err := push(r.Context(), &req.WriteRequest); err != nil {
			resp, ok := httpgrpc.HTTPResponseFromError(err)
			if !ok {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if resp.GetCode() != 202 {
				level.Error(logger).Log("msg", "push error", "err", err)
			}
			http.Error(w, string(resp.Body), int(resp.Code))
		}
	})
}
