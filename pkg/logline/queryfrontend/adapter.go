package queryfrontend

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

type frontendHintAdapter struct {
	inner *hintprovider.LoglineHintProvider
	next  queryrangebase.Handler
}

func (a *frontendHintAdapter) MinDate() time.Time { return a.inner.MinDate() }

func (a *frontendHintAdapter) ProvideHints(
	ctx context.Context,
	tenant string,
	expr syntax.Expr,
	from, through model.Time,
) (*hintprovider.Hints, *hintprovider.QueryStats, error) {
	plan, err := a.inner.PlanHints(tenant, expr, from, through)
	if err != nil || len(plan.Indexes) == 0 {
		return &hintprovider.Hints{TimeRanges: plan.Ranges}, plan.Stats, err
	}

	resp, err := a.next.Do(ctx, &logproto.LoglineIndexRequest{
		From:        from,
		Through:     through,
		Expr:        expr.String(),
		Indexes:     hintprovider.ToProtoIndexMetas(plan.Indexes), // export or keep toProto in this pkg via a helper on HintPlan
		NgramLength: plan.NgramLength,                             // or pass from cfg
		MaxParallel: plan.MaxParallel,
	})

	if err != nil {
		return nil, plan.Stats, err
	}
	hr, ok := resp.(*queryrange.LoglineIndexResponse)
	if !ok || hr == nil || hr.Response == nil {
		return nil, plan.Stats, fmt.Errorf("unexpected hint response type %T", resp)
	}
	ranges := append(plan.Ranges, hintprovider.FromProtoRanges(hr.Response.TimeRanges)...)
	return &hintprovider.Hints{TimeRanges: hintprovider.NormalizeRanges(ranges)}, hintprovider.FromProtoStats(hr.Response.Stats), nil
}

var _ hintprovider.QueryHintProvider = (*frontendHintAdapter)(nil)
