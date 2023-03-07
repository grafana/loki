package querylimits

import (
	"net/http"

	"github.com/go-kit/log"
	"github.com/weaveworks/common/middleware"
)

type queryLimitsMiddleware struct {
	logger log.Logger
}

// NewQueryLimitsMiddleware creates a middleware that extracts the query limits
// policy from the HTTP header and injects it into the context of the request.
func NewQueryLimitsMiddleware(logger log.Logger) middleware.Interface {
	return &queryLimitsMiddleware{
		logger: logger,
	}
}

// Wrap implements the middleware interface
func (l *queryLimitsMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limits, err := ExtractQueryLimitsHTTP(r)
		if err != nil {
			// todo, how to return error
			// api.WritePromError(w, err.Error(), http.StatusBadRequest)
			return
		}

		if limits != nil {
			r = r.Clone(InjectQueryLimitsContext(r.Context(), *limits))
		}

		next.ServeHTTP(w, r)
	})
}
