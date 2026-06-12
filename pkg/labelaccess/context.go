package labelaccess

import "context"

type contextKey int

const (
	contextKeyAuthorization contextKey = iota
	contextKeyAccessPolicy
)

// ExtractAuthorization a previously injected authorization header from context.
func ExtractAuthorization(ctx context.Context) (auth string, ok bool) {
	auth, ok = ctx.Value(contextKeyAuthorization).(string)
	return
}
