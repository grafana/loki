package fanout

import (
	"context"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/querier"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
)

func NewQuerier(primary querier.Querier, secondaries ...querier.Querier) querier.Querier {
	return &Querier{Querier: primary, secondaries: secondaries}
}

type Querier struct {
	querier.Querier
	secondaries []querier.Querier
}

func (q *Querier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	if len(q.secondaries) == 0 {
		return q.Querier.SelectLogs(ctx, params)
	}
	iters := make([]iter.EntryIterator, 0)
	localIter, err := q.Querier.SelectLogs(ctx, params)
	if err != nil {
		return nil, err
	}
	iters = append(iters, localIter)
	for _, remoteStore := range q.secondaries {
		iter, err := remoteStore.SelectLogs(ctx, params)
		if err != nil {
			return nil, err
		}
		iters = append(iters, iter)
	}
	return iter.NewSortEntryIterator(iters, params.Direction), nil
}

func (q *Querier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	if len(q.secondaries) == 0 {
		return q.Querier.SelectSamples(ctx, params)
	}
	iters := make([]iter.SampleIterator, 0)
	localIter, err := q.Querier.SelectSamples(ctx, params)
	if err != nil {
		return nil, err
	}
	iters = append(iters, localIter)
	for _, remoteStore := range q.secondaries {
		iter, err := remoteStore.SelectSamples(ctx, params)
		if err != nil {
			return nil, err
		}
		iters = append(iters, iter)
	}
	return iter.NewSortSampleIterator(iters), nil
}

func (q *Querier) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	if len(q.secondaries) == 0 {
		return q.Querier.Label(ctx, req)
	}
	responses := make([]*logproto.LabelResponse, 0)
	localLabel, err := q.Querier.Label(ctx, req)
	if err != nil {
		return nil, err
	}
	responses = append(responses, localLabel)
	for _, remoteStore := range q.secondaries {
		iter, err := remoteStore.Label(ctx, req)
		if err != nil {
			return nil, err
		}
		responses = append(responses, iter)
	}

	return logproto.MergeLabelResponses(responses)
}

func (q *Querier) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	if len(q.secondaries) == 0 {
		return q.Querier.Series(ctx, req)
	}
	responses := make([]*logproto.SeriesResponse, 0)
	localSeries, err := q.Querier.Series(ctx, req)
	if err != nil {
		return nil, err
	}
	responses = append(responses, localSeries)

	for _, querier := range q.secondaries {
		resp, err := querier.Series(ctx, req)
		if err != nil {
			return nil, err
		}
		responses = append(responses, resp)
	}

	return logproto.MergeSeriesResponses(responses)
}

func (q *Querier) IndexStats(ctx context.Context, req *loghttp.RangeQuery) (*stats.Stats, error) {
	if len(q.secondaries) == 0 {
		return q.IndexStats(ctx, req)
	}
	responses := make([]*stats.Stats, 0)
	localIndStats, err := q.IndexStats(ctx, req)
	if err != nil {
		return nil, err
	}
	responses = append(responses, localIndStats)
	for _, querier := range q.secondaries {
		resp, err := querier.IndexStats(ctx, req)
		if err != nil {
			return nil, err
		}
		responses = append(responses, resp)
	}

	merged := stats.MergeStats(responses...)

	return &merged, nil
}
