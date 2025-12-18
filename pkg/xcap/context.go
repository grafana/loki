package xcap

import (
	"context"
)

type ctxKeyType string

const (
	captureKey ctxKeyType = "capture"
	regionKey  ctxKeyType = "region"
)

// CaptureFromContext returns the Capture from the context, or nil if no Capture
// is present.
func CaptureFromContext(ctx context.Context) *Capture {
	v, ok := ctx.Value(captureKey).(*Capture)
	if !ok {
		return nil
	}
	return v
}

// contextWithCapture returns a new context with the given Capture.
func contextWithCapture(ctx context.Context, capture *Capture) context.Context {
	return context.WithValue(ctx, captureKey, capture)
}

// RegionFromContext returns the current Region from the context, or nil if no Region
// is present.
func RegionFromContext(ctx context.Context) *Region {
	v, ok := ctx.Value(regionKey).(*Region)
	if !ok {
		return nil
	}
	return v
}

// ContextWithRegion returns a new context with the given Region.
func ContextWithRegion(ctx context.Context, region *Region) context.Context {
	return context.WithValue(ctx, regionKey, region)
}
