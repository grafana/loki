package querylimits

import (
	"net/http"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/weaveworks/common/middleware"

	util_log "github.com/grafana/loki/pkg/util/log"
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
			w.WriteHeader(http.StatusBadRequest)
			if _, err := w.Write([]byte(err.Error())); err != nil {
				level.Error(util_log.Logger).Log("msg", "error in queryLimitsMiddleware Wrap", "err", err)
			}
			return
		}

		if limits != nil {
			r = r.Clone(InjectQueryLimitsContext(r.Context(), *limits))
		}

		next.ServeHTTP(w, r)
	})
}
