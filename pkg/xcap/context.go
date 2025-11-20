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

// regionFromContext returns the current Region from the context, or nil if no Region
// is present.
func regionFromContext(ctx context.Context) *Region {
	v, ok := ctx.Value(regionKey).(*Region)
	if !ok {
		return nil
	}
	return v
}

// contextWithRegion returns a new context with the given Region.
func contextWithRegion(ctx context.Context, region *Region) context.Context {
	return context.WithValue(ctx, regionKey, region)
}
