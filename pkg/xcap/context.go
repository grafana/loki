package xcap

import (
	"context"
)

type ctxKeyType string

const (
	xcapKey ctxKeyType = "xcap"
)

// FromContext returns the Capture from the context, or nil if no Capture
// is present.
func FromContext(ctx context.Context) *Capture {
	v, ok := ctx.Value(xcapKey).(*Capture)
	if !ok {
		return nil
	}
	return v
}

// WithCapture returns a new context with the given Capture.
func WithCapture(ctx context.Context, capture *Capture) context.Context {
	return context.WithValue(ctx, xcapKey, capture)
}

