package frontend

import (
	"context"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

type limitsClient interface {
	// ExceedsLimits checks if the streams in the request have exceeded their
	// per-partition limits.
	ExceedsLimits(context.Context, *proto.ExceedsLimitsRequest) ([]*proto.ExceedsLimitsResponse, error)

	// UpdateRate updates the rate of streams.
	UpdateRate(context.Context, *proto.UpdateRateRequest) ([]*proto.UpdateRateResponse, error)
}
