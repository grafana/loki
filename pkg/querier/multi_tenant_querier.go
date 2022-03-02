package querier

import (
	"context"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/tenant"
)

const (
	defaultTenantLabel   = "__tenant_id__"
	retainExistingPrefix = "original_"
)

// MultiTenantQuerier is able to query across different tenants.
type MultiTenantQuerier struct {
	Querier
	resolver tenant.Resolver
}

// NewMultiTenantQuerier returns a new querier able to query across different tenants.
func NewMultiTenantQuerier(querier Querier, logger log.Logger) *MultiTenantQuerier {
	return &MultiTenantQuerier{
		Querier:  querier,
		resolver: tenant.NewMultiResolver(),
	}
}

func (q *MultiTenantQuerier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {

	tenantIDs, err := q.resolver.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}

	if len(tenantIDs) == 1 {
		singleContext := user.InjectUserID(ctx, tenantIDs[0])
		return q.Querier.SelectLogs(singleContext, params)
	}

	iters := make([]iter.EntryIterator, len(tenantIDs))
	for i, id := range tenantIDs {
		singleContext := user.InjectUserID(ctx, id)
		iter, err := q.Querier.SelectLogs(singleContext, params)

		if err != nil {
			return nil, err
		}

		iters[i] = NewTenantEntryIterator(id, iter)
	}
	return iter.NewMergeEntryIterator(ctx, iters, params.Direction), nil
}

func (q *MultiTenantQuerier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {

	tenantIDs, err := q.resolver.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}

	if len(tenantIDs) == 1 {
		singleContext := user.InjectUserID(ctx, tenantIDs[0])
		return q.Querier.SelectSamples(singleContext, params)
	}

	iters := make([]iter.SampleIterator, len(tenantIDs))
	for i, id := range tenantIDs {
		singleContext := user.InjectUserID(ctx, id)
		iter, err := q.Querier.SelectSamples(singleContext, params)

		if err != nil {
			return nil, err
		}

		iters[i] = NewTenantSampleIterator(id, iter)
	}
	return iter.NewMergeSampleIterator(ctx, iters), nil
}

type TenantEntryIterator struct {
	iter.EntryIterator
	tenantID string
}

func NewTenantEntryIterator(id string, iter iter.EntryIterator) *TenantEntryIterator {
	return &TenantEntryIterator{EntryIterator: iter, tenantID: id}
}

func (i *TenantEntryIterator) Labels() string {
	// TODO: cache manipulated labels
	lbls, _ := logql.ParseLabels(i.EntryIterator.Labels())

	// TODO: handle if lbls.Has(defaultTenantLabel)

	lbls = append(lbls, labels.Label{Name: defaultTenantLabel, Value: i.tenantID})
	return lbls.String()
}

type TenantSampleIterator struct {
	iter.SampleIterator
	tenantID string
}

func NewTenantSampleIterator(id string, iter iter.SampleIterator) *TenantSampleIterator {
	return &TenantSampleIterator{SampleIterator: iter, tenantID: id}
}

func (i *TenantSampleIterator) Labels() string {
	// TODO: cache manipulated labels
	lbls, _ := logql.ParseLabels(i.SampleIterator.Labels())

	// TODO: handle if lbls.Has(defaultTenantLabel)

	lbls = append(lbls, labels.Label{Name: defaultTenantLabel, Value: i.tenantID})
	return lbls.String()
}
