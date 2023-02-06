package querier

import (
	"context"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/weaveworks/common/user"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
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

	selector, err := params.LogSelector()
	if err != nil {
		return nil, err
	}
	matchedTenants, filteredMatchers := filterValuesByMatchers(defaultTenantLabel, tenantIDs, selector.Matchers()...)
	params.Selector = replaceMatchers(selector, filteredMatchers).String()

	iters := make([]iter.EntryIterator, len(matchedTenants))
	i := 0
	for id := range matchedTenants {
		singleContext := user.InjectOrgID(ctx, id)
		iter, err := q.Querier.SelectLogs(singleContext, params)
		if err != nil {
			return nil, err
		}

		iters[i] = NewTenantEntryIterator(iter, id)
		i++
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

	matchedTenants, updatedSelector, err := removeTenantSelector(params, tenantIDs)
	if err != nil {
		return nil, err
	}
	params.Selector = updatedSelector.String()

	iters := make([]iter.SampleIterator, len(matchedTenants))
	i := 0
	for id := range matchedTenants {
		singleContext := user.InjectOrgID(ctx, id)
		iter, err := q.Querier.SelectSamples(singleContext, params)
		if err != nil {
			return nil, err
		}

		iters[i] = NewTenantSampleIterator(iter, id)
		i++
	}
	return iter.NewSortSampleIterator(iters), nil
}

func (q *MultiTenantQuerier) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}

	if req.Values && req.Name == defaultTenantLabel {
		return &logproto.LabelResponse{Values: tenantIDs}, nil
	}

	if len(tenantIDs) == 1 {
		return q.Querier.Label(ctx, req)
	}

	responses := make([]*logproto.LabelResponse, len(tenantIDs))
	for i, id := range tenantIDs {
		singleContext := user.InjectOrgID(ctx, id)
		resp, err := q.Querier.Label(singleContext, req)
		if err != nil {
			return nil, err
		}

		responses[i] = resp
	}

	// Append tenant ID label name if label names are requested.
	if !req.Values {
		responses = append(responses, &logproto.LabelResponse{Values: []string{defaultTenantLabel}})
	}

	return logproto.MergeLabelResponses(responses)
}

func (q *MultiTenantQuerier) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}

	if len(tenantIDs) == 1 {
		return q.Querier.Series(ctx, req)
	}

	responses := make([]*logproto.SeriesResponse, len(tenantIDs))
	for i, id := range tenantIDs {
		singleContext := user.InjectOrgID(ctx, id)
		resp, err := q.Querier.Series(singleContext, req)
		if err != nil {
			return nil, err
		}

		for _, s := range resp.GetSeries() {
			if _, ok := s.Labels[defaultTenantLabel]; !ok {
				s.Labels[defaultTenantLabel] = id
			}
		}

		responses[i] = resp
	}

	return logproto.MergeSeriesResponses(responses)
}

func (q *MultiTenantQuerier) IndexStats(ctx context.Context, req *loghttp.RangeQuery) (*stats.Stats, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}

	if len(tenantIDs) == 1 {
		return q.Querier.IndexStats(ctx, req)
	}

	responses := make([]*stats.Stats, len(tenantIDs))
	for i, id := range tenantIDs {
		singleContext := user.InjectOrgID(ctx, id)
		resp, err := q.Querier.IndexStats(singleContext, req)
		if err != nil {
			return nil, err
		}

		responses[i] = resp
	}

	merged := stats.MergeStats(responses...)

	return &merged, nil
}

// removeTenantSelector filters the given tenant IDs based on any tenant ID filter the in passed selector.
func removeTenantSelector(params logql.SelectSampleParams, tenantIDs []string) (map[string]struct{}, syntax.Expr, error) {
	expr, err := params.Expr()
	if err != nil {
		return nil, nil, err
	}
	selector, err := expr.Selector()
	if err != nil {
		return nil, nil, err
	}
	matchedTenants, filteredMatchers := filterValuesByMatchers(defaultTenantLabel, tenantIDs, selector.Matchers()...)
	updatedExpr := replaceMatchers(expr, filteredMatchers)
	return matchedTenants, updatedExpr, nil
}

// replaceMatchers traverses the passed expression and replaces all matchers.
func replaceMatchers(expr syntax.Expr, matchers []*labels.Matcher) syntax.Expr {
	expr, _ = syntax.Clone(expr)
	expr.Walk(func(e interface{}) {
		switch concrete := e.(type) {
		case *syntax.MatchersExpr:
			concrete.Mts = matchers
		}
	})
	return expr
}

// See https://github.com/grafana/mimir/blob/114ab88b50638a2047e2ca2a60640f6ca6fe8c17/pkg/querier/tenantfederation/tenant_federation.go#L29-L69
// filterValuesByMatchers applies matchers to inputed `idLabelName` and
// `ids`. A set of matched IDs is returned and also all label matchers not
// targeting the `idLabelName` label.
//
// In case a label matcher is set on a label conflicting with `idLabelName`, we
// need to rename this labelMatcher's name to its original name. This is used
// to as part of Select in the mergeQueryable, to ensure only relevant queries
// are considered and the forwarded matchers do not contain matchers on the
// `idLabelName`.
func filterValuesByMatchers(idLabelName string, ids []string, matchers ...*labels.Matcher) (matchedIDs map[string]struct{}, unrelatedMatchers []*labels.Matcher) {
	// this contains the matchers which are not related to idLabelName
	unrelatedMatchers = make([]*labels.Matcher, 0, len(matchers))

	// build map of values to consider for the matchers
	matchedIDs = sliceToSet(ids)

	for _, m := range matchers {
		switch m.Name {
		// matcher has idLabelName to target a specific tenant(s)
		case idLabelName:
			for value := range matchedIDs {
				if !m.Matches(value) {
					delete(matchedIDs, value)
				}
			}

		// check if has the retained label name
		case retainExistingPrefix + idLabelName:
			// rewrite label to the original name, by copying matcher and
			// replacing the label name
			rewrittenM := *m
			rewrittenM.Name = idLabelName
			unrelatedMatchers = append(unrelatedMatchers, &rewrittenM)

		default:
			unrelatedMatchers = append(unrelatedMatchers, m)
		}
	}

	return matchedIDs, unrelatedMatchers
}

func sliceToSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		out[v] = struct{}{}
	}
	return out
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
	builder := labels.NewBuilder(lbls).Del(defaultTenantLabel)

	// Prefix label if it conflicts with the tenant label.
	if lbls.Has(defaultTenantLabel) {
		builder.Set(retainExistingPrefix+defaultTenantLabel, lbls.Get(defaultTenantLabel))
	}
	builder.Set(defaultTenantLabel, r.tenantID)

	lbls = builder.Labels(nil)
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
