package middleware

import (
	"context"
	"sync/atomic"

	"github.com/aws/smithy-go/middleware"
)

// AddTimeOffsetMiddleware is deprecated.
//
// Deprecated: handled in retry loop.
type AddTimeOffsetMiddleware struct {
	Offset *atomic.Int64
}

// ID the identifier for AddTimeOffsetMiddleware
func (m *AddTimeOffsetMiddleware) ID() string { return "AddTimeOffsetMiddleware" }

// HandleBuild is a no-op.
func (m AddTimeOffsetMiddleware) HandleBuild(ctx context.Context, in middleware.BuildInput, next middleware.BuildHandler) (
	out middleware.BuildOutput, metadata middleware.Metadata, err error,
) {
	return next.HandleBuild(ctx, in)
}

// HandleDeserialize is a no-op.
func (m *AddTimeOffsetMiddleware) HandleDeserialize(ctx context.Context, in middleware.DeserializeInput, next middleware.DeserializeHandler) (
	out middleware.DeserializeOutput, metadata middleware.Metadata, err error,
) {
	return next.HandleDeserialize(ctx, in)
}
