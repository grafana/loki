package labelaccess

import (
	"context"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

type contextKeyLabelPolicyHashType string

const (
	contextKeyLabelPolicyHash contextKeyLabelPolicyHashType = "lbac_hash"

	tenantSeparator string = "!"
)

// Not used by GEM or GEL and supported by upstream

// NewMiddleware wraps the existing middleware to make sure the LBAC headers are propagated
func NewMiddleware() queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
			labelPolicySet, err := ExtractLabelMatchersContext(ctx)

			// This should never happen anyways because we have auth middleware before this.
			if err != nil {
				return nil, err
			}

			// If the instance policies are set on the query, ensure the user ID is altered to respect
			// them before being passed to the wrapped tripperware.
			if len(labelPolicySet) != 0 {
				ctx = context.WithValue(ctx, contextKeyLabelPolicyHash, labelPolicySet.hash())
			}

			return next.Do(ctx, req)
		})
	})
}

// UserIDTransformer returns the changed userID if there is a LabelPolicy set in the context.
// This is used in Loki in GenerateCacheKey so the cached results have the labelpolicy as part of the cache key.
func UserIDTransformer(ctx context.Context, userID string) string {
	if labelPolicyHash, _ := ctx.Value(contextKeyLabelPolicyHash).(string); len(labelPolicyHash) > 0 {
		userID = userID + tenantSeparator + labelPolicyHash
	}
	return userID
}
