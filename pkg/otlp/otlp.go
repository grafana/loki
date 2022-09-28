package otlp

import (
	"context"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/multierror"
	adapter "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/translator/loki"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

	"github.com/grafana/loki/pkg/logproto"
	util_log "github.com/grafana/loki/pkg/util/log"
)

var _ plogotlp.Server = (*Server)(nil)

// Server is an OpenTelemetry Collector Logs gRPC server which converts OTLP
// into Loki's PushRequest and forwards the requeste to the provided PusherServer.
type Server struct {
	ps logproto.PusherServer
}

// NewServer returns a new OpenTelemetry Collector Logs gRPC server which will
// use the provided push server as the destination of the received OTLP Logs.
func NewServer(p logproto.PusherServer) *Server {
	return &Server{
		ps: p,
	}
}

// Export receives the OpenTelemetry Collector pipeline data for Logs (pdata.Logs)
// and converts into a PushRequest, calling the underlying logproto.PusherServer
// to do the actual processing. Returns an empty response in all cases.
func (s *Server) Export(ctx context.Context, req plogotlp.Request) (plogotlp.Response, error) {
	resp := plogotlp.NewResponse()
	pushReq, pushRep := adapter.LogsToLoki(req.Logs())
	if len(pushRep.Errors) > 0 {
		if pushRep.NumSubmitted == 0 {
			// if we couldn't process any items, we consider the operation an error
			return resp, multierror.New(pushRep.Errors...).Err()
		} else {
			level.Info(util_log.Logger).Log(
				"msg", "one or more items couldn't be converted from OTLP to Loki",
				"submitted", pushRep.NumSubmitted,
				"dropped", pushRep.NumDropped,
				"errors", multierror.New(pushRep.Errors...).Err(),
			)
		}
	}

	level.Debug(util_log.Logger).Log(
		"msg", "pushing converted OTLP data to the PusherServer",
		"numItems", pushRep.NumSubmitted,
	)

	_, err := s.ps.Push(ctx, pushReq)
	if err != nil {
		return resp, err
	}

	return resp, nil
}
