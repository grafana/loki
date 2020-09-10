package push

import (
	"context"
	"net/http"

	"github.com/go-kit/kit/log/level"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/middleware"

	"github.com/cortexproject/cortex/pkg/distributor"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util"
)

// Handler is a http.Handler which accepts WriteRequests.
func Handler(cfg distributor.Config, sourceIPs *middleware.SourceIPExtractor, push func(context.Context, *client.WriteRequest) (*client.WriteResponse, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger := util.WithContext(ctx, util.Logger)
		if sourceIPs != nil {
			source := sourceIPs.Get(r)
			if source != "" {
				ctx = util.AddSourceIPsToOutgoingContext(ctx, source)
				logger = util.WithSourceIPs(source, logger)
			}
		}
		compressionType := util.CompressionTypeFor(r.Header.Get("X-Prometheus-Remote-Write-Version"))
		var req client.PreallocWriteRequest
		_, err := util.ParseProtoReader(ctx, r.Body, int(r.ContentLength), cfg.MaxRecvMsgSize, &req, compressionType)
		if err != nil {
			level.Error(logger).Log("err", err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Source == 0 {
			req.Source = client.API
		}

		if _, err := push(ctx, &req.WriteRequest); err != nil {
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
