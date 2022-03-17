package querier

import (
	"context"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/tenant"
)

const (
	defaultTenantLabel   = "__tenant_id__"
	retainExistingPrefix = "original_"
)

// MultiTenantQuerier is able to query across different tenants.
type MultiTenantQuerier struct {
	Querier
}

// NewMultiTenantQuerier returns a new querier able to query across different tenants.
func NewMultiTenantQuerier(querier Querier, logger log.Logger) *MultiTenantQuerier {
	return &MultiTenantQuerier{
		Querier: querier,
	}
}

func (q *MultiTenantQuerier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}

	if len(tenantIDs) == 1 {
		return q.Querier.SelectLogs(ctx, params)
	}

	iters := make([]iter.EntryIterator, len(tenantIDs))
	for i, id := range tenantIDs {
		singleContext := user.InjectOrgID(ctx, id)
		iter, err := q.Querier.SelectLogs(singleContext, params)
		if err != nil {
			return nil, err
		}

		iters[i] = NewTenantEntryIterator(iter, id)
	}
	return iter.NewSortEntryIterator(iters, params.Direction), nil
}

func (q *MultiTenantQuerier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}

	if len(tenantIDs) == 1 {
		return q.Querier.SelectSamples(ctx, params)
	}

	iters := make([]iter.SampleIterator, len(tenantIDs))
	for i, id := range tenantIDs {
		singleContext := user.InjectOrgID(ctx, id)
		iter, err := q.Querier.SelectSamples(singleContext, params)
		if err != nil {
			return nil, err
		}

		iters[i] = NewTenantSampleIterator(iter, id)
	}
	return iter.NewSortSampleIterator(iters), nil
}

// TenantEntry Iterator wraps an entry iterator and adds the tenant label.
type TenantEntryIterator struct {
	iter.EntryIterator
	tenantID string
}

func NewTenantEntryIterator(iter iter.EntryIterator, id string) *TenantEntryIterator {
	return &TenantEntryIterator{EntryIterator: iter, tenantID: id}
}

func (i *TenantEntryIterator) Labels() string {
	// TODO: cache manipulated labels and add a benchmark.
	lbls, _ := syntax.ParseLabels(i.EntryIterator.Labels())
	builder := labels.NewBuilder(lbls.WithoutLabels(defaultTenantLabel))

	// Prefix label if it conflicts with the tenant label.
	if lbls.Has(defaultTenantLabel) {
		builder.Set(retainExistingPrefix+defaultTenantLabel, lbls.Get(defaultTenantLabel))
	}
	builder.Set(defaultTenantLabel, i.tenantID)

	return builder.Labels().String()
}

// TenantEntry Iterator wraps a sample iterator and adds the tenant label.
type TenantSampleIterator struct {
	iter.SampleIterator
	tenantID string
}

func NewTenantSampleIterator(iter iter.SampleIterator, id string) *TenantSampleIterator {
	return &TenantSampleIterator{SampleIterator: iter, tenantID: id}
}

func (i *TenantSampleIterator) Labels() string {
	// TODO: cache manipulated labels
	lbls, _ := syntax.ParseLabels(i.SampleIterator.Labels())
	builder := labels.NewBuilder(lbls.WithoutLabels(defaultTenantLabel))

	// Prefix label if it conflicts with the tenant label.
	if lbls.Has(defaultTenantLabel) {
		builder.Set(retainExistingPrefix+defaultTenantLabel, lbls.Get(defaultTenantLabel))
	}
	builder.Set(defaultTenantLabel, i.tenantID)

	return builder.Labels().String()
}
