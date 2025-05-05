package frontend

import (
	"context"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type ExceedsLimitsGatherer interface {
	// ExceedsLimits checks if the streams in the request have exceeded their
	// per-partition limits. It returns more than one response when the
	// requested streams are sharded over two or more limits instances.
	ExceedsLimits(context.Context, *logproto.ExceedsLimitsRequest) ([]*logproto.ExceedsLimitsResponse, error)
}
