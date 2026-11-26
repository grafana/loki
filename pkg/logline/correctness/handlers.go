package correctness

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/server"
)

// RegisterHandlers registers HTTP handlers for the correctness target.
func RegisterHandlers(srv *server.Server, svc *Service, logger log.Logger) {
	level.Info(logger).Log("msg", "registering correctness handlers")
	srv.HTTP.HandleFunc("/correctness/debug", svc.handleDebug)
}
