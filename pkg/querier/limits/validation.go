package limits

import (
	"context"
	"net/http"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
	util_validation "github.com/grafana/loki/v3/pkg/util/validation"
)

var nowFunc = func() time.Time { return time.Now() }

func ValidateQueryRequest(ctx context.Context, req logql.QueryParams, limits Limits) (time.Time, time.Time, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	selector, err := req.LogSelector()
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	matchers := selector.Matchers()

	maxStreamMatchersPerQuery := limits.MaxStreamsMatchersPerQuery(ctx, userID)
	if len(matchers) > maxStreamMatchersPerQuery {
		return time.Time{}, time.Time{}, httpgrpc.Errorf(http.StatusBadRequest,
			"max streams matchers per query exceeded, matchers-count > limit (%d > %d)", len(matchers), maxStreamMatchersPerQuery)
	}

	return ValidateQueryTimeRangeLimits(ctx, userID, limits, req.GetStart(), req.GetEnd())
}

func ValidateQueryTimeRangeLimits(ctx context.Context, userID string, limits TimeRangeLimits, from, through time.Time) (time.Time, time.Time, error) {
	now := nowFunc()
	// Clamp the time range based on the max query lookback.
	maxQueryLookback := limits.MaxQueryLookback(ctx, userID)
	if maxQueryLookback > 0 && from.Before(now.Add(-maxQueryLookback)) {
		origStartTime := from
		from = now.Add(-maxQueryLookback)

		level.Debug(spanlogger.FromContext(ctx)).Log(
			"msg", "the start time of the query has been manipulated because of the 'max query lookback' setting",
			"original", origStartTime,
			"updated", from)

	}
	maxQueryLength := limits.MaxQueryLength(ctx, userID)
	if maxQueryLength > 0 && (through).Sub(from) > maxQueryLength {
		return time.Time{}, time.Time{}, httpgrpc.Errorf(http.StatusBadRequest, util_validation.ErrQueryTooLong, (through).Sub(from), model.Duration(maxQueryLength))
	}
	if through.Before(from) {
		return time.Time{}, time.Time{}, httpgrpc.Errorf(http.StatusBadRequest, util_validation.ErrQueryTooOld, model.Duration(maxQueryLookback))
	}
	return from, through, nil
}
