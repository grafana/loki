package querier

import (
	"context"
	"sort"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
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
	chks, fetchers, err := q.store.GetChunkRefs(ctx, from, through, matchers...)
	if err != nil {
		return nil, err
	}

	for i := range chks {
		chks[i], _ = filterChunksByTime(from, through, chks[i])
	}

	chksBySeries := partitionBySeriesChunks(chks, fetchers)
	// Make sure the initial chunks are loaded. This is not one chunk
	// per series, but rather a chunk per non-overlapping iterator.
	if err := loadFirstChunks(ctx, chksBySeries); err != nil {
		return nil, err
	}

	// Now that we have the first chunk for each series loaded,
	// we can proceed to filter the series that don't match.
	chksBySeries = filterSeriesByMatchers(chksBySeries, matchers)

	return buildIterators(ctx, req, chksBySeries)
}

func filterChunksByTime(from, through model.Time, chunks []chunk.Chunk) ([]chunk.Chunk, []string) {
	filtered := make([]chunk.Chunk, 0, len(chunks))
	keys := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.Through < from || through < chunk.From {
			continue
		}
		filtered = append(filtered, chunk)
		keys = append(keys, chunk.ExternalKey())
	}
	return filtered, keys
}

func filterSeriesByMatchers(chks map[model.Fingerprint][][]chunkenc.LazyChunk, matchers []*labels.Matcher) map[model.Fingerprint][][]chunkenc.LazyChunk {
outer:
	for fp, chunks := range chks {
		for _, matcher := range matchers {
			if !matcher.Matches(string(chunks[0][0].Chunk.Metric[model.LabelName(matcher.Name)])) {
				delete(chks, fp)
				continue outer
			}
		}
	}

	return chks
}

func buildIterators(ctx context.Context, req *logproto.QueryRequest, chks map[model.Fingerprint][][]chunkenc.LazyChunk) ([]iter.EntryIterator, error) {
	result := make([]iter.EntryIterator, 0, len(chks))
	for _, chunks := range chks {
		iterator, err := buildHeapIterator(ctx, req, chunks)
		if err != nil {
			return nil, err
		}

		result = append(result, iterator)
	}

	return result, nil
}

func buildHeapIterator(ctx context.Context, req *logproto.QueryRequest, chks [][]chunkenc.LazyChunk) (iter.EntryIterator, error) {
	result := make([]iter.EntryIterator, 0, len(chks))

	labels := chks[0][0].Chunk.Metric.String()

	for i := range chks {
		iterators := make([]iter.EntryIterator, 0, len(chks[i]))
		for j := range chks[i] {
			iterator, err := chks[i][j].Iterator(ctx, req.Start, req.End, req.Direction)
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

	return iter.NewHeapIterator(result, req.Direction), nil
}

func loadFirstChunks(ctx context.Context, chks map[model.Fingerprint][][]chunkenc.LazyChunk) error {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "loadFirstChunks")
	defer sp.Finish()

	// If chunks span buckets, then we'll have different fetchers for each bucket.
	chksByFetcher := map[*chunk.Fetcher][]*chunkenc.LazyChunk{}
	for _, lchks := range chks {
		for _, lchk := range lchks {
			chksByFetcher[lchk[0].Fetcher] = append(chksByFetcher[lchk[0].Fetcher], &lchk[0])
		}
	}

	sp.LogFields(otlog.Int("fetchers", len(chksByFetcher)))

	errChan := make(chan error)
	for fetcher, chunks := range chksByFetcher {
		go func(fetcher *chunk.Fetcher, chunks []*chunkenc.LazyChunk) {
			keys := make([]string, 0, len(chunks))
			chks := make([]chunk.Chunk, 0, len(chunks))
			for _, chk := range chunks {
				keys = append(keys, chk.Chunk.ExternalKey())
				chks = append(chks, chk.Chunk)
			}
			chks, err := fetcher.FetchChunks(ctx, chks, keys)
			errChan <- err
			if err != nil {
				return
			}

			for i, chk := range chks {
				chunks[i].Chunk = chk
			}
		}(fetcher, chunks)
	}

	var lastErr error
	for i := 0; i < len(chksByFetcher); i++ {
		if err := <-errChan; err != nil {
			lastErr = err
		}
	}

	return lastErr
}

func partitionBySeriesChunks(chunks [][]chunk.Chunk, fetchers []*chunk.Fetcher) map[model.Fingerprint][][]chunkenc.LazyChunk {
	chunksByFp := map[model.Fingerprint][]chunkenc.LazyChunk{}
	metricByFp := map[model.Fingerprint]model.Metric{}
	for i, chks := range chunks {
		for _, c := range chks {
			fp := c.Metric.Fingerprint()
			chunksByFp[fp] = append(chunksByFp[fp], chunkenc.LazyChunk{Chunk: c, Fetcher: fetchers[i]})
			delete(c.Metric, "__name__")
			metricByFp[fp] = c.Metric
		}
	}

	result := make(map[model.Fingerprint][][]chunkenc.LazyChunk, len(chunksByFp))

	for fp, chks := range chunksByFp {
		result[fp] = partitionOverlappingChunks(chks)
	}

	return result
}

func partitionOverlappingChunks(chunks []chunkenc.LazyChunk) [][]chunkenc.LazyChunk {
	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].Chunk.From < chunks[i].Chunk.From
	})

	css := [][]chunkenc.LazyChunk{}
outer:
	for _, c := range chunks {
		for i, cs := range css {
			if cs[len(cs)-1].Chunk.Through.Before(c.Chunk.From) {
				css[i] = append(css[i], c)
				continue outer
			}
		}
		cs := make([]chunkenc.LazyChunk, 0, len(chunks)/(len(css)+1))
		cs = append(cs, c)
		css = append(css, cs)
	}

	return css
}

type byFrom []chunk.Chunk

func (b byFrom) Len() int           { return len(b) }
func (b byFrom) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byFrom) Less(i, j int) bool { return b[i].From < b[j].From }
