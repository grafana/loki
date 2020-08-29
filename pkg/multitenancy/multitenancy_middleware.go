package multitenancy

import (
	"context"
	"net/http"

	"github.com/weaveworks/common/middleware"
)

// Pretty similar to the fake_auth middleware, we basically just need a way to inform the handler that
// we intend on using the a label from the data as the orgID rather than from the actual header, and pass
// the label we want to use via context

// InjectLabelForID injects a field into the context that specifies using a label rather than orgID from header
func InjectLabelForID(ctx context.Context, label string, undefined string) context.Context {
	newCtx := context.WithValue(ctx, interface{}("useLabelAsOrgID"), label)
	return context.WithValue(newCtx, interface{}("useLabelAsOrgIDUndefined"), undefined)
}

// GetLabelFromContext returns the value set for label and undefined
func GetLabelFromContext(ctx context.Context) (string, string) {
	label, ok1 := ctx.Value("useLabelAsOrgID").(string)
	undefined, ok2 := ctx.Value("useLabelAsOrgIDUndefined").(string)
	if ok1 && ok2 {
		return undefined, label
	}
	return "", ""
}

// HTTPAuthMiddleware middleware use for injecting value specificying using labels
func HTTPAuthMiddleware(label string, undefined string) middleware.Func {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// ctx := user.InjectOrgID(r.Context(), nil)
			ctx := InjectLabelForID(r.Context(), label, undefined)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
}
