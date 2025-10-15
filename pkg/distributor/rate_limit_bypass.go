package distributor

import (
	"context"
	"net/http"
)

type contextKey int

const (
	rateLimitBypassContextKey contextKey = 0
	BypassRateLimitHeader     string     = "X-Bypass-Rate-Limit"
)

func injectRateLimitBypassFromRequest(req *http.Request) context.Context {
	bypass := req.Header.Get(BypassRateLimitHeader)
	return context.WithValue(req.Context(), interface{}(rateLimitBypassContextKey), bypass == "true")
}

func retrieveRateLimitBypass(ctx context.Context) bool {
	bypass, ok := ctx.Value(rateLimitBypassContextKey).(bool)
	if !ok {
		return false
	}
	return bypass
}
