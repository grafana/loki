package logql

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
)

func NewMockQuerier(shards int, streams []logproto.Stream) MockQuerier {
	return MockQuerier{
		shards:  shards,
		streams: streams,
	}
}

// Shard aware mock querier
type MockQuerier struct {
	shards  int
	streams []logproto.Stream
}

func (q MockQuerier) SelectLogs(ctx context.Context, req SelectLogParams) (iter.EntryIterator, error) {
	expr, err := req.LogSelector()
	if err != nil {
		return nil, err
	}
	filter, err := expr.Filter()
	if err != nil {
		return nil, err
	}

	matchers := expr.Matchers()

	var shard *astmapper.ShardAnnotation
	if len(req.Shards) > 0 {
		shards, err := ParseShards(req.Shards)
		if err != nil {
			return nil, err
		}
		shard = &shards[0]
	}

	var matched []logproto.Stream

outer:
	for _, stream := range q.streams {
		ls := mustParseLabels(stream.Labels)

		// filter by shard if requested
		if shard != nil && ls.Hash()%uint64(shard.Of) != uint64(shard.Shard) {
			continue
		}

		for _, matcher := range matchers {
			if !matcher.Matches(ls.Get(matcher.Name)) {
				continue outer
			}
		}
		matched = append(matched, stream)
	}

	// apply the LineFilter
	filtered := make([]logproto.Stream, 0, len(matched))
	if filter == nil || filter == TrueFilter {
		filtered = matched
	} else {
		for _, s := range matched {
			var entries []logproto.Entry
			for _, entry := range s.Entries {
				if filter.Filter([]byte(entry.Line)) {
					entries = append(entries, entry)
				}
			}

			if len(entries) > 0 {
				filtered = append(filtered, logproto.Stream{
					Labels:  s.Labels,
					Entries: entries,
				})
			}
		}

	}

	return iter.NewTimeRangedIterator(
		iter.NewStreamsIterator(ctx, filtered, req.Direction),
		req.Start,
		req.End,
	), nil
}

func (q MockQuerier) SelectSamples(ctx context.Context, req SelectSampleParams) (iter.SampleIterator, error) {
	selector, err := req.LogSelector()
	if err != nil {
		return nil, err
	}
	filter, err := selector.Filter()
	if err != nil {
		return nil, err
	}
	expr, err := req.Expr()
	if err != nil {
		return nil, err
	}

	extractor, err := expr.Extractor()
	if err != nil {
		return nil, err
	}

	matchers := selector.Matchers()

	var shard *astmapper.ShardAnnotation
	if len(req.Shards) > 0 {
		shards, err := ParseShards(req.Shards)
		if err != nil {
			return nil, err
		}
		shard = &shards[0]
	}

	var matched []logproto.Stream

outer:
	for _, stream := range q.streams {
		ls := mustParseLabels(stream.Labels)

		// filter by shard if requested
		if shard != nil && ls.Hash()%uint64(shard.Of) != uint64(shard.Shard) {
			continue
		}

		for _, matcher := range matchers {
			if !matcher.Matches(ls.Get(matcher.Name)) {
				continue outer
			}
		}
		matched = append(matched, stream)
	}

	// apply the LineFilter
	filtered := make([]logproto.Series, 0, len(matched))
	for _, s := range matched {
		var samples []logproto.Sample
		for _, entry := range s.Entries {
			if filter == nil || filter.Filter([]byte(entry.Line)) {
				v, ok := extractor.Extract([]byte(entry.Line))
				if !ok {
					continue
				}
				samples = append(samples, logproto.Sample{
					Timestamp: entry.Timestamp.UnixNano(),
					Value:     v,
					Hash:      xxhash.Sum64([]byte(entry.Line)),
				})
			}
		}

		if len(samples) > 0 {
			filtered = append(filtered, logproto.Series{
				Labels:  s.Labels,
				Samples: samples,
			})
		}
	}

	return iter.NewTimeRangedSampleIterator(
		iter.NewMultiSeriesIterator(ctx, filtered),
		req.Start.UnixNano(),
		req.End.UnixNano(),
	), nil
}

type MockDownstreamer struct {
	*Engine
}

func (m MockDownstreamer) Downstreamer() Downstreamer { return m }

func (m MockDownstreamer) Downstream(ctx context.Context, queries []DownstreamQuery) ([]Result, error) {
	results := make([]Result, 0, len(queries))
	for _, query := range queries {
		params := NewLiteralParams(
			query.Expr.String(),
			query.Params.Start(),
			query.Params.End(),
			query.Params.Step(),
			query.Params.Interval(),
			query.Params.Direction(),
			query.Params.Limit(),
			query.Shards.Encode(),
		)
		res, err := m.Query(params).Exec(ctx)
		if err != nil {
			return nil, err
		}

		results = append(results, res)
	}
	return results, nil

}

// create nStreams of nEntries with labelNames each where each label value
// with the exception of the "index" label is modulo'd into a shard
func randomStreams(nStreams, nEntries, nShards int, labelNames []string) (streams []logproto.Stream) {
	for i := 0; i < nStreams; i++ {
		// labels
		stream := logproto.Stream{}
		ls := labels.Labels{{Name: "index", Value: fmt.Sprintf("%d", i)}}

		for _, lName := range labelNames {
			// I needed a way to hash something to uint64
			// in order to get some form of random label distribution
			shard := append(ls, labels.Label{
				Name:  lName,
				Value: fmt.Sprintf("%d", i),
			}).Hash() % uint64(nShards)

			ls = append(ls, labels.Label{
				Name:  lName,
				Value: fmt.Sprintf("%d", shard),
			})
		}
		for j := 0; j < nEntries; j++ {
			stream.Entries = append(stream.Entries, logproto.Entry{
				Timestamp: time.Unix(0, int64(j*int(time.Second))),
				Line:      fmt.Sprintf("line number: %d", j),
			})
		}

		stream.Labels = ls.String()
		streams = append(streams, stream)
	}
	return streams

}

func mustParseLabels(s string) labels.Labels {
	labels, err := parser.ParseMetric(s)
	if err != nil {
		log.Fatalf("Failed to parse %s", s)
	}

	return labels
}
