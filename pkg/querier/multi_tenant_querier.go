package querier

import (
	"context"

	"github.com/go-kit/log"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/tenant"
)

// MultiTenantQuerier is able to query across different tenants.
type MultiTenantQuerier struct {
	SingleTenantQuerier
	resolver tenant.Resolver
}

// NewMultiTenantQuerier returns a new querier able to query across different tenants.
func NewMultiTenantQuerier(querier SingleTenantQuerier, logger log.Logger) *MultiTenantQuerier {
	return &MultiTenantQuerier{
		SingleTenantQuerier: querier,
		resolver:            tenant.NewMultiResolver(),
	}
}

func (q *MultiTenantQuerier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {

	tenantIDs, err := q.resolver.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}

	iters := make([]iter.EntryIterator, len(tenantIDs))

	for _, id := range tenantIDs {
		singleContext := user.InjectUserID(ctx, id)
		iter, err := q.SingleTenantQuerier.SelectLogs(singleContext, params)

		if err != nil {
			return nil, err
		}

		iters = append(iters, NewTenantIterator(id, iter))
	}
	return iter.NewMergeEntryIterator(ctx, iters, params.Direction), nil
}

func (q *MultiTenantQuerier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {

	tenantIDs, err := q.resolver.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}

	iters := make([]iter.EntryIterator, len(tenantIDs))

	for _, id := range tenantIDs {
		singleContext := user.InjectUserID(ctx, id)
		iter, err := q.SingleTenantQuerier.SelectSamples(singleContext, params)

		if err != nil {
			return nil, err
		}

		iters = append(iters, NewTenantIterator(id, iter))
	}
	return iter.NewMergeEntryIterator(ctx, iters, params.Direction), nil
}
