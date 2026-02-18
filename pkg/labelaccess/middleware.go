package labelaccess

import (
	"fmt"
	"net/http"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/middleware"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type labelAccessMiddleware struct {
	logger log.Logger
}

// NewLabelAccessMiddleware returns a HTTP middleware that parses the X-Prom-Label-Policy header
// and injects the parsed label selectors into the request context.
func NewLabelAccessMiddleware(logger log.Logger) middleware.Interface {
	return &labelAccessMiddleware{
		logger: logger,
	}
}

// Wrap implements the middleware interface
func (l *labelAccessMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		matchers, err := ExtractLabelMatchersHTTP(r)
		if err != nil {
			_ = level.Debug(util_log.Logger).Log("msg", "unable to find instance policy map in HTTP request", "err", err)
			WritePromError(w, err.Error(), http.StatusBadRequest)
			return
		}

		r = r.Clone(InjectLabelMatchersContext(r.Context(), matchers))

		if len(matchers) > 0 {
			_ = level.Debug(util_log.Logger).Log("msg", "inject x-prom-label-policy", "matchers", matchers.String())

			trace.SpanFromContext(r.Context()).SetAttributes(
				attribute.String("inject x-prom-label-policy", matchers.String()),
			)

			// If this is a query request, check for aggregated metrics parameters
			// and modify query if needed
			if isQueryRequest(r) {
				err := ModifyAggregatedMetricsQuery(r, matchers)
				if err != nil {
					_ = level.Error(util_log.Logger).Log("msg", "failed to modify aggregated metric query", "err", err)
					WritePromError(w, err.Error(), http.StatusBadRequest)
					return
				}
				_ = level.Debug(util_log.Logger).Log("msg", "request after modifying aggregated metric query", "request", fmt.Sprintf("%v", r))
			}
		}

		next.ServeHTTP(w, r)
	})
}
