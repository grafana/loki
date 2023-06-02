package distributor

import (
	"context"
	"net/http"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
)

var DefaultLogSender LogSender

type LogSender interface {
	Send(ctx context.Context, tenantID string, req *logproto.PushRequest, header http.Header) error
}

var DefaultLogPipeline LogPipeline

type LogPipeline interface {
	Pipeline(ctx context.Context, tenantID string, labels string, entry logproto.Entry) (bool, error)
}

var DefaultLabelPipeline LabelPipeline

type LabelPipeline interface {
	Pipeline(tenantID string, labels labels.Labels) (labels.Labels, error)
}

var DefaultLogShip LogShip

type LogShip interface {
	Ship(ctx context.Context, tenantID string, req *logproto.PushRequest) (*logproto.PushRequest, error)
}
