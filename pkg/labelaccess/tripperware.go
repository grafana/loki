package labelaccess

import (
	"context"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
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

			resp, err := next.Do(ctx, req)
			if err != nil {
				return nil, err
			}

			// Filter volume responses to enforce LBAC as defense-in-depth.
			// This catches any streams that slipped through upstream filtering,
			// and correctly handles multi-policy OR semantics for series aggregation.
			if len(labelPolicySet) > 0 {
				resp = filterVolumeResponse(ctx, resp, labelPolicySet)
			}

			return resp, nil
		})
	})
}

// filterVolumeResponse filters out volume entries whose stream labels don't match
// any LBAC policy. It mirrors ChunkFilterer.ShouldFilter semantics: within a policy
// all matchers must match (AND), across policies any match passes (OR).
//
// For non-series aggregation modes, Volume.Name is a bare label name (e.g. "namespace")
// rather than a full label set — those entries are passed through unchanged, since
// query-level LBAC injection in the HTTP middleware already filtered upstream.
func filterVolumeResponse(ctx context.Context, resp queryrangebase.Response, policySet LabelPolicySet) queryrangebase.Response {
	volResp, ok := resp.(*queryrange.VolumeResponse)
	if !ok || volResp.Response == nil {
		return resp
	}

	orgID, _ := user.ExtractOrgID(ctx)
	policies, exists := policySet[orgID]
	if !exists || len(policies) == 0 {
		return resp
	}

	cf := &ChunkFilterer{policies: policies}
	cf.loadMatchers()

	filtered := make([]logproto.Volume, 0, len(volResp.Response.Volumes))
	filteredCount := 0
	for _, v := range volResp.Response.Volumes {
		ls, err := syntax.ParseLabels(v.Name)
		if err != nil {
			// Not a parseable label set (e.g. a bare label name from non-series aggregation).
			// Pass through — query-level filtering handles this case.
			filtered = append(filtered, v)
			continue
		}
		if cf.ShouldFilter(ls) {
			filteredCount++
			continue
		}
		filtered = append(filtered, v)
	}

	if filteredCount == 0 {
		return resp
	}

	level.Debug(util_log.Logger).Log(
		"msg", "lbac tripperware: filtered volume response entries",
		"org_id", orgID,
		"total", len(volResp.Response.Volumes),
		"filtered_out", filteredCount,
		"remaining", len(filtered),
	)

	return &queryrange.VolumeResponse{
		Response: &logproto.VolumeResponse{
			Volumes: filtered,
			Limit:   volResp.Response.Limit,
		},
		Headers: volResp.Headers,
	}
}

// UserIDTransformer returns the changed userID if there is a LabelPolicy set in the context.
// This is used in Loki in GenerateCacheKey so the cached results have the labelpolicy as part of the cache key.
func UserIDTransformer(ctx context.Context, userID string) string {
	if labelPolicyHash, _ := ctx.Value(contextKeyLabelPolicyHash).(string); len(labelPolicyHash) > 0 {
		userID = userID + tenantSeparator + labelPolicyHash
	}
	return userID
}
