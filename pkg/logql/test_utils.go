package logql

import (
	"context"
	"fmt"
	logger "log"
	"math/rand"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/cespare/xxhash/v2"
	"github.com/prometheus/prometheus/model/labels"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
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

func (q MockQuerier) extractOldShard(xs []string) (*index.ShardAnnotation, error) {
	parsed, version, err := ParseShards(xs)
	if err != nil {
		return nil, err
	}

	if version != PowerOfTwoVersion {
		return nil, fmt.Errorf("unsupported shard version: %d", version)
	}

	return parsed[0].PowerOfTwo, nil

}

func (q MockQuerier) SelectLogs(_ context.Context, req SelectLogParams) (iter.EntryIterator, error) {
	expr, err := req.LogSelector()
	if err != nil {
		return nil, err
	}
	pipeline, err := expr.Pipeline()
	if err != nil {
		return nil, err
	}

	matchers := expr.Matchers()

	var shard *index.ShardAnnotation
	if len(req.Shards) > 0 {
		shard, err = q.extractOldShard(req.Shards)
		if err != nil {
			return nil, err
		}
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
	filtered := processStream(matched, pipeline)

	streamIters := make([]iter.EntryIterator, 0, len(filtered))
	for i := range filtered {
		// This is the same as how LazyChunk or MemChunk build their iterators,
		// they return a TimeRangedIterator which is wrapped in a EntryReversedIter if the direction is BACKWARD
		iterForward := iter.NewTimeRangedIterator(iter.NewStreamIterator(filtered[i]), req.Start, req.End)
		if req.Direction == logproto.FORWARD {
			streamIters = append(streamIters, iterForward)
		} else {
			reversed, err := iter.NewEntryReversedIter(iterForward)
			if err != nil {
				return nil, err
			}
			streamIters = append(streamIters, reversed)
		}
	}

	return iter.NewSortEntryIterator(streamIters, req.Direction), nil
}

func processStream(in []logproto.Stream, pipeline log.Pipeline) []logproto.Stream {
	resByStream := map[string]*logproto.Stream{}

	for _, stream := range in {
		for _, e := range stream.Entries {
			sp := pipeline.ForStream(mustParseLabels(stream.Labels))
			if l, out, matches := sp.Process(e.Timestamp.UnixNano(), []byte(e.Line)); matches {
				var s *logproto.Stream
				var found bool
				s, found = resByStream[out.String()]
				if !found {
					s = &logproto.Stream{Labels: out.String()}
					resByStream[out.String()] = s
				}
				s.Entries = append(s.Entries, logproto.Entry{
					Timestamp: e.Timestamp,
					Line:      string(l),
				})
			}
		}
	}
	streams := []logproto.Stream{}
	for _, stream := range resByStream {
		streams = append(streams, *stream)
	}
	return streams
}

func processSeries(in []logproto.Stream, ex log.SampleExtractor) []logproto.Series {
	resBySeries := map[string]*logproto.Series{}

	for _, stream := range in {
		for _, e := range stream.Entries {
			exs := ex.ForStream(mustParseLabels(stream.Labels))
			if f, lbs, ok := exs.Process(e.Timestamp.UnixNano(), []byte(e.Line)); ok {
				var s *logproto.Series
				var found bool
				s, found = resBySeries[lbs.String()]
				if !found {
					s = &logproto.Series{Labels: lbs.String(), StreamHash: exs.BaseLabels().Hash()}
					resBySeries[lbs.String()] = s
				}
				s.Samples = append(s.Samples, logproto.Sample{
					Timestamp: e.Timestamp.UnixNano(),
					Value:     f,
					Hash:      xxhash.Sum64([]byte(e.Line)),
				})
			}
		}
	}
	series := []logproto.Series{}
	for _, s := range resBySeries {
		sort.Sort(s)
		series = append(series, *s)
	}
	return series
}

func (q MockQuerier) SelectSamples(_ context.Context, req SelectSampleParams) (iter.SampleIterator, error) {
	selector, err := req.LogSelector()
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

	var shard *index.ShardAnnotation
	if len(req.Shards) > 0 {
		shard, err = q.extractOldShard(req.Shards)
		if err != nil {
			return nil, err
		}
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

	filtered := processSeries(matched, extractor)

	return iter.NewTimeRangedSampleIterator(
		iter.NewMultiSeriesIterator(filtered),
		req.Start.UnixNano(),
		req.End.UnixNano()+1,
	), nil
}

type MockDownstreamer struct {
	*Engine
}

func (m MockDownstreamer) Downstreamer(_ context.Context) Downstreamer { return m }

func (m MockDownstreamer) Downstream(ctx context.Context, queries []DownstreamQuery, _ Accumulator) ([]logqlmodel.Result, error) {
	results := make([]logqlmodel.Result, 0, len(queries))
	for _, query := range queries {
		res, err := m.Query(query.Params).Exec(ctx)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}

	if matrix, ok := results[0].Data.(ProbabilisticQuantileMatrix); ok {
		if len(results) == 1 {
			return results, nil
		}
		for _, m := range results[1:] {
			matrix, _ = matrix.Merge(m.Data.(ProbabilisticQuantileMatrix))
		}
		return []logqlmodel.Result{{Data: matrix}}, nil
	}
	return results, nil
}

// create nStreams of nEntries with labelNames each where each label value
// with the exception of the "index" label is modulo'd into a shard
func randomStreams(nStreams, nEntries, nShards int, labelNames []string, valueField bool) (streams []logproto.Stream) {
	r := rand.New(rand.NewSource(42))
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
		for j := 0; j <= nEntries; j++ {
			line := fmt.Sprintf("stream=stderr level=debug line=%d", j)
			if valueField {
				line = fmt.Sprintf("%s value=%f", line, r.Float64()*100.0)
			}
			nanos := r.Int63n(time.Second.Nanoseconds())
			stream.Entries = append(stream.Entries, logproto.Entry{
				Timestamp: time.Unix(0, int64(j*int(time.Second))+nanos),
				Line:      line,
			})
		}

		stream.Labels = ls.String()
		stream.Hash = ls.Hash()
		streams = append(streams, stream)
	}
	return streams
}

func mustParseLabels(s string) labels.Labels {
	labels, err := promql_parser.ParseMetric(s)
	if err != nil {
		logger.Fatalf("Failed to parse %s", s)
	}

	return labels
}

func removeWhiteSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}
