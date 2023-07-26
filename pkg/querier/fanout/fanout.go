package fanout

import (
	"context"
	"github.com/grafana/loki/pkg/storage/stores/index/seriesvolume"
	"time"

	"github.com/grafana/dskit/concurrency"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/querier"
	"github.com/grafana/loki/pkg/querier/remote"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
)

func NewQuerier(primary querier.Querier, readConcurrency int, batchSize int, secondaries ...remote.DetailQuerier) querier.Querier {
	return &Querier{
		Querier:         primary,
		readConcurrency: readConcurrency,
		batchSize:       batchSize,
		secondaries:     secondaries,
	}
}

type Querier struct {
	querier.Querier
	secondaries     []remote.DetailQuerier
	readConcurrency int
	batchSize       int
}

func (q *Querier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	if len(q.secondaries) == 0 {
		return q.Querier.SelectLogs(ctx, params)
	}
	localIter, err := q.Querier.SelectLogs(ctx, params)
	if err != nil {
		return nil, err
	}
	iters := make([]iter.EntryIterator, 0)
	iters = append(iters, localIter)
	resps := make([]iter.EntryIterator, len(q.secondaries))
	if q.batchSize > 0 {
		resps, err = q.queryBatchLog(ctx, params)
	} else {
		err = concurrency.ForEachJob(ctx, len(q.secondaries), q.readConcurrency, func(ctx context.Context, idx int) error {
			remoteStore := q.secondaries[idx]
			iter, _, err := remoteStore.SelectLogDetails(ctx, params)
			if err != nil {
				return err
			}
			resps[idx] = iter
			return nil
		})
	}
	if err != nil {
		return nil, err
	}
	return iter.NewSortEntryIterator(append(iters, resps...), params.Direction), nil

}

// queryBatchLog Fetch the results from the remote in batches.
func (q *Querier) queryBatchLog(ctx context.Context, params logql.SelectLogParams) ([]iter.EntryIterator, error) {
	allTotal := int(params.Limit)
	unlimited := params.Limit == 0

	batchLimit := int(params.Limit)
	batchSize := q.batchSize
	if batchLimit < q.batchSize && !unlimited {
		batchSize = batchLimit
	}

	total := 0
	start := params.Start
	end := params.End
	var lastEntry loghttp.Entry
	var lastEntryLen int

	loopCount := 0
	iters := make([]iter.EntryIterator, 0)
	for total < batchLimit || unlimited {
		//prepare query param
		bs := batchSize
		if batchLimit-total < batchSize && !unlimited {
			bs = batchLimit - total + lastEntryLen
		}
		params.Start = start
		params.End = end
		params.Limit = uint32(bs)
		//query log
		respStreams := make([]loghttp.Streams, len(q.secondaries))
		err := concurrency.ForEachJob(ctx, len(q.secondaries), q.readConcurrency, func(ctx context.Context, idx int) error {
			remoteStore := q.secondaries[idx]
			_, logs, err := remoteStore.SelectLogDetails(ctx, params)
			if err != nil {
				return err
			}
			respStreams[idx] = logs
			return nil
		})
		if err != nil {
			return nil, err
		}
		//process resp data.
		isLoopOver := false
		lastEntryLen = 0
		for _, respStream := range respStreams {
			streamsIterator := iter.NewStreamsIterator(respStream.ToProto(), params.Direction)
			iters = append(iters, streamsIterator)
			if isLoopOver {
				break
			}
			for _, stream := range respStream {
				if isLoopOver {
					break
				}
				for _, entry := range stream.Entries {
					entry := entry
					lastEntry = entry
					lastEntryLen++
					total++
					if total >= allTotal {
						isLoopOver = true
						break
					}
				}
			}
		}
		//check condition
		if lastEntryLen == 0 {
			break
		}
		if isLoopOver {
			break
		}
		if params.Direction == logproto.FORWARD {
			start = lastEntry.Timestamp
		} else {
			end = lastEntry.Timestamp.Add(1 * time.Nanosecond)
		}
		loopCount++
	}
	return iters, nil
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

	resps := make([]iter.SampleIterator, len(q.secondaries))
	err = concurrency.ForEachJob(ctx, len(q.secondaries), q.readConcurrency, func(ctx context.Context, idx int) error {
		remoteStore := q.secondaries[idx]
		iter, err := remoteStore.SelectSamples(ctx, params)
		if err != nil {
			return err
		}
		resps[idx] = iter
		return nil
	})
	if err != nil {
		return nil, err
	}
	iters = append(iters, resps...)
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
		return q.Querier.IndexStats(ctx, req)
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

func (q *Querier) Volume(ctx context.Context, req *logproto.VolumeRequest) (*logproto.VolumeResponse, error) {
	if len(q.secondaries) == 0 {
		return q.Querier.Volume(ctx, req)
	}

	responses := make([]*logproto.VolumeResponse, 0)
	for _, querier := range q.secondaries {
		resp, err := querier.Volume(ctx, req)
		if err != nil {
			return nil, err
		}
		responses = append(responses, resp)
	}

	merged := seriesvolume.Merge(responses, req.Limit)
	return merged, nil
}
