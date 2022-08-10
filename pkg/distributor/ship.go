package distributor

import (
	"context"
	"net/http"

	"github.com/grafana/loki/pkg/logproto"

	"github.com/prometheus/prometheus/model/labels"
)

var DefaultLogSender LogSender

//send log to ES or kafka
type LogSender interface {
	Send(ctx context.Context, tenantID string, req *logproto.PushRequest, header http.Header) error
}

var DefaultLogPipeline LogPipeline

//send log to ES or kafka
type LogPipeline interface {
	Pipeline(ctx context.Context, tenantID string, labels string, entry logproto.Entry) (bool, error)
}

var DefaultLabelPipeline LabelPipeline

//label filter base server
type LabelPipeline interface {
	Pipeline(tenantID string, labels labels.Labels) (labels.Labels, error)
}

var DefaultLogShip LogShip

//send log to ES or kafka
type LogShip interface {
	Ship(ctx context.Context, tenantID string, req *logproto.PushRequest) (*logproto.PushRequest, error)
}
