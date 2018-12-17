package querier

import (
	"context"
	"sort"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/parser"
)

func (q Querier) queryStore(ctx context.Context, req *logproto.QueryRequest) ([]iter.EntryIterator, error) {
	matchers, err := parser.Matchers(req.Query)
	if err != nil {
		return nil, err
	}

	nameLabelMatcher, err := labels.NewMatcher(labels.MatchEqual, labels.MetricName, "logs")
	if err != nil {
		return nil, err
	}

	matchers = append(matchers, nameLabelMatcher)
	from, through := model.TimeFromUnixNano(req.Start.UnixNano()), model.TimeFromUnixNano(req.End.UnixNano())
	chunks, err := q.store.Get(ctx, from, through, matchers...)
	if err != nil {
		return nil, err
	}

	return partitionBySeriesChunks(req, chunks)
}

func partitionBySeriesChunks(req *logproto.QueryRequest, chunks []chunk.Chunk) ([]iter.EntryIterator, error) {
	chunksByFp := map[model.Fingerprint][]chunk.Chunk{}
	metricByFp := map[model.Fingerprint]model.Metric{}
	for _, c := range chunks {
		fp := c.Metric.Fingerprint()
		chunksByFp[fp] = append(chunksByFp[fp], c)
		delete(c.Metric, "__name__")
		metricByFp[fp] = c.Metric
	}

	iters := make([]iter.EntryIterator, 0, len(chunksByFp))
	for fp := range chunksByFp {
		iterators, err := partitionOverlappingChunks(req, metricByFp[fp].String(), chunksByFp[fp])
		if err != nil {
			return nil, err
		}
		iterator := iter.NewHeapIterator(iterators, req.Direction)
		iters = append(iters, iterator)
	}

	return iters, nil
}

func partitionOverlappingChunks(req *logproto.QueryRequest, labels string, chunks []chunk.Chunk) ([]iter.EntryIterator, error) {
	sort.Sort(byFrom(chunks))

	css := [][]chunk.Chunk{}
outer:
	for _, c := range chunks {
		for i, cs := range css {
			if cs[len(cs)-1].Through.Before(c.From) {
				css[i] = append(css[i], c)
				continue outer
			}
		}
		cs := make([]chunk.Chunk, 0, len(chunks)/(len(css)+1))
		cs = append(cs, c)
		css = append(css, cs)
	}

	result := make([]iter.EntryIterator, 0, len(css))
	for i := range css {
		iterators := make([]iter.EntryIterator, 0, len(css[i]))
		for j := range css[i] {
			lokiChunk := css[i][j].Data.(*chunkenc.Facade).LokiChunk()
			iterator, err := lokiChunk.Iterator(req.Start, req.End, req.Direction)
			if err != nil {
				return nil, err
			}
			if req.Regex != "" {
				iterator, err = iter.NewRegexpFilter(req.Regex, iterator)
				if err != nil {
					return nil, err
				}
			}
			iterators = append(iterators, iterator)
		}
		result = append(result, iter.NewNonOverlappingIterator(iterators, labels))
	}
	return result, nil
}

type byFrom []chunk.Chunk

func (b byFrom) Len() int           { return len(b) }
func (b byFrom) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byFrom) Less(i, j int) bool { return b[i].From < b[j].From }
