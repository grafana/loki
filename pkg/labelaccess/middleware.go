package labelaccess

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/middleware"
	"github.com/prometheus/prometheus/model/labels"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/labelaccess/types"
	"github.com/grafana/loki/v3/pkg/logql/syntax"

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

			// For volume requests, inject LBAC matchers into the stream selector
			// so the index only returns streams matching the policies.
			if isVolumeRequest(r) {
				if err := modifyVolumeQuery(r, matchers); err != nil {
					_ = level.Error(util_log.Logger).Log("msg", "lbac middleware: failed to modify volume query for LBAC", "err", err)
					WritePromError(w, err.Error(), http.StatusBadRequest)
					return
				}
			}

		}

		next.ServeHTTP(w, r)
	})
}

// isVolumeRequest checks if the request is a Loki volume query.
func isVolumeRequest(r *http.Request) bool {
	return strings.Contains(r.URL.Path, "/loki/api/v1/index/volume")
}

// modifyVolumeQuery injects LBAC label matchers into the volume request's stream selector.
// For a single policy, all its matchers are ANDed into the query parameter so the index
// only returns streams the tenant is allowed to see. For multiple policies, query
// modification is skipped (response-level filtering in the tripperware handles LBAC).
func modifyVolumeQuery(r *http.Request, policySet LabelPolicySet) error {
	orgID := r.Header.Get("X-Scope-OrgID")
	if orgID == "" {
		return nil
	}

	policies, exists := policySet[orgID]
	if !exists || len(policies) == 0 {
		return nil
	}

	query := r.URL.Query().Get("query")
	if query == "" {
		query = "{}"
	}

	existingMatchers, err := syntax.ParseMatchers(query, true)
	if err != nil {
		return fmt.Errorf("lbac middleware: failed to parse volume query %q: %w", query, err)
	}

	// For multiple policies, skip query modification — the response-level filter in the
	// tripperware middleware handles LBAC with correct OR semantics across policies.
	if len(policies) > 1 {
		_ = level.Debug(util_log.Logger).Log(
			"msg", "lbac middleware: multiple policies for volume query, skipping query-level modification",
			"org_id", orgID,
			"num_policies", len(policies),
		)
		return nil
	}

	policy := policies[0]
	lbacMatchers := make([]*labels.Matcher, 0, len(policy.Selector))
	for _, lm := range policy.Selector {
		m, err := types.LabelMatcherToPromLabel(lm)
		if err != nil {
			return fmt.Errorf("lbac middleware: failed to convert label matcher for volume query: %w", err)
		}
		lbacMatchers = append(lbacMatchers, m)
	}

	if len(lbacMatchers) == 0 {
		return nil
	}

	combined := make([]*labels.Matcher, 0, len(existingMatchers)+len(lbacMatchers))
	combined = append(combined, existingMatchers...)
	combined = append(combined, lbacMatchers...)
	modifiedQuery := syntax.MatchersString(combined)

	if modifiedQuery == query {
		return nil
	}

	q := r.URL.Query()
	q.Set("query", modifiedQuery)
	r.URL.RawQuery = q.Encode()

	_ = level.Info(util_log.Logger).Log(
		"msg", "lbac middleware: modified volume query to enforce LBAC",
		"original_query", query,
		"modified_query", modifiedQuery,
		"org_id", orgID,
	)

	return nil
}
