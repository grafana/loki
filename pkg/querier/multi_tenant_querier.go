package querier

import (
	"context"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/weaveworks/common/user"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
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

type relabel struct {
	tenantID string
	cache    map[string]labels.Labels
}

func (r relabel) relabel(original string) string {
	lbls, ok := r.cache[original]
	if ok {
		return lbls.String()
	}

	lbls, _ = syntax.ParseLabels(original)
	builder := labels.NewBuilder(lbls.WithoutLabels(defaultTenantLabel))

	// Prefix label if it conflicts with the tenant label.
	if lbls.Has(defaultTenantLabel) {
		builder.Set(retainExistingPrefix+defaultTenantLabel, lbls.Get(defaultTenantLabel))
	}
	builder.Set(defaultTenantLabel, r.tenantID)

	lbls = builder.Labels()
	r.cache[original] = lbls
	return lbls.String()
}

// TenantEntry Iterator wraps an entry iterator and adds the tenant label.
type TenantEntryIterator struct {
	iter.EntryIterator
	relabel
}

func NewTenantEntryIterator(iter iter.EntryIterator, id string) *TenantEntryIterator {
	return &TenantEntryIterator{
		EntryIterator: iter,
		relabel: relabel{
			tenantID: id,
			cache:    map[string]labels.Labels{},
		},
	}
}

func (i *TenantEntryIterator) Labels() string {
	return i.relabel.relabel(i.EntryIterator.Labels())
}

// TenantEntry Iterator wraps a sample iterator and adds the tenant label.
type TenantSampleIterator struct {
	iter.SampleIterator
	relabel
}

func NewTenantSampleIterator(iter iter.SampleIterator, id string) *TenantSampleIterator {
	return &TenantSampleIterator{
		SampleIterator: iter,
		relabel: relabel{
			tenantID: id,
			cache:    map[string]labels.Labels{},
		},
	}

}

func (i *TenantSampleIterator) Labels() string {
	return i.relabel.relabel(i.SampleIterator.Labels())
}
