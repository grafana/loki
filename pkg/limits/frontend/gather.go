package frontend

import (
	"context"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

type exceedsLimitsGatherer interface {
	// exceedsLimits checks if the streams in the request have exceeded their
	// per-partition limits. It returns more than one response when the
	// requested streams are sharded over two or more limits instances.
	exceedsLimits(context.Context, *proto.ExceedsLimitsRequest) ([]*proto.ExceedsLimitsResponse, error)
}
