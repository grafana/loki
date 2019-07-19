package storage

import (
	"context"
	"sort"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/storage"
	"github.com/cortexproject/cortex/pkg/util/validation"
	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/util"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

// Store is the Loki chunk store to retrieve and save chunks.
type Store interface {
	chunk.Store
	LazyQuery(ctx context.Context, req *logproto.QueryRequest) (iter.EntryIterator, error)
}

type store struct {
	chunk.Store
}

// NewStore creates a new Loki Store using configuration supplied.
func NewStore(cfg storage.Config, storeCfg chunk.StoreConfig, schemaCfg chunk.SchemaConfig, limits *validation.Overrides) (Store, error) {
	s, err := storage.NewStore(cfg, storeCfg, schemaCfg, limits)
	if err != nil {
		return nil, err
	}
	return &store{
		Store: s,
	}, nil
}

// LazyQuery returns an iterator that will query the store for more chunks while iterating instead of fetching all chunks upfront
// for that request.
func (s *store) LazyQuery(ctx context.Context, req *logproto.QueryRequest) (iter.EntryIterator, error) {
	expr, err := logql.ParseExpr(req.Query)
	if err != nil {
		return nil, err
	}

	if req.Regex != "" {
		expr = logql.NewFilterExpr(expr, labels.MatchRegexp, req.Regex)
	}

	querier := logql.QuerierFunc(func(matchers []*labels.Matcher, filter logql.Filter) (iter.EntryIterator, error) {
		nameLabelMatcher, err := labels.NewMatcher(labels.MatchEqual, labels.MetricName, "logs")
		if err != nil {
			return nil, err
		}

		matchers = append(matchers, nameLabelMatcher)
		from, through := util.RoundToMilliseconds(req.Start, req.End)
		chks, fetchers, err := s.GetChunkRefs(ctx, from, through, matchers...)
		if err != nil {
			return nil, err
		}

		for i := range chks {
			chks[i] = filterChunksByTime(from, through, chks[i])
		}

		chksBySeries := partitionBySeriesChunks(chks, fetchers)

		// Make sure the initial chunks are loaded. This is not one chunk
		// per series, but rather a chunk per non-overlapping iterator.
		if err := loadFirstChunks(ctx, chksBySeries, req); err != nil {
			return nil, err
		}

		// Now that we have the first chunk for each series loaded,
		// we can proceed to filter the series that don't match.
		chksBySeries = filterSeriesByMatchers(chksBySeries, matchers, req)

		iters, err := buildIterators(ctx, req, chksBySeries, filter)
		if err != nil {
			return nil, err
		}

		return iter.NewHeapIterator(iters, req.Direction), nil
	})

	return expr.Eval(querier)
}

func filterChunksByTime(from, through model.Time, chunks []chunk.Chunk) []chunk.Chunk {
	filtered := make([]chunk.Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.Through < from || through < chunk.From {
			continue
		}
		filtered = append(filtered, chunk)
	}
	return filtered
}

func filterSeriesByMatchers(chks map[model.Fingerprint][][]chunkenc.LazyChunk, matchers []*labels.Matcher, req *logproto.QueryRequest) map[model.Fingerprint][][]chunkenc.LazyChunk {
outer:
	for fp, chunks := range chks {
		for _, matcher := range matchers {
			// checks matchers against the last chunk if we're doing BACKWARD
			if req.Direction == logproto.BACKWARD {
				if !matcher.Matches(chunks[0][len(chunks[0])-1].Chunk.Metric.Get(matcher.Name)) {
					delete(chks, fp)
					continue outer
				}
			} else {
				if !matcher.Matches(chunks[0][0].Chunk.Metric.Get(matcher.Name)) {
					delete(chks, fp)
					continue outer
				}
			}

		}
	}

	return chks
}

func buildIterators(ctx context.Context, req *logproto.QueryRequest, chks map[model.Fingerprint][][]chunkenc.LazyChunk, filter logql.Filter) ([]iter.EntryIterator, error) {
	result := make([]iter.EntryIterator, 0, len(chks))
	for _, chunks := range chks {
		iterator, err := buildHeapIterator(ctx, req, chunks, filter)
		if err != nil {
			return nil, err
		}
		result = append(result, iterator)
	}

	return result, nil
}

func buildHeapIterator(ctx context.Context, req *logproto.QueryRequest, chks [][]chunkenc.LazyChunk, filter logql.Filter) (iter.EntryIterator, error) {
	result := make([]iter.EntryIterator, 0, len(chks))
	var fetchedChunkIndex int
	if req.Direction == logproto.BACKWARD {
		fetchedChunkIndex = len(chks[0]) - 1
	}
	if chks[0][fetchedChunkIndex].Chunk.Metric.Has("__name__") {
		labelsBuilder := labels.NewBuilder(chks[0][fetchedChunkIndex].Chunk.Metric)
		labelsBuilder.Del("__name__")
		chks[0][fetchedChunkIndex].Chunk.Metric = labelsBuilder.Labels()
	}
	labels := chks[0][fetchedChunkIndex].Chunk.Metric.String()

	for i := range chks {
		iterators := make([]iter.EntryIterator, 0, len(chks[i]))
		for j := range chks[i] {
			iterator, err := chks[i][j].Iterator(ctx, req.Start, req.End, req.Direction, filter)
			if err != nil {
				return nil, err
			}
			iterators = append(iterators, iterator)
		}
		// reverse chunks to start with the last one.
		if req.Direction == logproto.BACKWARD {
			for i, j := 0, len(iterators)-1; i < j; i, j = i+1, j-1 {
				iterators[i], iterators[j] = iterators[j], iterators[i]
			}
		}
		result = append(result, iter.NewNonOverlappingIterator(iterators, labels))
	}

	return iter.NewHeapIterator(result, req.Direction), nil
}

func loadFirstChunks(ctx context.Context, chks map[model.Fingerprint][][]chunkenc.LazyChunk, req *logproto.QueryRequest) error {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "loadFirstChunks")
	defer sp.Finish()

	// If chunks span buckets, then we'll have different fetchers for each bucket.
	chksByFetcher := map[*chunk.Fetcher][]*chunkenc.LazyChunk{}
	for _, lchks := range chks {
		for _, lchk := range lchks {
			if len(lchk) == 0 {
				continue
			}
			// load the last chunk if we're doing BACKWARD
			if req.Direction == logproto.BACKWARD {
				chksByFetcher[lchk[0].Fetcher] = append(chksByFetcher[lchk[0].Fetcher], &lchk[len(lchk)-1])
			} else {
				chksByFetcher[lchk[0].Fetcher] = append(chksByFetcher[lchk[0].Fetcher], &lchk[0])
			}
		}
	}

	errChan := make(chan error)
	for fetcher, chunks := range chksByFetcher {
		go func(fetcher *chunk.Fetcher, chunks []*chunkenc.LazyChunk) {

			keys := make([]string, 0, len(chunks))
			chks := make([]chunk.Chunk, 0, len(chunks))
			index := make(map[string]*chunkenc.LazyChunk, len(chunks))

			for _, chk := range chunks {
				key := chk.Chunk.ExternalKey()
				keys = append(keys, key)
				chks = append(chks, chk.Chunk)
				index[key] = chk
			}
			chks, err := fetcher.FetchChunks(ctx, chks, keys)
			if err != nil {
				errChan <- err
				return
			}
			// assign fetched chunk by key as FetchChunks doesn't guarantee the order.
			for _, chk := range chks {
				index[chk.ExternalKey()].Chunk = chk
			}

			errChan <- nil
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
	for i, chks := range chunks {
		for _, c := range chks {
			fp := c.Fingerprint
			chunksByFp[fp] = append(chunksByFp[fp], chunkenc.LazyChunk{Chunk: c, Fetcher: fetchers[i]})
		}
	}

	result := make(map[model.Fingerprint][][]chunkenc.LazyChunk, len(chunksByFp))

	for fp, chks := range chunksByFp {
		result[fp] = partitionOverlappingChunks(chks)
	}

	return result
}

// partitionOverlappingChunks splits the list of chunks into different non-overlapping lists.
func partitionOverlappingChunks(chunks []chunkenc.LazyChunk) [][]chunkenc.LazyChunk {
	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].Chunk.From < chunks[j].Chunk.From
	})

	css := [][]chunkenc.LazyChunk{}
outer:
	for _, c := range chunks {
		for i, cs := range css {
			// If the chunk doesn't overlap with the current list, then add it to it.
			if cs[len(cs)-1].Chunk.Through.Before(c.Chunk.From) {
				css[i] = append(css[i], c)
				continue outer
			}
		}
		// If the chunk overlaps with every existing list, then create a new list.
		cs := make([]chunkenc.LazyChunk, 0, len(chunks)/(len(css)+1))
		cs = append(cs, c)
		css = append(css, cs)
	}

	return css
}
