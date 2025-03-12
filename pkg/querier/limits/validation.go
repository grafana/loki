package limits

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
	util_validation "github.com/grafana/loki/v3/pkg/util/validation"
)

const logsDrilldownAppName = "grafana-lokiexplore-app"

var nowFunc = func() time.Time { return time.Now() }
var ErrAggMetricsDrilldownOnly = fmt.Errorf("aggregated metric queries can only be accessed from Logs Drilldown")

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

// ValidateAggregatedMetricQuery checks if the query is accessing __aggregated_metric__ streams
// and ensures that only queries from Grafana Explore Logs can access them.
func ValidateAggregatedMetricQuery(ctx context.Context, req logql.QueryParams) error {
	selector, err := req.LogSelector()
	if err != nil {
		return err
	}

	// Check if the query targets aggregated metrics
	isAggregatedMetricQuery := false
	matchers := selector.Matchers()

	for _, matcher := range matchers {
		if matcher.Name == push.AggregatedMetricLabel {
			isAggregatedMetricQuery = true
			break
		}
	}

	if !isAggregatedMetricQuery {
		return nil
	}

	tags := httpreq.ExtractQueryTagsFromContext(ctx)
	kvs := httpreq.TagsToKeyValues(tags)

	// KVs is an []interface{} of key value pairs, so iterate by keys
	for i := 0; i < len(kvs); i += 2 {
		current, ok := kvs[i].(string)
		if !ok {
			continue
		}

		next, ok := kvs[i+1].(string)
		if !ok {
			continue
		}

		if current == "source" && strings.EqualFold(next, logsDrilldownAppName) {
			return nil
		}
	}
	return ErrAggMetricsDrilldownOnly
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
