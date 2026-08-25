package limits

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
	util_validation "github.com/grafana/loki/v3/pkg/util/validation"
)

var nowFunc = func() time.Time { return time.Now() }
var ErrInternalStreamsDrilldownOnly = fmt.Errorf("internal streams can only be queried from Logs Drilldown")

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

// ValidateAggregatedMetricQuery checks if the query is accessing __aggregated_metric__ or __pattern__ streams
// and ensures that only queries from Grafana Explore Logs can access them.
func ValidateAggregatedMetricQuery(ctx context.Context, req logql.QueryParams) error {
	selector, err := req.LogSelector()
	if err != nil {
		return err
	}

	// Check if the query targets aggregated metrics or patterns
	isInternalStreamQuery := false
	matchers := selector.Matchers()

	for _, matcher := range matchers {
		if matcher.Name == constants.AggregatedMetricLabel || matcher.Name == constants.PatternLabel {
			isInternalStreamQuery = true
			break
		}
	}

	if !isInternalStreamQuery {
		return nil
	}

	if httpreq.IsLogsDrilldownRequest(ctx) {
		return nil
	}
	return ErrInternalStreamsDrilldownOnly
}

func ValidateQueryTimeRangeLimits(ctx context.Context, userID string, limits TimeRangeLimits, from, through time.Time) (time.Time, time.Time, error) {
	now := nowFunc()
	// Clamp the time range based on the max query lookback.
	maxQueryLookback := limits.MaxQueryLookback(ctx, userID)
	if maxQueryLookback > 0 && from.Before(now.Add(-maxQueryLookback)) {
		origStartTime := from
		from = now.Add(-maxQueryLookback)

		level.Debug(spanlogger.FromContext(ctx, util_log.Logger)).Log(
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
